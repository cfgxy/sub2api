package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// inflightReservationEstimator 估算单请求在途预留金额（USD）；false 表示无法定价。
type inflightReservationEstimator interface {
	EstimateInflightReservation(ctx context.Context, apiKey *service.APIKey, req service.InflightEstimateRequest) (float64, bool)
}

type enterpriseEmployeeUsageReserver interface {
	ReserveEnterpriseEmployeeUsage(ctx context.Context, apiKey *service.APIKey, req service.InflightEstimateRequest) (*service.EnterpriseEmployeeUsageReservation, error)
}

// requestMaxOutputTokens 从请求体中提取输出 token 上限（兼容 Anthropic / OpenAI Chat / Responses / Gemini）。
func requestMaxOutputTokens(body []byte) int {
	for _, path := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens", "generationConfig.maxOutputTokens", "generation_config.max_output_tokens"} {
		if v := gjson.GetBytes(body, path); v.Exists() && v.Type == gjson.Number && v.Int() > 0 {
			return int(v.Int())
		}
	}
	return 0
}

// tokenInflightEstimate 文本类请求的估算输入。
func tokenInflightEstimate(model string, body []byte) service.InflightEstimateRequest {
	return service.InflightEstimateRequest{
		Model:                  model,
		BodyBytes:              len(body),
		MaxTokens:              requestMaxOutputTokens(body),
		Kind:                   service.InflightEstimateToken,
		RequestPayloadHash:     service.HashUsageRequestPayload(body),
		HasExternalContext:     requestReferencesExternalContext(body),
		HasUnboundedSurcharges: requestHasUnboundedToolSurcharge(body),
	}
}

// requestReferencesExternalContext identifies request shapes where upstream
// input billing can include server-side content absent from the request body.
func requestReferencesExternalContext(body []byte) bool {
	for _, path := range []string{"previous_response_id", "cachedContent", "cached_content", "conversation"} {
		if gjson.GetBytes(body, path).Exists() {
			return true
		}
	}
	// Responses WebSocket 的 ingress 会为无 previous_response_id 的
	// function_call_output 自动补上一轮响应 ID。预占发生在该改写之前，
	// 因而此处必须把这类帧视为带有服务端上下文，避免仅按当前帧大小低估。
	if gjson.GetBytes(body, `input.#(type=="function_call_output")`).Exists() {
		return true
	}
	return requestReferencesRemoteMedia(body)
}

// requestReferencesRemoteMedia identifies image or media URLs whose bytes and
// resulting input-token charge are not contained in the request body. Data URLs
// remain bounded by BodyBytes; remote URLs must fail closed for employee keys.
func requestReferencesRemoteMedia(body []byte) bool {
	isRemoteURL := func(value string) bool {
		value = strings.ToLower(strings.TrimSpace(value))
		return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "gs://")
	}
	var visit func(gjson.Result) bool
	visit = func(value gjson.Result) bool {
		if value.IsArray() {
			for _, child := range value.Array() {
				if visit(child) {
					return true
				}
			}
			return false
		}
		if !value.IsObject() {
			return false
		}

		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		if typ == "image_url" || typ == "input_image" || typ == "image" || typ == "input_video" || typ == "input_audio" ||
			value.Get("file_data").Exists() || value.Get("fileData").Exists() {
			for _, path := range []string{"image_url", "image_url.url", "url", "file_data.file_uri", "fileData.fileUri"} {
				if isRemoteURL(value.Get(path).String()) {
					return true
				}
			}
		}
		for _, child := range value.Map() {
			if visit(child) {
				return true
			}
		}
		return false
	}
	return visit(gjson.ParseBytes(body))
}

// requestHasUnboundedToolSurcharge identifies tools whose charge cannot be
// bounded from request fields alone. Enterprise employee requests fail closed
// rather than reserve only the token portion of these calls.
func requestHasUnboundedToolSurcharge(body []byte) bool {
	// OAuth Responses 转换会根据历史 web_search_call 在后续补入搜索工具。
	// 预占时顶层 tools 尚未出现该工具，但其附加收费仍不可由请求字段证明上界。
	if gjson.GetBytes(body, `input.#(type=="web_search_call")`).Exists() {
		return true
	}

	var visit func(gjson.Result) bool
	visit = func(value gjson.Result) bool {
		if value.IsArray() {
			for _, child := range value.Array() {
				if visit(child) {
					return true
				}
			}
			return false
		}
		if !value.IsObject() {
			return false
		}
		for key, child := range value.Map() {
			label := strings.ToLower(key)
			if label == "type" {
				label = strings.ToLower(child.String())
			}
			if strings.Contains(label, "web_search") ||
				strings.Contains(label, "google_search") ||
				strings.Contains(label, "image_generation") {
				return true
			}
			if visit(child) {
				return true
			}
		}
		return false
	}
	return visit(gjson.GetBytes(body, "tools"))
}

func inflightNoop() {}

// reserveInflightBalance 在 CheckBillingEligibility 之后为余额模式请求登记在途预留。
//
// 成功时把预留句柄挂到 c.Request 的 context 上：之后通过 submit*UsageRecordTask 提交的
// 计费任务会接管一个引用，直到余额缓存实际扣减后才释放。返回的 done 必须 defer 调用
// （停止续期并归还 handler 引用；没有提交计费任务时即刻释放）。
// 开关关闭、订阅模式、Redis 故障时返回 no-op（fail-open）；无法定价时默认 fail-open，
// 配置 fail_closed_on_unpriced=true 时返回 ErrInsufficientBalance。
func reserveInflightBalance(
	c *gin.Context,
	billing *service.BillingCacheService,
	estimator inflightReservationEstimator,
	apiKey *service.APIKey,
	subscription *service.UserSubscription,
	req service.InflightEstimateRequest,
) (func(), error) {
	if c == nil || c.Request == nil {
		return inflightNoop, nil
	}
	ctx, done, err := reserveInflightBalanceCtx(c.Request.Context(), billing, estimator, apiKey, subscription, req)
	if err != nil {
		return inflightNoop, err
	}
	c.Request = c.Request.WithContext(ctx)
	return done, nil
}

// reserveInflightBalanceCtx 同 reserveInflightBalance，但返回携带预留句柄的新 context
// （供 WebSocket 等自管 context 的路径）。
func reserveInflightBalanceCtx(
	ctx context.Context,
	billing *service.BillingCacheService,
	estimator inflightReservationEstimator,
	apiKey *service.APIKey,
	subscription *service.UserSubscription,
	req service.InflightEstimateRequest,
) (context.Context, func(), error) {
	if estimator == nil || apiKey == nil || apiKey.User == nil {
		return ctx, inflightNoop, nil
	}
	if apiKey.EnterpriseEmployeeWeeklyLimit {
		reserver, ok := estimator.(enterpriseEmployeeUsageReserver)
		if !ok {
			return ctx, inflightNoop, service.ErrBillingServiceUnavailable
		}
		employee, err := reserver.ReserveEnterpriseEmployeeUsage(ctx, apiKey, req)
		if err != nil {
			return ctx, inflightNoop, err
		}
		res := service.AttachEnterpriseEmployeeUsageReservation(nil, employee)
		return service.WithInflightReservation(ctx, res), res.HandlerDone, nil
	}
	if billing == nil || !billing.InflightReservationEnabled() {
		return ctx, inflightNoop, nil
	}
	if apiKey.Group != nil && apiKey.Group.IsSubscriptionType() && subscription != nil {
		return ctx, inflightNoop, nil
	}
	estimate, priced := estimator.EstimateInflightReservation(ctx, apiKey, req)
	if !priced && billing.InflightReservationFailClosedOnUnpriced() {
		return ctx, inflightNoop, service.ErrInsufficientBalance
	}
	if estimate <= 0 {
		return ctx, inflightNoop, nil
	}
	res, err := billing.ReserveInflight(ctx, apiKey.User, apiKey.Group, subscription, estimate)
	if err != nil {
		return ctx, inflightNoop, err
	}
	if res == nil {
		return ctx, inflightNoop, nil
	}
	return service.WithInflightReservation(ctx, res), res.HandlerDone, nil
}

// grokMediaInflightEstimate 媒体生成请求的估算输入；状态/内容查询返回空模型（不预留：
// 查询会为已生成的媒体计费，不能因余额预留而拦截用户取回已付费结果）。
func grokMediaInflightEstimate(endpoint service.GrokMediaEndpoint, model string, info service.GrokMediaRequestInfo, body []byte) service.InflightEstimateRequest {
	if !endpoint.IsGenerationRequest() {
		return service.InflightEstimateRequest{}
	}
	switch endpoint {
	case service.GrokMediaEndpointImagesGenerations, service.GrokMediaEndpointImagesEdits:
		return service.InflightEstimateRequest{Model: model, BodyBytes: len(body), Kind: service.InflightEstimateImage, Units: info.N}
	default:
		return service.InflightEstimateRequest{
			Model:                model,
			BodyBytes:            len(body),
			Kind:                 service.InflightEstimateVideo,
			Units:                1,
			VideoResolution:      info.Resolution,
			VideoDurationSeconds: info.DurationSeconds,
		}
	}
}

// grokVoiceSTTBytesPerSecond STT 时长粗估（~128kbps 压缩音频）。
const grokVoiceSTTBytesPerSecond = 16000

// grokVoiceInflightEstimate 语音 HTTP 接口估算：TTS 按输入字符数（百万字符），STT 按音频字节粗估时长（小时）。
// 其他接口（custom-voices）无音频计量，返回的估算为 0（不预留）。
func grokVoiceInflightEstimate(endpoint string, body []byte) service.InflightEstimateRequest {
	req := service.InflightEstimateRequest{Model: endpoint, Kind: service.InflightEstimateAudio, AudioMode: endpoint}
	switch endpoint {
	case "tts":
		req.AudioUnits = float64(len([]rune(extractGrokTTSInputText(body)))) / 1e6
	case "stt":
		req.AudioUnits = float64(len(body)) / grokVoiceSTTBytesPerSecond / 3600
	}
	return req
}
