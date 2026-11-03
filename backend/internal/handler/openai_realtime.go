package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// OfficialRealtimeWebSocket handles the public Platform Realtime protocol.
// It is intentionally separate from Grok /v1/realtime and private /v1/live.
func (h *OpenAIGatewayHandler) OfficialRealtimeWebSocket(c *gin.Context) {
	if !isOpenAIWSUpgradeRequest(c.Request) {
		h.errorResponse(c, http.StatusUpgradeRequired, "invalid_request_error", "WebSocket upgrade required (Upgrade: websocket)")
		return
	}
	setOpenAIClientTransportWS(c)
	apiKey, subject, model, subscription, ok := h.realtimeAdmission(c)
	if !ok {
		return
	}
	reqLog := requestLogger(c, "handler.openai_gateway.official_realtime_ws",
		zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID), zap.Any("group_id", apiKey.GroupID))

	userRelease, acquired, err := h.concurrencyHelper.TryAcquireUserSlot(c.Request.Context(), subject.UserID, subject.Concurrency)
	if err != nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Realtime concurrency unavailable")
		return
	}
	if !acquired {
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Realtime concurrency limit reached")
		return
	}
	defer userRelease()

	selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
		c.Request.Context(), apiKey.GroupID, "", openAIWSIngressFallbackSessionSeed(subject.UserID, apiKey.ID, apiKey.GroupID),
		model, nil, service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityRealtime, false, false, true,
	)
	if err != nil || selection == nil || selection.Account == nil {
		markOpsRoutingCapacityLimited(c)
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No official Realtime account available")
		return
	}
	streamStarted := false
	accountRelease, slotResult := h.acquireOpenAIAccountSlot(c, apiKey.GroupID, "", selection, true, &streamStarted, reqLog, nil)
	if slotResult != openAISlotAcquireOK || accountRelease == nil {
		return
	}
	defer accountRelease()

	client, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer client.CloseNow()
	messageType, firstMessage, err := client.Read(c.Request.Context())
	if err != nil {
		return
	}
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, model, firstMessage); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		_ = client.Close(coderws.StatusPolicyViolation, "request rejected by security policy")
		return
	}
	requestPayloadHash := service.HashUsageRequestPayload(firstMessage)
	attribution := realtimeEnterpriseAttribution(apiKey, subscription)
	account := selection.Account
	proxyErr := h.gatewayService.ProxyOfficialRealtimeWebSocket(c.Request.Context(), client, account, model, messageType, firstMessage,
		func(turn openaiwsv2.RelayTurnResult) {
			if !realtimeUsageHasTokens(turn.Usage) {
				return
			}
			result := &service.OpenAIForwardResult{
				RequestID:             turn.RequestID,
				ResponseID:            turn.RequestID,
				Usage:                 realtimeUsage(turn.Usage),
				Model:                 firstNonEmptyRealtime(turn.RequestModel, model),
				UpstreamResponseModel: turn.ResponseModel,
				Stream:                true,
				OpenAIRealtime:        true,
				UpstreamEndpoint:      "/v1/realtime",
				UpstreamTerminalEvent: turn.TerminalEventType,
				Duration:              turn.Duration,
				FirstTokenMs:          turn.FirstTokenMs,
			}
			if err := h.gatewayService.RecordUsage(c.Request.Context(), &service.OpenAIRecordUsageInput{
				Result:                result,
				APIKey:                apiKey,
				User:                  apiKey.User,
				Account:               account,
				Subscription:          subscription,
				InboundEndpoint:       GetInboundEndpoint(c),
				UpstreamEndpoint:      "/v1/realtime",
				UserAgent:             c.GetHeader("User-Agent"),
				IPAddress:             ip.GetClientIP(c),
				RequestPayloadHash:    requestPayloadHash,
				APIKeyService:         h.apiKeyService,
				QuotaPlatform:         service.QuotaPlatform(c.Request.Context(), apiKey),
				PricingAt:             turn.StartedAt,
				EnterpriseAttribution: attribution,
			}); err != nil {
				reqLog.Error("official_realtime_record_usage_failed", zap.Int64("account_id", account.ID), zap.String("request_id", turn.RequestID), zap.Error(err))
			}
		})
	if proxyErr == nil || openAIWSIngressEndedByClient(proxyErr) {
		_ = client.Close(coderws.StatusNormalClosure, "")
		return
	}
	reqLog.Warn("official_realtime_proxy_failed", zap.Int64("account_id", account.ID), zap.Error(proxyErr))
	_ = client.Close(coderws.StatusInternalError, "official realtime upstream closed")
}

// OfficialRealtimeCall proxies a WebRTC SDP offer to the Platform Calls API.
func (h *OpenAIGatewayHandler) OfficialRealtimeCall(c *gin.Context) {
	apiKey, subject, model, subscription, ok := h.realtimeAdmission(c)
	if !ok {
		return
	}
	request, err := parseOfficialRealtimeCallRequest(c)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if err := service.ValidateRealtimeClientFrameModel(request.Session, model); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	reqLog := requestLogger(c, "handler.openai_gateway.official_realtime_call",
		zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID), zap.Any("group_id", apiKey.GroupID))
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, model, request.Session); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	reservationDone, err := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(model, request.Session))
	if err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	defer reservationDone()
	userRelease, acquired, err := h.concurrencyHelper.TryAcquireUserSlot(c.Request.Context(), subject.UserID, subject.Concurrency)
	if err != nil || !acquired {
		status := http.StatusTooManyRequests
		if err != nil {
			status = http.StatusServiceUnavailable
		}
		h.errorResponse(c, status, "rate_limit_error", "Realtime concurrency limit reached")
		return
	}
	defer userRelease()
	selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(c.Request.Context(), apiKey.GroupID, "", "", model, nil, service.OpenAIUpstreamTransportHTTPSSE, service.OpenAIEndpointCapabilityRealtime, false, false, true)
	if err != nil || selection == nil || selection.Account == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No official Realtime account available")
		return
	}
	streamStarted := false
	accountRelease, slotResult := h.acquireOpenAIAccountSlot(c, apiKey.GroupID, "", selection, false, &streamStarted, reqLog, nil)
	if slotResult != openAISlotAcquireOK || accountRelease == nil {
		return
	}
	defer accountRelease()
	created, err := h.gatewayService.CreateOfficialRealtimeCall(c.Request.Context(), selection.Account, request)
	if err != nil {
		h.writeOfficialRealtimeError(c, err)
		return
	}
	c.Header("Location", created.Location)
	c.Header("X-Realtime-Call-ID", created.CallID)
	c.Data(http.StatusOK, "application/sdp", created.SDP)
}

func (h *OpenAIGatewayHandler) realtimeAdmission(c *gin.Context) (*service.APIKey, middleware2.AuthSubject, string, *service.UserSubscription, bool) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return nil, middleware2.AuthSubject{}, "", nil, false
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return nil, middleware2.AuthSubject{}, "", nil, false
	}
	if apiKey.Group == nil || (apiKey.Group.Platform != service.PlatformOpenAI && apiKey.Group.Platform != service.PlatformComposite) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Official Realtime is not supported for this platform")
		return nil, middleware2.AuthSubject{}, "", nil, false
	}
	if !apiKey.Group.AllowRealtime {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "Official Realtime is not enabled for this group")
		return nil, middleware2.AuthSubject{}, "", nil, false
	}
	model := strings.TrimSpace(c.Query("model"))
	if model == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model query parameter is required")
		return nil, middleware2.AuthSubject{}, "", nil, false
	}
	if blocked := blockedModelAllowlistCandidate(apiKey.Group, []string{model}); blocked != "" {
		h.errorResponse(c, http.StatusNotFound, "invalid_request_error", "Model is not allowed for this group")
		return nil, middleware2.AuthSubject{}, "", nil, false
	}
	if !compositeTargetPlatformAllowed(c, apiKey, model, service.PlatformOpenAI) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Official Realtime only supports OpenAI models for Composite groups")
		return nil, middleware2.AuthSubject{}, "", nil, false
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if h.billingCacheService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Billing service unavailable")
		return nil, middleware2.AuthSubject{}, "", nil, false
	}
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return nil, middleware2.AuthSubject{}, "", nil, false
	}
	return apiKey, subject, model, subscription, true
}

func parseOfficialRealtimeCallRequest(c *gin.Context) (*service.RealtimeCallRequest, error) {
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		request := &service.RealtimeCallRequest{SDP: c.PostForm("sdp"), Session: json.RawMessage(c.PostForm("session"))}
		return request, service.ValidateRealtimeCallRequest(request)
	}
	var request service.RealtimeCallRequest
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&request); err != nil {
		return nil, errors.New("request body must be valid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("request body must contain one JSON object")
	}
	return &request, service.ValidateRealtimeCallRequest(&request)
}

func realtimeEnterpriseAttribution(apiKey *service.APIKey, subscription *service.UserSubscription) *service.EnterpriseUsageAttributionSnapshot {
	if apiKey == nil || subscription == nil {
		return nil
	}
	return service.SnapshotEnterpriseUsageAttribution(apiKey.EnterpriseAttributionIdentity, time.Now(), subscription.DailyWindowStart, subscription.WeeklyWindowStart, subscription.MonthlyWindowStart)
}

func realtimeUsage(usage openaiwsv2.Usage) service.OpenAIUsage {
	return service.OpenAIUsage{InputTokens: usage.InputTokens, ImageInputTokens: usage.ImageInputTokens, OutputTokens: usage.OutputTokens, CacheCreationInputTokens: usage.CacheCreationInputTokens, CacheReadInputTokens: usage.CacheReadInputTokens, ImageOutputTokens: usage.ImageOutputTokens}
}

func realtimeUsageHasTokens(usage openaiwsv2.Usage) bool {
	return usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.ImageInputTokens > 0 || usage.ImageOutputTokens > 0 || usage.CacheCreationInputTokens > 0 || usage.CacheReadInputTokens > 0
}

func firstNonEmptyRealtime(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (h *OpenAIGatewayHandler) writeOfficialRealtimeError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrRealtimeUnavailable) {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Official Realtime is unavailable")
		return
	}
	var upstreamErr *service.UpstreamFailoverError
	if errors.As(err, &upstreamErr) && upstreamErr.StatusCode >= 400 && upstreamErr.StatusCode < 500 {
		h.errorResponse(c, upstreamErr.StatusCode, "invalid_request_error", "Official Realtime upstream rejected the request")
		return
	}
	h.errorResponse(c, http.StatusBadGateway, "api_error", "Official Realtime upstream request failed")
}
