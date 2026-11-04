package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	realtimeWebhookBodyLimit  = 1 << 20
	defaultRealtimeWebhookAge = 5 * time.Minute
	realtimeWebhookReplayTTL  = 24 * time.Hour
	realtimeWebhookClaimLease = 30 * time.Second
)

type officialRealtimeWebhookEvent struct {
	Type string `json:"type"`
	Data struct {
		CallID string `json:"call_id"`
	} `json:"data"`
}

type officialRealtimeCallTerminator interface {
	ControlOfficialRealtimeCall(ctx context.Context, account *service.Account, callID, action, model string) error
	FinalizeOfficialRealtimeCall(record *service.RealtimeCallRecord)
}

type realtimeWSEmployeeTurnBindings struct {
	mu         sync.Mutex
	byResponse map[string]*service.InflightReservation
	prepared   []*service.InflightReservation
}

// bindReservation attaches the hold to the provider-issued response ID. Unlike
// the legacy pending FIFO, this is correct for VAD-created and interleaved turns.
func (b *realtimeWSEmployeeTurnBindings) bindReservation(responseID string, reservation *service.InflightReservation) bool {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" || reservation == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byResponse == nil {
		b.byResponse = make(map[string]*service.InflightReservation)
	}
	if _, exists := b.byResponse[responseID]; exists {
		return false
	}
	b.byResponse[responseID] = reservation
	return true
}

func (b *realtimeWSEmployeeTurnBindings) prepare(reservation *service.InflightReservation) bool {
	if reservation == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prepared = append(b.prepared, reservation)
	return true
}

func (b *realtimeWSEmployeeTurnBindings) hasPrepared() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.prepared) > 0
}

func (b *realtimeWSEmployeeTurnBindings) bindPrepared(responseID string) (*service.InflightReservation, bool) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if reservation := b.byResponse[responseID]; reservation != nil {
		return reservation, true
	}
	if len(b.prepared) == 0 {
		return nil, false
	}
	reservation := b.prepared[0]
	b.prepared = b.prepared[1:]
	if b.byResponse == nil {
		b.byResponse = make(map[string]*service.InflightReservation)
	}
	b.byResponse[responseID] = reservation
	return reservation, true
}

func (b *realtimeWSEmployeeTurnBindings) take(responseID string) *service.InflightReservation {
	b.mu.Lock()
	defer b.mu.Unlock()
	responseID = strings.TrimSpace(responseID)
	reservation := b.byResponse[responseID]
	delete(b.byResponse, responseID)
	return reservation
}

func (b *realtimeWSEmployeeTurnBindings) drain() (map[string]*service.InflightReservation, []*service.InflightReservation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	responses := b.byResponse
	prepared := b.prepared
	b.byResponse = nil
	b.prepared = nil
	return responses, prepared
}

func cleanupAcceptedRealtimeCallAfterWebhookCommitFailure(terminator officialRealtimeCallTerminator, account *service.Account, record *service.RealtimeCallRecord) {
	if terminator == nil || account == nil || record == nil {
		return
	}
	_ = terminator.ControlOfficialRealtimeCall(context.Background(), account, record.CallID, "hangup", "")
	terminator.FinalizeOfficialRealtimeCall(record)
}

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
	pricingCtx, pricingAt := service.WithGatewayTokenRequestPricing(c.Request.Context())
	c.Request = c.Request.WithContext(pricingCtx)
	reservationDone, err := reserveRealtimeSessionInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(model, nil))
	if err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	defer reservationDone()
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
	// Realtime GA sends session.created before clients need to send a frame.
	// The first auditable client event supplies the request hash; event payloads
	// are never logged or retained after the audit callback returns.
	var auditMu sync.Mutex
	auditTurn := 0
	requestPayloadHash := ""
	var prepareEmployeeTurn func(string) error
	auditClientFrame := func(frameCtx context.Context, payload []byte) error {
		auditMu.Lock()
		defer auditMu.Unlock()
		auditTurn++
		if requestPayloadHash == "" {
			requestPayloadHash = service.HashUsageRequestPayload(payload)
		}
		c.Set(securityAuditWSTurnContextKey, auditTurn)
		stage := "subsequent_turn"
		if auditTurn == 1 {
			stage = "first_turn"
		}
		if decision := h.checkSecurityAuditStage(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, model, payload, stage); decision != nil && !decision.AllowNextStage {
			writeSecurityAuditWSError(frameCtx, client, decision)
			return service.NewOpenAIWSClientCloseError(securityAuditWSCloseStatus(decision), securityAuditWSCloseReason(decision), nil)
		}
		if realtimeResponseCreateFrame(payload) && prepareEmployeeTurn != nil {
			return prepareEmployeeTurn(strings.TrimSpace(gjson.GetBytes(payload, "response.model").String()))
		}
		return nil
	}
	requestHash := func() string {
		auditMu.Lock()
		defer auditMu.Unlock()
		return requestPayloadHash
	}
	attribution := realtimeEnterpriseAttribution(apiKey, subscription)
	account := selection.Account
	employeeTurns := &realtimeWSEmployeeTurnBindings{}
	defer func() {
		responses, prepared := employeeTurns.drain()
		for responseID, reservation := range responses {
			if reservation != nil {
				billingCtx := service.WithInflightReservation(c.Request.Context(), reservation)
				if err := h.gatewayService.RecordUsage(billingCtx, &service.OpenAIRecordUsageInput{
					Result: &service.OpenAIForwardResult{
						RequestID:        "realtime-conservative:" + responseID,
						ResponseID:       responseID,
						Model:            model,
						Stream:           true,
						OpenAIRealtime:   true,
						UpstreamEndpoint: "/v1/realtime",
					},
					APIKey: apiKey, User: apiKey.User, Account: account, Subscription: subscription,
					InboundEndpoint: GetInboundEndpoint(c), UpstreamEndpoint: "/v1/realtime",
					UserAgent: c.GetHeader("User-Agent"), IPAddress: ip.GetClientIP(c), APIKeyService: h.apiKeyService,
					QuotaPlatform: service.QuotaPlatform(c.Request.Context(), apiKey), PricingAt: pricingAt,
					EnterpriseAttribution: attribution, ConservativeCostUSD: reservation.Amount(),
				}); err != nil {
					reqLog.Error("official_realtime_conservative_usage_failed", zap.Int64("account_id", account.ID), zap.String("response_id", responseID), zap.Error(err))
				}
				reservation.HandlerDone()
			}
		}
		for _, reservation := range prepared {
			if reservation != nil {
				reservation.HandlerDone()
			}
		}
	}()
	prepareEmployeeTurn = func(responseModel string) error {
		if !apiKey.EnterpriseEmployeeWeeklyLimit {
			return nil
		}
		req := tokenInflightEstimate(firstNonEmptyRealtime(responseModel, model), nil)
		req.RequestID = "realtime-ws:preflight:" + uuid.NewString()
		employee, reserveErr := h.gatewayService.ReserveEnterpriseEmployeeUsage(c.Request.Context(), apiKey, req)
		if reserveErr != nil {
			return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "billing check failed", reserveErr)
		}
		handle := service.AttachEnterpriseEmployeeUsageReservation(nil, employee)
		if !employeeTurns.prepare(handle) {
			handle.HandlerDone()
		}
		return nil
	}
	if err := prepareEmployeeTurn(model); err != nil {
		var closeErr *service.OpenAIWSClientCloseError
		if errors.As(err, &closeErr) {
			closeOpenAIClientWS(client, closeErr.StatusCode(), closeErr.Reason())
		}
		return
	}
	proxyErr := h.gatewayService.ProxyOfficialRealtimeWebSocket(c.Request.Context(), client, account, model, func(frameCtx context.Context, payload []byte) error {
		if err := auditClientFrame(frameCtx, payload); err != nil {
			return err
		}
		return nil
	},
		func(turn openaiwsv2.RelayTurnResult) {
			turnReservation := employeeTurns.take(turn.RequestID)
			if turnReservation != nil {
				defer turnReservation.HandlerDone()
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
			billingCtx := c.Request.Context()
			if turnReservation != nil {
				billingCtx = service.WithInflightReservation(billingCtx, turnReservation)
			}
			conservativeCost := float64(0)
			if !realtimeUsageHasTokens(turn.Usage) {
				if reservation := service.InflightReservationFromContext(billingCtx); reservation != nil {
					conservativeCost = reservation.Amount()
				}
			}
			if err := h.gatewayService.RecordUsage(billingCtx, &service.OpenAIRecordUsageInput{
				Result:                result,
				APIKey:                apiKey,
				User:                  apiKey.User,
				Account:               account,
				Subscription:          subscription,
				InboundEndpoint:       GetInboundEndpoint(c),
				UpstreamEndpoint:      "/v1/realtime",
				UserAgent:             c.GetHeader("User-Agent"),
				IPAddress:             ip.GetClientIP(c),
				RequestPayloadHash:    requestHash(),
				APIKeyService:         h.apiKeyService,
				QuotaPlatform:         service.QuotaPlatform(c.Request.Context(), apiKey),
				PricingAt:             firstNonZeroRealtimeTime(turn.StartedAt, pricingAt),
				EnterpriseAttribution: attribution,
				ConservativeCostUSD:   conservativeCost,
			}); err != nil {
				reqLog.Error("official_realtime_record_usage_failed", zap.Int64("account_id", account.ID), zap.String("request_id", turn.RequestID), zap.Error(err))
			}
		}, func(payload []byte) error {
			if strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "input_audio_buffer.speech_started" && !employeeTurns.hasPrepared() {
				return prepareEmployeeTurn("")
			}
			return nil
		}, func(responseID, _ string) error {
			if _, ok := employeeTurns.bindPrepared(responseID); !ok && apiKey.EnterpriseEmployeeWeeklyLimit {
				return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "billing check failed", service.ErrRealtimeUnavailable)
			}
			return nil
		})
	if proxyErr == nil || openAIWSIngressEndedByClient(proxyErr) {
		_ = client.Close(coderws.StatusNormalClosure, "")
		return
	}
	var closeErr *service.OpenAIWSClientCloseError
	if errors.As(proxyErr, &closeErr) {
		closeOpenAIClientWS(client, closeErr.StatusCode(), closeErr.Reason())
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
	pricingCtx, pricingAt := service.WithGatewayTokenRequestPricing(c.Request.Context())
	c.Request = c.Request.WithContext(pricingCtx)
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
	reservationDone, err := reserveRealtimeSessionInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(model, request.Session))
	if err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	defer reservationDone()
	sessionReservationRelease := inflightNoop
	if reservation := service.InflightReservationFromContext(c.Request.Context()); reservation != nil {
		sessionReservationRelease = reservation.Acquire()
	}
	sessionReservationOwnedByHandler := true
	defer func() {
		if sessionReservationOwnedByHandler {
			sessionReservationRelease()
		}
	}()
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
	identity := realtimeCallIdentity(c, apiKey, subject.UserID, subscription, model, pricingAt)
	if reservation := service.InflightReservationFromContext(c.Request.Context()); reservation != nil {
		identity.ReservationUSD = reservation.Amount()
		identity.ReservationRequestID, identity.ReservationHasBalance, identity.ReservationScopes = reservation.Snapshot()
	}
	created, record, err := h.gatewayService.CreateOfficialRealtimeTrackedCall(
		c.Request.Context(), selection.Account, request, identity, subject.Concurrency, sessionReservationRelease,
	)
	if err != nil {
		h.writeOfficialRealtimeError(c, err)
		return
	}
	sessionReservationOwnedByHandler = false
	if err := h.gatewayService.PrepareOfficialRealtimeEmployeeTurn(record, model, h.realtimeTurnStart(record, apiKey)); err != nil {
		_ = h.gatewayService.ControlOfficialRealtimeCall(context.Background(), selection.Account, record.CallID, "hangup", "")
		h.gatewayService.FinalizeOfficialRealtimeCall(record)
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	c.Header("Location", officialRealtimeSidebandLocation(created.CallID))
	c.Header("X-Realtime-Call-ID", created.CallID)
	c.Data(created.StatusCode, "application/sdp", created.SDP)
}

func officialRealtimeSidebandLocation(callID string) string {
	return "/v1/realtime/calls/" + url.PathEscape(callID)
}

// OfficialRealtimeSideband 转发已认证 WebRTC 调用的服务端控制通道。计费身份和
// 定价信息始终以调用账本而不是当前请求参数为准。
func (h *OpenAIGatewayHandler) OfficialRealtimeSideband(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	if apiKey.Group == nil || (apiKey.Group.Platform != service.PlatformOpenAI && apiKey.Group.Platform != service.PlatformComposite) || !apiKey.Group.AllowRealtime {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "Official Realtime is not enabled for this group")
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	identity := realtimeCallIdentity(c, apiKey, subject.UserID, subscription, "", time.Now())
	record, err := h.gatewayService.GetOfficialRealtimeCallForIdentity(c.Request.Context(), c.Param("call_id"), identity)
	if err != nil {
		if errors.Is(err, service.ErrRealtimeIdentityMismatch) {
			h.errorResponse(c, http.StatusForbidden, "permission_error", "Realtime call belongs to another identity")
			return
		}
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Realtime call not found")
		return
	}
	if record.InboundEndpoint == "/v1/realtime/webhooks/openai" {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "SIP Realtime calls use a server-managed sideband")
		return
	}
	if h.apiKeyService == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Billing service unavailable")
		return
	}
	billingAPIKey, err := h.apiKeyService.GetByID(c.Request.Context(), record.APIKeyID)
	billingGroupID := int64(0)
	if billingAPIKey != nil && billingAPIKey.GroupID != nil {
		billingGroupID = *billingAPIKey.GroupID
	}
	if err != nil || billingAPIKey == nil || billingAPIKey.User == nil || billingAPIKey.UserID != record.UserID || billingGroupID != record.GroupID {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Realtime billing identity unavailable")
		return
	}
	account, err := h.gatewayService.GetOfficialRealtimeCallAccount(c.Request.Context(), record)
	if err != nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Realtime account unavailable")
		return
	}
	downstream, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = downstream.CloseNow() }()
	reqLog := requestLogger(c, "handler.openai_gateway.official_realtime_sideband",
		zap.Int64("user_id", record.UserID), zap.Int64("api_key_id", record.APIKeyID), zap.Int64("account_id", record.AccountID))
	if err := h.gatewayService.ProxyOfficialRealtimeSideband(c.Request.Context(), record, downstream, h.realtimeTurnStart(record, billingAPIKey), func(turn openaiwsv2.RelayTurnResult) error {
		if !realtimeUsageHasTokens(turn.Usage) {
			return h.recordConservativeRealtimeTurnUsage(record, turn.RequestID)
		}
		result := &service.OpenAIForwardResult{
			RequestID:             turn.RequestID,
			ResponseID:            turn.RequestID,
			Usage:                 realtimeUsage(turn.Usage),
			Model:                 firstNonEmptyRealtime(turn.RequestModel, record.Model),
			UpstreamResponseModel: turn.ResponseModel,
			Stream:                true,
			OpenAIRealtime:        true,
			UpstreamEndpoint:      "/v1/realtime",
			UpstreamTerminalEvent: turn.TerminalEventType,
			Duration:              turn.Duration,
			FirstTokenMs:          turn.FirstTokenMs,
		}
		var frozenSubscription *service.UserSubscription
		if record.SubscriptionID > 0 {
			if h.subscriptionService == nil {
				return errors.New("realtime frozen subscription service unavailable")
			}
			var err error
			frozenSubscription, err = h.subscriptionService.GetByID(c.Request.Context(), record.SubscriptionID)
			if err != nil || frozenSubscription == nil || frozenSubscription.UserID != record.UserID || frozenSubscription.GroupID != record.GroupID {
				return errors.New("realtime frozen subscription unavailable")
			}
		}
		billingCtx, billingDone, err := h.realtimeTurnBillingContext(c.Request.Context(), record, billingAPIKey, turn)
		if err != nil {
			return err
		}
		defer billingDone()
		if err := h.gatewayService.RecordUsage(billingCtx, &service.OpenAIRecordUsageInput{
			Result:                result,
			APIKey:                billingAPIKey,
			User:                  billingAPIKey.User,
			Account:               account,
			Subscription:          frozenSubscription,
			InboundEndpoint:       record.InboundEndpoint,
			UpstreamEndpoint:      "/v1/realtime",
			UserAgent:             record.UserAgent,
			IPAddress:             record.IPAddress,
			APIKeyService:         h.apiKeyService,
			QuotaPlatform:         service.QuotaPlatform(c.Request.Context(), billingAPIKey),
			PricingAt:             record.PricingAt,
			EnterpriseAttribution: record.EnterpriseAttribution,
			RealtimeCallHash:      record.CallHash,
		}); err != nil {
			reqLog.Error("official_realtime_sideband_record_usage_failed", zap.String("request_id", turn.RequestID), zap.Error(err))
			return err
		}
		return nil
	}); err != nil {
		if errors.Is(err, service.ErrRealtimeControllerChanged) {
			_ = downstream.Close(coderws.StatusPolicyViolation, "realtime sideband controller unavailable")
			return
		}
		reqLog.Warn("official_realtime_sideband_proxy_failed", zap.Error(err))
		_ = downstream.Close(coderws.StatusInternalError, "official realtime sideband closed")
		return
	}
	_ = downstream.Close(coderws.StatusNormalClosure, "")
}

// OfficialRealtimeSIPWebhook processes OpenAI's signed realtime.call.incoming
// delivery. SIP headers are intentionally ignored: only the configured binding
// may select the API key, account, model, or tenant to be billed.
func (h *OpenAIGatewayHandler) OfficialRealtimeSIPWebhook(c *gin.Context) {
	if c.Request == nil || h == nil || h.cfg == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	config := h.cfg.Gateway.Realtime.SIP
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, realtimeWebhookBodyLimit+1))
	if err != nil || len(body) > realtimeWebhookBodyLimit {
		c.Status(http.StatusBadRequest)
		return
	}
	if !verifyOfficialRealtimeWebhook(config.WebhookSecret, c.GetHeader("webhook-id"), c.GetHeader("webhook-timestamp"), c.Request.Header.Values("webhook-signature"), body, realtimeWebhookMaxAge(config.WebhookMaxAgeSeconds), time.Now()) {
		c.Status(http.StatusUnauthorized)
		return
	}
	var event officialRealtimeWebhookEvent
	if err := json.Unmarshal(body, &event); err != nil || strings.TrimSpace(event.Data.CallID) == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	if event.Type != "realtime.call.incoming" {
		c.Status(http.StatusNoContent)
		return
	}
	if h.gatewayService == nil || h.apiKeyService == nil || config.APIKeyID <= 0 || config.AccountID <= 0 || strings.TrimSpace(config.Model) == "" {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	store, err := h.gatewayService.RealtimeCallStore()
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	webhookID := c.GetHeader("webhook-id")
	webhookOwner := uuid.NewString()
	commitWebhook := func(ctx context.Context) error {
		return store.CommitRealtimeWebhook(ctx, webhookID, realtimeWebhookReplayTTL)
	}
	releaseWebhook := func(ctx context.Context) {
		_ = store.ReleaseRealtimeWebhook(ctx, webhookID)
	}
	var claimed bool
	if claims, ok := store.(service.RealtimeUsageClaimStore); ok {
		claimed, err = claims.ClaimRealtimeWebhook(c.Request.Context(), webhookID, webhookOwner, realtimeWebhookClaimLease)
		commitWebhook = func(ctx context.Context) error {
			committed, commitErr := claims.CommitRealtimeWebhookClaim(ctx, webhookID, webhookOwner, realtimeWebhookReplayTTL)
			if commitErr != nil {
				return commitErr
			}
			if !committed {
				return errors.New("realtime webhook claim is no longer owned")
			}
			return nil
		}
		releaseWebhook = func(ctx context.Context) {
			_, _ = claims.ReleaseRealtimeWebhookClaim(ctx, webhookID, webhookOwner)
		}
	} else {
		claimed, err = store.BeginRealtimeWebhook(c.Request.Context(), webhookID, realtimeWebhookReplayTTL)
	}
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if !claimed {
		c.Status(http.StatusNoContent)
		return
	}
	committed := false
	defer func() {
		if !committed {
			releaseWebhook(context.Background())
		}
	}()
	account, accountErr := h.gatewayService.GetOfficialRealtimeAccountByID(c.Request.Context(), config.AccountID)
	if accountErr != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	reject := func() {
		_ = h.gatewayService.ControlOfficialRealtimeCall(context.Background(), account, event.Data.CallID, "reject", "")
	}
	apiKey, err := h.apiKeyService.GetByID(c.Request.Context(), config.APIKeyID)
	if err != nil || !validRealtimeSIPBinding(apiKey, config.Model) {
		reject()
		c.Status(http.StatusForbidden)
		return
	}
	var subscription *service.UserSubscription
	if h.subscriptionService != nil && apiKey.GroupID != nil {
		subscription, err = h.subscriptionService.GetActiveSubscription(c.Request.Context(), apiKey.UserID, *apiKey.GroupID)
		if err != nil && !errors.Is(err, service.ErrSubscriptionNotFound) {
			reject()
			c.Status(http.StatusServiceUnavailable)
			return
		}
	}
	if h.billingCacheService == nil || h.concurrencyHelper == nil || apiKey.User == nil {
		reject()
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reject()
		c.Status(http.StatusForbidden)
		return
	}
	pricingCtx, pricingAt := service.WithGatewayTokenRequestPricing(c.Request.Context())
	pricingCtx, reservationDone, err := reserveRealtimeSessionInflightBalanceCtx(pricingCtx, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(config.Model, nil))
	if err != nil {
		reject()
		c.Status(http.StatusPaymentRequired)
		return
	}
	defer reservationDone()
	sessionReservationRelease := inflightNoop
	if reservation := service.InflightReservationFromContext(pricingCtx); reservation != nil {
		sessionReservationRelease = reservation.Acquire()
	}
	sessionReservationOwned := true
	defer func() {
		if sessionReservationOwned {
			sessionReservationRelease()
		}
	}()
	identity := service.RealtimeCallIdentity{
		APIKeyID: apiKey.ID, UserID: apiKey.UserID, GroupID: apiKey.GroupID,
		Model: config.Model, PricingAt: pricingAt, UserAgent: "openai-webhook", IPAddress: ip.GetClientIP(c),
		InboundEndpoint: "/v1/realtime/webhooks/openai", EnterpriseAttribution: realtimeEnterpriseAttributionAt(apiKey, subscription, pricingAt),
	}
	if reservation := service.InflightReservationFromContext(pricingCtx); reservation != nil {
		identity.ReservationUSD = reservation.Amount()
		identity.ReservationRequestID, identity.ReservationHasBalance, identity.ReservationScopes = reservation.Snapshot()
	}
	if subscription != nil {
		identity.SubscriptionID = &subscription.ID
	}
	record, err := h.gatewayService.RegisterOfficialRealtimeIncomingCall(pricingCtx, account, event.Data.CallID, identity, apiKey.User.Concurrency, sessionReservationRelease)
	if err != nil {
		reject()
		h.writeOfficialRealtimeError(c, err)
		return
	}
	sessionReservationOwned = false
	if err := h.gatewayService.PrepareOfficialRealtimeEmployeeTurn(record, config.Model, h.realtimeTurnStart(record, apiKey)); err != nil {
		h.gatewayService.FinalizeOfficialRealtimeCall(record)
		reject()
		c.Status(http.StatusPaymentRequired)
		return
	}
	if err := h.gatewayService.ControlOfficialRealtimeCall(c.Request.Context(), account, record.CallID, "accept", record.Model); err != nil {
		h.gatewayService.FinalizeOfficialRealtimeCall(record)
		reject()
		c.Status(http.StatusBadGateway)
		return
	}
	if err := commitWebhook(c.Request.Context()); err != nil {
		cleanupAcceptedRealtimeCallAfterWebhookCommitFailure(h.gatewayService, account, record)
		c.Status(http.StatusServiceUnavailable)
		return
	}
	committed = true
	go h.observeOfficialRealtimeSIPCall(record, apiKey, account, subscription)
	c.Status(http.StatusNoContent)
}

func (h *OpenAIGatewayHandler) observeOfficialRealtimeSIPCall(record *service.RealtimeCallRecord, apiKey *service.APIKey, account *service.Account, subscription *service.UserSubscription) {
	if h == nil || h.gatewayService == nil || record == nil || apiKey == nil || apiKey.User == nil || account == nil {
		return
	}
	h.gatewayService.ObserveOfficialRealtimeCall(record, h.realtimeTurnStart(record, apiKey), func(turn openaiwsv2.RelayTurnResult) error {
		if !realtimeUsageHasTokens(turn.Usage) {
			return h.recordConservativeRealtimeTurnUsage(record, turn.RequestID)
		}
		result := &service.OpenAIForwardResult{
			RequestID:             turn.RequestID,
			ResponseID:            turn.RequestID,
			Usage:                 realtimeUsage(turn.Usage),
			Model:                 firstNonEmptyRealtime(turn.ResponseModel, record.Model),
			UpstreamResponseModel: turn.ResponseModel,
			Stream:                true,
			OpenAIRealtime:        true,
			UpstreamEndpoint:      "/v1/realtime",
			UpstreamTerminalEvent: turn.TerminalEventType,
			Duration:              turn.Duration,
			FirstTokenMs:          turn.FirstTokenMs,
		}
		frozenSubscription := subscription
		if record.SubscriptionID > 0 {
			if h.subscriptionService == nil {
				return errors.New("realtime frozen subscription service unavailable")
			}
			var err error
			frozenSubscription, err = h.subscriptionService.GetByID(context.Background(), record.SubscriptionID)
			if err != nil || frozenSubscription == nil || frozenSubscription.UserID != record.UserID || frozenSubscription.GroupID != record.GroupID {
				return errors.New("realtime frozen subscription unavailable")
			}
		}
		billingCtx, billingDone, err := h.realtimeTurnBillingContext(context.Background(), record, apiKey, turn)
		if err != nil {
			return err
		}
		defer billingDone()
		if err := h.gatewayService.RecordUsage(billingCtx, &service.OpenAIRecordUsageInput{
			Result:                result,
			APIKey:                apiKey,
			User:                  apiKey.User,
			Account:               account,
			Subscription:          frozenSubscription,
			InboundEndpoint:       record.InboundEndpoint,
			UpstreamEndpoint:      "/v1/realtime",
			UserAgent:             record.UserAgent,
			IPAddress:             record.IPAddress,
			APIKeyService:         h.apiKeyService,
			QuotaPlatform:         service.QuotaPlatform(context.Background(), apiKey),
			PricingAt:             record.PricingAt,
			EnterpriseAttribution: record.EnterpriseAttribution,
			RealtimeCallHash:      record.CallHash,
		}); err != nil {
			logger.L().Warn("official_realtime_sip_record_usage_failed", zap.String("request_id", turn.RequestID), zap.Error(err))
			return err
		}
		return nil
	})
}

func (h *OpenAIGatewayHandler) observeRecoveredOfficialRealtimeCall(record *service.RealtimeCallRecord) {
	apiKey, account, subscription, err := h.loadFrozenRealtimeIdentity(record)
	if err != nil {
		return
	}
	h.observeOfficialRealtimeSIPCall(record, apiKey, account, subscription)
}

func (h *OpenAIGatewayHandler) recordConservativeRealtimeUsage(record *service.RealtimeCallRecord) error {
	apiKey, account, subscription, err := h.loadFrozenRealtimeIdentity(record)
	if err != nil {
		return err
	}
	return h.gatewayService.RecordUsage(context.Background(), &service.OpenAIRecordUsageInput{
		Result: &service.OpenAIForwardResult{
			RequestID:        "realtime-conservative:" + record.CallHash,
			ResponseID:       "realtime-conservative:" + record.CallHash,
			Model:            record.Model,
			Stream:           true,
			OpenAIRealtime:   true,
			UpstreamEndpoint: "/v1/realtime",
		},
		APIKey:                apiKey,
		User:                  apiKey.User,
		Account:               account,
		Subscription:          subscription,
		InboundEndpoint:       record.InboundEndpoint,
		UpstreamEndpoint:      "/v1/realtime",
		UserAgent:             record.UserAgent,
		IPAddress:             record.IPAddress,
		APIKeyService:         h.apiKeyService,
		QuotaPlatform:         service.QuotaPlatform(context.Background(), apiKey),
		PricingAt:             record.PricingAt,
		EnterpriseAttribution: record.EnterpriseAttribution,
		ConservativeCostUSD:   record.ReservationUSD,
	})
}

func (h *OpenAIGatewayHandler) recordConservativeRealtimeTurnUsage(record *service.RealtimeCallRecord, responseID string) error {
	apiKey, account, subscription, err := h.loadFrozenRealtimeIdentity(record)
	if err != nil {
		return err
	}
	turn := openaiwsv2.RelayTurnResult{RequestID: responseID, RequestModel: record.Model}
	billingCtx, billingDone, err := h.realtimeTurnBillingContext(context.Background(), record, apiKey, turn)
	if err != nil {
		return err
	}
	defer billingDone()
	reservation := service.InflightReservationFromContext(billingCtx)
	if reservation == nil || reservation.Amount() <= 0 {
		return errors.New("realtime employee turn reservation unavailable")
	}
	return h.gatewayService.RecordUsage(billingCtx, &service.OpenAIRecordUsageInput{
		Result: &service.OpenAIForwardResult{
			RequestID:  "realtime-conservative:" + record.CallHash + ":" + responseID,
			ResponseID: responseID, Model: record.Model, Stream: true, OpenAIRealtime: true,
			UpstreamEndpoint: "/v1/realtime",
		},
		APIKey: apiKey, User: apiKey.User, Account: account, Subscription: subscription,
		InboundEndpoint: record.InboundEndpoint, UpstreamEndpoint: "/v1/realtime",
		UserAgent: record.UserAgent, IPAddress: record.IPAddress, APIKeyService: h.apiKeyService,
		QuotaPlatform: service.QuotaPlatform(context.Background(), apiKey), PricingAt: record.PricingAt,
		EnterpriseAttribution: record.EnterpriseAttribution, RealtimeCallHash: record.CallHash,
		ConservativeCostUSD: reservation.Amount(),
	})
}

func (h *OpenAIGatewayHandler) loadFrozenRealtimeIdentity(record *service.RealtimeCallRecord) (*service.APIKey, *service.Account, *service.UserSubscription, error) {
	if h == nil || h.gatewayService == nil || h.apiKeyService == nil || record == nil {
		return nil, nil, nil, errors.New("realtime frozen identity dependencies unavailable")
	}
	apiKey, err := h.apiKeyService.GetByID(context.Background(), record.APIKeyID)
	if err != nil || apiKey == nil || apiKey.User == nil || apiKey.UserID != record.UserID || apiKey.GroupID == nil || *apiKey.GroupID != record.GroupID {
		return nil, nil, nil, errors.New("realtime frozen API key unavailable")
	}
	account, err := h.gatewayService.GetOfficialRealtimeCallAccount(context.Background(), record)
	if err != nil || account == nil {
		return nil, nil, nil, errors.New("realtime frozen account unavailable")
	}
	var subscription *service.UserSubscription
	if record.SubscriptionID > 0 {
		if h.subscriptionService == nil {
			return nil, nil, nil, errors.New("realtime frozen subscription service unavailable")
		}
		subscription, err = h.subscriptionService.GetByID(context.Background(), record.SubscriptionID)
		if err != nil || subscription == nil || subscription.UserID != record.UserID || subscription.GroupID != record.GroupID {
			return nil, nil, nil, errors.New("realtime frozen subscription unavailable")
		}
	}
	return apiKey, account, subscription, nil
}

// OfficialRealtimeHangup terminates a call for its authenticated owner. A
// failed upstream hangup leaves the ledger active for lease-loss/timeout cleanup.
func (h *OpenAIGatewayHandler) OfficialRealtimeHangup(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	identity := realtimeCallIdentity(c, apiKey, apiKey.UserID, subscription, "", time.Now())
	record, err := h.gatewayService.GetOfficialRealtimeCallForIdentity(c.Request.Context(), c.Param("call_id"), identity)
	if err != nil {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Realtime call not found")
		return
	}
	account, err := h.gatewayService.GetOfficialRealtimeCallAccount(c.Request.Context(), record)
	if err != nil || h.gatewayService.ControlOfficialRealtimeCall(c.Request.Context(), account, record.CallID, "hangup", "") != nil {
		h.errorResponse(c, http.StatusBadGateway, "api_error", "Official Realtime hangup failed")
		return
	}
	h.gatewayService.FinalizeOfficialRealtimeCall(record)
	c.Status(http.StatusNoContent)
}

func realtimeWebhookMaxAge(value int) time.Duration {
	if value <= 0 {
		return defaultRealtimeWebhookAge
	}
	return time.Duration(value) * time.Second
}

func validRealtimeSIPBinding(apiKey *service.APIKey, model string) bool {
	return apiKey != nil && apiKey.User != nil && apiKey.Group != nil && apiKey.Group.AllowRealtime &&
		(apiKey.Group.Platform == service.PlatformOpenAI || apiKey.Group.Platform == service.PlatformComposite) &&
		strings.TrimSpace(model) != "" && blockedModelAllowlistCandidate(apiKey.Group, []string{model}) == ""
}

func verifyOfficialRealtimeWebhook(secret, webhookID, timestamp string, signatures []string, body []byte, maxAge time.Duration, now time.Time) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" || webhookID == "" || timestamp == "" || len(signatures) == 0 || maxAge <= 0 {
		return false
	}
	key := []byte(secret)
	if strings.HasPrefix(secret, "whsec_") {
		var err error
		key, err = base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
		if err != nil || len(key) == 0 {
			return false
		}
	}
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	when := time.Unix(unix, 0)
	if err != nil || now.Sub(when) > maxAge || when.Sub(now) > maxAge {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(webhookID + "." + timestamp + "."))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	for _, header := range signatures {
		for _, value := range strings.Fields(header) {
			parts := strings.Split(value, ",")
			for i := 0; i+1 < len(parts); i += 2 {
				if strings.TrimSpace(parts[i]) != "v1" {
					continue
				}
				actual, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[i+1]))
				if err == nil && hmac.Equal(expected, actual) {
					return true
				}
			}
		}
	}
	return false
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
	return realtimeEnterpriseAttributionAt(apiKey, subscription, time.Now())
}

func realtimeEnterpriseAttributionAt(apiKey *service.APIKey, subscription *service.UserSubscription, at time.Time) *service.EnterpriseUsageAttributionSnapshot {
	if apiKey == nil || subscription == nil {
		return nil
	}
	return service.SnapshotEnterpriseUsageAttribution(apiKey.EnterpriseAttributionIdentity, at, subscription.DailyWindowStart, subscription.WeeklyWindowStart, subscription.MonthlyWindowStart)
}

func realtimeCallIdentity(c *gin.Context, apiKey *service.APIKey, userID int64, subscription *service.UserSubscription, model string, pricingAt time.Time) service.RealtimeCallIdentity {
	var subscriptionID *int64
	if subscription != nil {
		value := subscription.ID
		subscriptionID = &value
	}
	return service.RealtimeCallIdentity{
		APIKeyID:              apiKey.ID,
		UserID:                userID,
		GroupID:               apiKey.GroupID,
		SubscriptionID:        subscriptionID,
		EnterpriseAttribution: realtimeEnterpriseAttributionAt(apiKey, subscription, pricingAt),
		Model:                 model,
		PricingAt:             pricingAt,
		UserAgent:             c.GetHeader("User-Agent"),
		IPAddress:             ip.GetClientIP(c),
		InboundEndpoint:       GetInboundEndpoint(c),
		ReservationUSD:        realtimeReservationUSD(c.Request.Context()),
	}
}

func realtimeReservationUSD(ctx context.Context) float64 {
	if reservation := service.InflightReservationFromContext(ctx); reservation != nil {
		return reservation.Amount()
	}
	return 0
}

func realtimeUsage(usage openaiwsv2.Usage) service.OpenAIUsage {
	return service.OpenAIUsage{
		InputTokens:              usage.InputTokens,
		ImageInputTokens:         usage.ImageInputTokens,
		AudioInputTokens:         usage.AudioInputTokens,
		ImageCacheReadTokens:     usage.ImageCacheReadTokens,
		AudioCacheReadTokens:     usage.AudioCacheReadTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		ImageOutputTokens:        usage.ImageOutputTokens,
		AudioOutputTokens:        usage.AudioOutputTokens,
		TokenBucketsSeparated:    usage.TokenBucketsSeparated,
	}
}

func realtimeUsageHasTokens(usage openaiwsv2.Usage) bool {
	return usage.InputTokens > 0 || usage.OutputTokens > 0 ||
		usage.ImageInputTokens > 0 || usage.ImageCacheReadTokens > 0 || usage.ImageOutputTokens > 0 ||
		usage.AudioInputTokens > 0 || usage.AudioCacheReadTokens > 0 || usage.AudioOutputTokens > 0 ||
		usage.CacheCreationInputTokens > 0 || usage.CacheReadInputTokens > 0
}

func realtimeResponseCreateFrame(payload []byte) bool {
	return strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "response.create"
}

// realtimeTurnBillingContext binds one terminal Realtime response to one durable
// enterprise employee reservation. The identifier is stable across sideband
// reconnects and SIP observer recovery, while remaining distinct for every
// upstream response in the call.
func (h *OpenAIGatewayHandler) realtimeTurnBillingContext(ctx context.Context, record *service.RealtimeCallRecord, apiKey *service.APIKey, turn openaiwsv2.RelayTurnResult) (context.Context, func(), error) {
	if h == nil || h.gatewayService == nil || record == nil || apiKey == nil || !apiKey.EnterpriseEmployeeWeeklyLimit {
		return ctx, inflightNoop, nil
	}
	responseID := strings.TrimSpace(turn.RequestID)
	if responseID == "" {
		return ctx, inflightNoop, errors.New("realtime usage response id is required")
	}
	if reservation := h.gatewayService.RealtimeEmployeeTurnReservation(record, responseID); reservation != nil {
		return service.WithInflightReservation(ctx, reservation), inflightNoop, nil
	}
	model := firstNonEmptyRealtime(turn.RequestModel, turn.ResponseModel, record.Model)
	req := tokenInflightEstimate(model, nil)
	req.RequestID = "realtime-call:" + record.CallHash + ":" + responseID
	employee, err := h.gatewayService.ReserveEnterpriseEmployeeUsage(ctx, apiKey, req)
	if err != nil {
		return ctx, inflightNoop, err
	}
	reservation := service.AttachEnterpriseEmployeeUsageReservation(nil, employee)
	return service.WithInflightReservation(ctx, reservation), reservation.HandlerDone, nil
}

func (h *OpenAIGatewayHandler) realtimeTurnStart(record *service.RealtimeCallRecord, apiKey *service.APIKey) service.RealtimeTurnStartFunc {
	if h == nil || h.gatewayService == nil || record == nil || apiKey == nil || !apiKey.EnterpriseEmployeeWeeklyLimit {
		return nil
	}
	return func(responseID, responseModel string) (*service.InflightReservation, error) {
		model := firstNonEmptyRealtime(responseModel, record.Model)
		req := tokenInflightEstimate(model, nil)
		req.RequestID = "realtime-call:" + record.CallHash + ":" + strings.TrimSpace(responseID)
		employee, err := h.gatewayService.ReserveEnterpriseEmployeeUsage(context.Background(), apiKey, req)
		if err != nil {
			return nil, err
		}
		return service.AttachEnterpriseEmployeeUsageReservation(nil, employee), nil
	}
}

func firstNonEmptyRealtime(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func firstNonZeroRealtimeTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func (h *OpenAIGatewayHandler) writeOfficialRealtimeError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrRealtimeConcurrencyFull) {
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Realtime concurrency limit reached")
		return
	}
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
