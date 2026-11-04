package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const (
	defaultRealtimeMaxSessionDuration = time.Hour
	realtimeLeaseRefreshInterval      = 20 * time.Second
	realtimeClosedRecordTTL           = 24 * time.Hour
	realtimeOperationTimeout          = 3 * time.Second
	realtimeObserverPollInterval      = 10 * time.Second
	realtimeObserverRetryInterval     = time.Second
	realtimeReplayClaimLease          = 30 * time.Second
)

var errRealtimeObserverSessionEnded = errors.New("official realtime session ended")

type realtimeLifecycleCallbacks struct {
	observe            func(*RealtimeCallRecord)
	settleConservative func(*RealtimeCallRecord) error
}

type realtimeUsageClaim struct {
	commit  func(context.Context) error
	release func(context.Context)
}

func hashRealtimeCallID(callID string) string {
	sum := sha256.Sum256([]byte(callID))
	return hex.EncodeToString(sum[:])
}

func realtimeGroupID(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *OpenAIGatewayService) realtimeStore() (RealtimeCallStore, error) {
	if s == nil || s.cache == nil {
		return nil, ErrRealtimeUnavailable
	}
	store, ok := s.cache.(RealtimeCallStore)
	if !ok {
		return nil, ErrRealtimeUnavailable
	}
	return store, nil
}

// RealtimeCallStore exposes the narrow persistence capability used by the
// signed webhook ingress. Callers cannot use it to select billing identity.
func (s *OpenAIGatewayService) RealtimeCallStore() (RealtimeCallStore, error) {
	return s.realtimeStore()
}

// SetOfficialRealtimeLifecycleCallbacks 注册 handler 所有的身份回查与结算能力，
// 并重建前一进程遗留在 Redis 中的活动会话。
func (s *OpenAIGatewayService) SetOfficialRealtimeLifecycleCallbacks(observe func(*RealtimeCallRecord), settleConservative func(*RealtimeCallRecord) error) {
	if s == nil {
		return
	}
	s.realtimeCallbacks.Store(&realtimeLifecycleCallbacks{observe: observe, settleConservative: settleConservative})
	go s.RecoverOfficialRealtimeCalls()
}

func (s *OpenAIGatewayService) realtimeCallbacksSnapshot() *realtimeLifecycleCallbacks {
	if s == nil {
		return nil
	}
	callbacks, _ := s.realtimeCallbacks.Load().(*realtimeLifecycleCallbacks)
	return callbacks
}

// RecoverOfficialRealtimeCalls 从 Redis 账本重建 maintainer 和 observer。进程退出
// 后旧 controller 不再存在，恢复前会把它重置为 pending 再争抢新的 controller。
func (s *OpenAIGatewayService) RecoverOfficialRealtimeCalls() {
	if s == nil {
		return
	}
	store, ok := s.cache.(RealtimeCallRecoveryStore)
	if !ok || store == nil {
		return
	}
	records, err := store.ListActiveRealtimeCalls(context.Background())
	if err != nil {
		return
	}
	callbacks := s.realtimeCallbacksSnapshot()
	for _, record := range records {
		if record == nil {
			continue
		}
		if resetter, ok := s.cache.(RealtimeControllerRecoveryStore); ok {
			_, _ = resetter.ResetRealtimeControllerForRecovery(context.Background(), record.CallHash)
		}
		if !time.Now().Before(record.ExpiresAt) {
			s.terminateOfficialRealtimeCall(record)
			continue
		}
		go s.maintainRealtimeCall(record)
		if callbacks != nil && callbacks.observe != nil {
			go callbacks.observe(record)
		}
	}
}

func (s *OpenAIGatewayService) claimRealtimeUsage(store RealtimeCallStore, callHash, responseID string) (*realtimeUsageClaim, bool, error) {
	if advanced, ok := store.(RealtimeUsageClaimStore); ok {
		owner := uuid.NewString()
		claimed, err := advanced.ClaimRealtimeUsage(context.Background(), callHash, responseID, owner, realtimeReplayClaimLease)
		if err != nil || !claimed {
			return nil, claimed, err
		}
		return &realtimeUsageClaim{
			commit: func(ctx context.Context) error {
				committed, err := advanced.CommitRealtimeUsageClaim(ctx, callHash, responseID, owner, realtimeClosedRecordTTL)
				if err != nil {
					return err
				}
				if !committed {
					return errors.New("realtime usage claim is no longer owned")
				}
				return nil
			},
			release: func(ctx context.Context) { _, _ = advanced.ReleaseRealtimeUsageClaim(ctx, callHash, responseID, owner) },
		}, true, nil
	}
	claimed, err := store.BeginRealtimeUsage(context.Background(), callHash, responseID, realtimeClosedRecordTTL)
	if err != nil || !claimed {
		return nil, claimed, err
	}
	return &realtimeUsageClaim{
		commit: func(ctx context.Context) error {
			return store.CommitRealtimeUsage(ctx, callHash, responseID, realtimeClosedRecordTTL)
		},
		release: func(ctx context.Context) { _ = store.ReleaseRealtimeUsage(ctx, callHash, responseID) },
	}, true, nil
}

func (s *OpenAIGatewayService) realtimeConcurrencyCache() (RealtimeConcurrencyCache, error) {
	if s == nil || s.concurrencyService == nil || s.concurrencyService.cache == nil {
		return nil, ErrRealtimeUnavailable
	}
	cache, ok := s.concurrencyService.cache.(RealtimeConcurrencyCache)
	if !ok {
		return nil, ErrRealtimeUnavailable
	}
	return cache, nil
}

func (s *OpenAIGatewayService) realtimeMaxSessionDuration() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.Realtime.MaxSessionDurationSeconds > 0 {
		return time.Duration(s.cfg.Gateway.Realtime.MaxSessionDurationSeconds) * time.Second
	}
	return defaultRealtimeMaxSessionDuration
}

// CreateOfficialRealtimeTrackedCall 在发送 offer 前将调用方持有的短期准入槽位
// 转换为会话租约。仅在持久账本保存成功后返回 SDP。
func (s *OpenAIGatewayService) CreateOfficialRealtimeTrackedCall(
	ctx context.Context,
	account *Account,
	request *RealtimeCallRequest,
	identity RealtimeCallIdentity,
	userMaxConcurrency int,
	onFinalize func(),
) (*RealtimeCallCreated, *RealtimeCallRecord, error) {
	identity.Model = strings.TrimSpace(identity.Model)
	if account == nil || identity.APIKeyID <= 0 || identity.UserID <= 0 || identity.Model == "" || identity.PricingAt.IsZero() {
		return nil, nil, ErrRealtimeUnavailable
	}
	store, err := s.realtimeStore()
	if err != nil {
		return nil, nil, err
	}
	leases, err := s.realtimeConcurrencyCache()
	if err != nil {
		return nil, nil, err
	}
	leaseID := generateRequestID()
	acquired, err := leases.AcquireRealtimeLease(ctx, account.ID, account.Concurrency, identity.UserID, userMaxConcurrency, identity.APIKeyID, leaseID, true)
	if err != nil {
		return nil, nil, err
	}
	if !acquired {
		return nil, nil, ErrRealtimeConcurrencyFull
	}
	created, err := s.CreateOfficialRealtimeCall(ctx, account, request)
	if err != nil {
		s.releaseRealtimeLease(account.ID, identity.UserID, identity.APIKeyID, leaseID)
		return nil, nil, err
	}
	now := time.Now()
	record := &RealtimeCallRecord{
		CallID:                created.CallID,
		CallHash:              hashRealtimeCallID(created.CallID),
		AccountID:             account.ID,
		APIKeyID:              identity.APIKeyID,
		UserID:                identity.UserID,
		GroupID:               realtimeGroupID(identity.GroupID),
		SubscriptionID:        realtimeGroupID(identity.SubscriptionID),
		LeaseID:               leaseID,
		Model:                 identity.Model,
		PricingAt:             identity.PricingAt,
		CreatedAt:             now,
		ExpiresAt:             now.Add(s.realtimeMaxSessionDuration()),
		Controller:            RealtimeControllerPending,
		UserAgent:             identity.UserAgent,
		IPAddress:             identity.IPAddress,
		InboundEndpoint:       identity.InboundEndpoint,
		EnterpriseAttribution: CloneEnterpriseUsageAttributionSnapshot(identity.EnterpriseAttribution),
		ReservationUSD:        identity.ReservationUSD,
	}
	if err := store.SaveRealtimeCall(ctx, record, s.realtimeMaxSessionDuration()+5*time.Minute); err != nil {
		s.releaseRealtimeLease(account.ID, identity.UserID, identity.APIKeyID, leaseID)
		return nil, nil, fmt.Errorf("save realtime call mapping: %w", err)
	}
	if onFinalize != nil {
		s.realtimeFinalizers.Store(record.CallHash, onFinalize)
	}
	go s.maintainRealtimeCall(record)
	return created, record, nil
}

// RegisterOfficialRealtimeIncomingCall creates the lifecycle ledger for an
// OpenAI-originated SIP call before it is accepted. Incoming calls have no
// short-lived gateway request slot to replace, so their long lease is acquired
// directly and remains the only concurrency representation of the call.
func (s *OpenAIGatewayService) RegisterOfficialRealtimeIncomingCall(
	ctx context.Context,
	account *Account,
	callID string,
	identity RealtimeCallIdentity,
	userMaxConcurrency int,
	onFinalize func(),
) (*RealtimeCallRecord, error) {
	callID = strings.TrimSpace(callID)
	identity.Model = strings.TrimSpace(identity.Model)
	if account == nil || callID == "" || identity.APIKeyID <= 0 || identity.UserID <= 0 || identity.Model == "" || identity.PricingAt.IsZero() {
		return nil, ErrRealtimeUnavailable
	}
	store, err := s.realtimeStore()
	if err != nil {
		return nil, err
	}
	leases, err := s.realtimeConcurrencyCache()
	if err != nil {
		return nil, err
	}
	leaseID := generateRequestID()
	acquired, err := leases.AcquireRealtimeLease(ctx, account.ID, account.Concurrency, identity.UserID, userMaxConcurrency, identity.APIKeyID, leaseID, false)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, ErrRealtimeConcurrencyFull
	}
	now := time.Now()
	record := &RealtimeCallRecord{
		CallID: callID, CallHash: hashRealtimeCallID(callID), AccountID: account.ID,
		APIKeyID: identity.APIKeyID, UserID: identity.UserID, GroupID: realtimeGroupID(identity.GroupID),
		SubscriptionID: realtimeGroupID(identity.SubscriptionID), LeaseID: leaseID, Model: identity.Model,
		PricingAt: identity.PricingAt, CreatedAt: now, ExpiresAt: now.Add(s.realtimeMaxSessionDuration()),
		Controller: RealtimeControllerPending, UserAgent: identity.UserAgent, IPAddress: identity.IPAddress,
		InboundEndpoint: identity.InboundEndpoint, EnterpriseAttribution: CloneEnterpriseUsageAttributionSnapshot(identity.EnterpriseAttribution),
		ReservationUSD: identity.ReservationUSD,
	}
	if err := store.SaveRealtimeCall(ctx, record, s.realtimeMaxSessionDuration()+5*time.Minute); err != nil {
		s.releaseRealtimeLease(account.ID, identity.UserID, identity.APIKeyID, leaseID)
		return nil, fmt.Errorf("save incoming realtime call mapping: %w", err)
	}
	if onFinalize != nil {
		s.realtimeFinalizers.Store(record.CallHash, onFinalize)
	}
	go s.maintainRealtimeCall(record)
	return record, nil
}

// ControlOfficialRealtimeCall invokes an official Calls control endpoint. The
// model is supplied only for accept, where OpenAI requires a session config.
func (s *OpenAIGatewayService) ControlOfficialRealtimeCall(ctx context.Context, account *Account, callID, action, model string) error {
	callID = strings.TrimSpace(callID)
	action = strings.TrimSpace(action)
	if callID == "" || (action != "accept" && action != "reject" && action != "hangup") {
		return ErrRealtimeUnavailable
	}
	endpoint, err := officialRealtimeEndpoint(account, false, "/calls/"+url.PathEscape(callID)+"/"+action)
	if err != nil {
		return err
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil || strings.TrimSpace(token) == "" {
		return fmt.Errorf("get official realtime API key: %w", err)
	}
	body := []byte(`{}`)
	if action == "accept" {
		if model = strings.TrimSpace(model); model == "" {
			return ErrRealtimeUnavailable
		}
		body, _ = json.Marshal(map[string]string{"type": "realtime", "model": model})
	}
	reqCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	request, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.doOpenAIUpstream(request, resolveAccountProxyURL(account), account)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, realtimeUpstreamBodyLimit+1))
	if err != nil {
		return err
	}
	if len(responseBody) > realtimeUpstreamBodyLimit {
		return errors.New("official realtime call control response is too large")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &UpstreamFailoverError{StatusCode: response.StatusCode, ResponseBody: responseBody, ResponseHeaders: response.Header.Clone()}
	}
	return nil
}

func (s *OpenAIGatewayService) GetOfficialRealtimeCall(ctx context.Context, callID string) (*RealtimeCallRecord, error) {
	store, err := s.realtimeStore()
	if err != nil {
		return nil, err
	}
	return store.GetRealtimeCall(ctx, hashRealtimeCallID(strings.TrimSpace(callID)))
}

func (s *OpenAIGatewayService) GetOfficialRealtimeCallForIdentity(ctx context.Context, callID string, identity RealtimeCallIdentity) (*RealtimeCallRecord, error) {
	store, err := s.realtimeStore()
	if err != nil {
		return nil, err
	}
	record, err := store.GetRealtimeCall(ctx, hashRealtimeCallID(callID))
	if err != nil {
		return nil, err
	}
	if record.CallID != callID || record.UserID != identity.UserID || record.GroupID != realtimeGroupID(identity.GroupID) ||
		(record.APIKeyID != identity.APIKeyID && !sameRealtimeEnterpriseEmployee(record.EnterpriseAttribution, identity.EnterpriseAttribution)) {
		return nil, ErrRealtimeIdentityMismatch
	}
	if record.Controller == RealtimeControllerClosed {
		return nil, ErrRealtimeCallNotFound
	}
	return record, nil
}

// GetOfficialRealtimeCallAccount 返回调用账本中冻结的账号。按 ID 回查而不接受
// 调用方提供的账号，避免 sideband 请求改变该调用的计费账号。
func (s *OpenAIGatewayService) GetOfficialRealtimeCallAccount(ctx context.Context, record *RealtimeCallRecord) (*Account, error) {
	if record == nil || record.AccountID <= 0 || s == nil || s.accountRepo == nil {
		return nil, ErrRealtimeCallNotFound
	}
	account, err := s.accountRepo.GetByID(ctx, record.AccountID)
	if err != nil || account == nil || !account.IsOpenAIApiKey() {
		return nil, ErrRealtimeUnavailable
	}
	return account, nil
}

func (s *OpenAIGatewayService) GetOfficialRealtimeAccountByID(ctx context.Context, accountID int64) (*Account, error) {
	if accountID <= 0 || s == nil || s.accountRepo == nil {
		return nil, ErrRealtimeUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil || !account.IsOpenAIApiKey() || !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityRealtime) {
		return nil, ErrRealtimeUnavailable
	}
	return account, nil
}

func sameRealtimeEnterpriseEmployee(record, current *EnterpriseUsageAttributionSnapshot) bool {
	if record == nil || current == nil || record.EnterpriseID <= 0 || current.EnterpriseID <= 0 || record.EnterpriseID != current.EnterpriseID || record.EmployeeID == nil || current.EmployeeID == nil {
		return false
	}
	return *record.EmployeeID > 0 && *record.EmployeeID == *current.EmployeeID
}

func (s *OpenAIGatewayService) dialOfficialRealtimeSideband(ctx context.Context, record *RealtimeCallRecord) (openaiwsv2.FrameConn, error) {
	if record == nil || s.accountRepo == nil {
		return nil, ErrRealtimeCallNotFound
	}
	account, err := s.accountRepo.GetByID(ctx, record.AccountID)
	if err != nil || account == nil {
		return nil, ErrRealtimeUnavailable
	}
	endpoint, err := officialRealtimeEndpoint(account, true, "")
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	query.Set("call_id", record.CallID)
	parsed.RawQuery = query.Encode()
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("get official realtime API key: %w", err)
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	conn, status, _, err := s.getOpenAIWSPassthroughDialer().Dial(ctx, parsed.String(), headers, resolveAccountProxyURL(account))
	if err != nil {
		return nil, fmt.Errorf("dial official realtime sideband (status %d): %w", status, err)
	}
	frames, ok := conn.(openaiwsv2.FrameConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("official realtime sideband transport does not support raw frames")
	}
	return frames, nil
}

// ProxyOfficialRealtimeSideband owns only the control channel. Media remains
// between the client and OpenAI through WebRTC or SIP.
func (s *OpenAIGatewayService) ProxyOfficialRealtimeSideband(ctx context.Context, record *RealtimeCallRecord, downstream *coderws.Conn, onTurn func(openaiwsv2.RelayTurnResult) error) error {
	if record == nil || downstream == nil {
		return ErrRealtimeCallNotFound
	}
	store, err := s.realtimeStore()
	if err != nil {
		return err
	}
	owner := uuid.NewString()
	claimed, err := store.ClaimRealtimeController(ctx, record.CallHash, RealtimeControllerProxy, owner)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrRealtimeControllerChanged
	}
	controllerReleased := false
	releaseController := func() {
		if controllerReleased {
			return
		}
		controllerReleased = true
		_, _ = store.ReleaseRealtimeController(context.Background(), record.CallHash, owner)
	}
	defer releaseController()
	upstream, err := s.dialOfficialRealtimeSideband(ctx, record)
	if err != nil {
		return err
	}
	defer upstream.Close()
	downstream.SetReadLimit(openAIWSMessageReadLimitBytes)
	clientFrames := realtimeModelBoundFrameConn{upstream: realtimeFrameConn{conn: downstream}, model: record.Model}
	var providerSessionEnded atomic.Bool
	_, exit := openaiwsv2.Relay(ctx, clientFrames, upstream, nil, openaiwsv2.RelayOptions{
		StartClientAfterFirstDownstream: true,
		BeforeWriteClient: func(messageType coderws.MessageType, payload []byte, _ bool) error {
			if messageType == coderws.MessageText && realtimeObserverSessionEnded(strings.TrimSpace(gjson.GetBytes(payload, "type").String())) {
				providerSessionEnded.Store(true)
			}
			return nil
		},
		OnTurnComplete: func(turn openaiwsv2.RelayTurnResult) {
			if strings.TrimSpace(turn.RequestID) == "" {
				return
			}
			claim, claimed, claimErr := s.claimRealtimeUsage(store, record.CallHash, turn.RequestID)
			if claimErr != nil || !claimed {
				return
			}
			if onTurn == nil || onTurn(turn) == nil {
				_ = claim.commit(context.Background())
				return
			}
			claim.release(context.Background())
		},
	})
	releaseController()
	if providerSessionEnded.Load() {
		s.finalizeOfficialRealtimeCall(record)
	} else if !time.Now().Before(record.ExpiresAt) {
		s.terminateOfficialRealtimeCall(record)
	} else {
		// 客户端控制连接断开不等于 WebRTC 媒体已经结束；改由服务端 observer
		// 接管，持续接收终态 usage 并在上游明确结束时释放会话租约。
		go s.ObserveOfficialRealtimeCall(record, onTurn)
	}
	if exit != nil && exit.Err != nil {
		return exit.Err
	}
	return nil
}

// ObserveOfficialRealtimeCall owns the server-side sideband for an accepted
// SIP call. The observer has no media path; it only settles terminal usage and
// detects provider-reported session termination.
func (s *OpenAIGatewayService) ObserveOfficialRealtimeCall(record *RealtimeCallRecord, onTurn func(openaiwsv2.RelayTurnResult) error) {
	if record == nil {
		return
	}
	store, err := s.realtimeStore()
	if err != nil {
		return
	}
	owner := uuid.NewString()
	claimed, err := store.ClaimRealtimeController(context.Background(), record.CallHash, RealtimeControllerObserver, owner)
	if err != nil || !claimed {
		return
	}
	for {
		latest, err := store.GetRealtimeCall(context.Background(), record.CallHash)
		if err != nil || latest.Controller != RealtimeControllerObserver {
			return
		}
		record = latest
		if !time.Now().Before(record.ExpiresAt) {
			s.finalizeOfficialRealtimeCall(record)
			return
		}
		upstream, err := s.dialOfficialRealtimeSideband(context.Background(), record)
		if err == nil {
			err = s.runOfficialRealtimeObserverConnection(record, upstream, onTurn)
			_ = upstream.Close()
		}
		if errors.Is(err, ErrRealtimeControllerChanged) {
			return
		}
		if errors.Is(err, errRealtimeObserverSessionEnded) {
			s.finalizeOfficialRealtimeCall(record)
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			s.terminateOfficialRealtimeCall(record)
			return
		}
		if !s.waitForRealtimeObserverRetry(record) {
			return
		}
	}
}

func (s *OpenAIGatewayService) runOfficialRealtimeObserverConnection(record *RealtimeCallRecord, upstream openaiwsv2.FrameConn, onTurn func(openaiwsv2.RelayTurnResult) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frameCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		for {
			messageType, payload, err := upstream.ReadFrame(ctx)
			if err != nil {
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
				return
			}
			if messageType != coderws.MessageText {
				continue
			}
			select {
			case frameCh <- payload:
			case <-ctx.Done():
				return
			}
		}
	}()
	refreshTicker := time.NewTicker(realtimeLeaseRefreshInterval)
	defer refreshTicker.Stop()
	controllerTicker := time.NewTicker(realtimeObserverPollInterval)
	defer controllerTicker.Stop()
	maxTimer := time.NewTimer(time.Until(record.ExpiresAt))
	defer maxTimer.Stop()
	store, err := s.realtimeStore()
	if err != nil {
		return err
	}
	for {
		select {
		case payload := <-frameCh:
			eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
			if realtimeObserverSessionEnded(eventType) {
				return errRealtimeObserverSessionEnded
			}
			turn, terminal := openaiwsv2.ParseRealtimeTerminalEvent(payload, record.CreatedAt, time.Now())
			if !terminal || strings.TrimSpace(turn.RequestID) == "" {
				continue
			}
			claim, claimed, claimErr := s.claimRealtimeUsage(store, record.CallHash, turn.RequestID)
			if claimErr != nil {
				return claimErr
			}
			if claimed {
				if onTurn == nil || onTurn(turn) == nil {
					_ = claim.commit(context.Background())
				} else {
					claim.release(context.Background())
				}
			}
		case err := <-errCh:
			return err
		case <-controllerTicker.C:
			controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
			if err != nil {
				return err
			}
			if controller != RealtimeControllerObserver {
				return ErrRealtimeControllerChanged
			}
		case <-refreshTicker.C:
			if !s.refreshRealtimeLease(record) {
				return ErrRealtimeUnavailable
			}
		case <-maxTimer.C:
			return context.DeadlineExceeded
		}
	}
}

func realtimeObserverSessionEnded(eventType string) bool {
	switch eventType {
	case "session.closed", "session.ended", "realtime.call.ended", "call.ended":
		return true
	default:
		return false
	}
}

func (s *OpenAIGatewayService) waitForRealtimeObserverRetry(record *RealtimeCallRecord) bool {
	timer := time.NewTimer(realtimeObserverRetryInterval)
	defer timer.Stop()
	<-timer.C
	store, err := s.realtimeStore()
	if err != nil {
		return false
	}
	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	return err == nil && controller == RealtimeControllerObserver && time.Now().Before(record.ExpiresAt)
}

func (s *OpenAIGatewayService) maintainRealtimeCall(record *RealtimeCallRecord) {
	if record == nil {
		return
	}
	ticker := time.NewTicker(realtimeLeaseRefreshInterval)
	defer ticker.Stop()
	timer := time.NewTimer(time.Until(record.ExpiresAt))
	defer timer.Stop()
	for {
		select {
		case <-ticker.C:
			if !s.refreshRealtimeLease(record) {
				s.terminateOfficialRealtimeCall(record)
				return
			}
		case <-timer.C:
			s.terminateOfficialRealtimeCall(record)
			return
		}
	}
}

// terminateOfficialRealtimeCall 仅用于本地生命周期到期或丢失租约的兜底路径。
// 上游请求失败时仍必须结束本地账本，避免失去并发约束的会话继续占用预留。
func (s *OpenAIGatewayService) terminateOfficialRealtimeCall(record *RealtimeCallRecord) {
	if record == nil {
		return
	}
	if account, err := s.GetOfficialRealtimeCallAccount(context.Background(), record); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), realtimeOperationTimeout)
		_ = s.ControlOfficialRealtimeCall(ctx, account, record.CallID, "hangup", "")
		cancel()
	}
	s.finalizeOfficialRealtimeCall(record)
}

// FinalizeOfficialRealtimeCall provides the single first-writer terminal path
// for timeout, explicit hangup and lease loss. It cannot recover an in-memory
// balance reservation after a process restart; the reservation's Redis TTL is
// the deliberate crash fallback.
func (s *OpenAIGatewayService) FinalizeOfficialRealtimeCall(record *RealtimeCallRecord) {
	s.finalizeOfficialRealtimeCall(record)
}

func (s *OpenAIGatewayService) finalizeOfficialRealtimeCall(record *RealtimeCallRecord) {
	if record == nil {
		return
	}
	store, err := s.realtimeStore()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), realtimeOperationTimeout)
	first, err := store.MarkRealtimeCallClosed(ctx, record.CallHash, realtimeClosedRecordTTL)
	cancel()
	if err != nil || !first {
		return
	}
	if record.ReservationUSD > 0 {
		if claims, ok := store.(RealtimeUsageClaimStore); ok {
			used, usageErr := claims.HasCommittedRealtimeUsage(context.Background(), record.CallHash)
			if usageErr == nil && !used {
				if callbacks := s.realtimeCallbacksSnapshot(); callbacks != nil && callbacks.settleConservative != nil {
					_ = callbacks.settleConservative(record)
				}
			}
		}
	}
	s.releaseRealtimeLease(record.AccountID, record.UserID, record.APIKeyID, record.LeaseID)
	if finalizer, ok := s.realtimeFinalizers.LoadAndDelete(record.CallHash); ok {
		finalizer.(func())()
	}
}

func (s *OpenAIGatewayService) refreshRealtimeLease(record *RealtimeCallRecord) bool {
	cache, err := s.realtimeConcurrencyCache()
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), realtimeOperationTimeout)
	defer cancel()
	ok, err := cache.RefreshRealtimeLease(ctx, record.AccountID, record.UserID, record.APIKeyID, record.LeaseID)
	return err == nil && ok
}

func (s *OpenAIGatewayService) releaseRealtimeLease(accountID, userID, apiKeyID int64, leaseID string) {
	cache, err := s.realtimeConcurrencyCache()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), realtimeOperationTimeout)
	defer cancel()
	_ = cache.ReleaseRealtimeLease(ctx, accountID, userID, apiKeyID, leaseID)
}
