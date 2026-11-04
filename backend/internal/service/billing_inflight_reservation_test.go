//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestEstimateInflightReservationCost(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	pricing, err := billing.GetModelPricing("claude-sonnet-4-5")
	require.NoError(t, err)

	cfg := config.InflightReservationConfig{DefaultMaxTokens: 8192, MaxOutputTokens: 64000, MaxInputTokens: 1000}

	// explicit max_tokens; body 400 bytes → 100 input tokens
	cost, ok := estimateInflightReservationCostForTest(billing, cfg, "claude-sonnet-4-5", 400, 1000, 1)
	require.True(t, ok)
	require.InDelta(t, 100*pricing.InputPricePerToken+1000*pricing.OutputPricePerToken, cost, 1e-12)

	// missing max_tokens → default; rate multiplier applied
	cost, ok = estimateInflightReservationCostForTest(billing, cfg, "claude-sonnet-4-5", 0, 0, 2)
	require.True(t, ok)
	require.InDelta(t, 2*8192*pricing.OutputPricePerToken, cost, 1e-12)

	// caps: input and output clamped
	cost, ok = estimateInflightReservationCostForTest(billing, cfg, "claude-sonnet-4-5", 1_000_000, 1_000_000, 1)
	require.True(t, ok)
	require.InDelta(t, 1000*pricing.InputPricePerToken+64000*pricing.OutputPricePerToken, cost, 1e-12)

	// free group / missing billing → fail open
	_, ok = estimateInflightReservationCostForTest(billing, cfg, "claude-sonnet-4-5", 10, 10, 0)
	require.False(t, ok)
	_, ok = estimateInflightReservationCostForTest(nil, cfg, "claude-sonnet-4-5", 10, 10, 1)
	require.False(t, ok)
	_, ok = estimateInflightReservationCostForTest(billing, cfg, "", 10, 10, 1)
	require.False(t, ok)
}

func estimateInflightReservationCostForTest(billing *BillingService, cfg config.InflightReservationConfig, model string, bodyBytes, maxTokens int, rateMultiplier float64) (float64, bool) {
	if billing == nil || model == "" || rateMultiplier <= 0 {
		return 0, false
	}
	pricing, err := billing.GetModelPricing(model)
	if err != nil || pricing == nil {
		return 0, false
	}
	inputTokens, outputTokens := tokenCounts(cfg, bodyBytes, maxTokens)
	cost := (float64(inputTokens)*pricing.InputPricePerToken + float64(outputTokens)*pricing.OutputPricePerToken) * rateMultiplier
	return cost, validCost(cost)
}

func TestReserveInflightBalance_NoReservationCacheFailsOpen(t *testing.T) {
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	release, err := svc.ReserveInflightBalance(context.Background(), &User{ID: 1}, nil, nil, 100)
	require.NoError(t, err)
	release()
}

func TestReserveInflightBalance_SimpleModeDisabled(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Billing.InflightReservation.Enabled = true
	svc := NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	require.False(t, svc.InflightReservationEnabled())
}

// ---------------------------------------------------------------------------
// estimate: same model / resolver as billing
// ---------------------------------------------------------------------------

func newInflightEstimateGateway(t *testing.T, channelService *ChannelService) *GatewayService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1000, MaxInputTokens: 200000, MaxOutputTokens: 128000}
	billing := NewBillingService(cfg, nil)
	return &GatewayService{
		cfg:            cfg,
		billingService: billing,
		resolver:       NewModelPricingResolver(channelService, billing),
		channelService: channelService,
	}
}

func TestInflightEstimate_ChannelAliasUsesMappedModel(t *testing.T) {
	groupID := int64(10)
	ch := Channel{
		ID:       1,
		Status:   StatusActive,
		GroupIDs: []int64{groupID},
		ModelMapping: map[string]map[string]string{
			"anthropic": {"my-alias": "claude-sonnet-4-5"},
		},
	}
	cs := newTestChannelService(makeStandardRepo(ch, map[int64]string{groupID: "anthropic"}))
	svc := newInflightEstimateGateway(t, cs)
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 1}}

	// Old estimate (client model, base pricing only) → 0 → reservation bypassed.
	_, ok := estimateInflightReservationCostForTest(svc.billingService, svc.cfg.Billing.InflightReservation, "my-alias", 4000, 1000, 1)
	require.False(t, ok, "precondition: alias has no base pricing")

	est, priced := svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{Model: "my-alias", BodyBytes: 4000, MaxTokens: 1000})
	require.True(t, priced)
	require.Greater(t, est, 0.0)

	direct, _ := svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{Model: "claude-sonnet-4-5", BodyBytes: 4000, MaxTokens: 1000})
	require.InDelta(t, direct, est, 1e-12, "alias must be estimated as the channel-mapped billing model")
}

func TestInflightEstimate_GroupPerRequestPricing(t *testing.T) {
	groupID := int64(20)
	price := 0.5
	group := &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 2, ModelPricing: []ChannelModelPricing{
		{Models: []string{"custom-per-request"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price},
	}}
	svc := newInflightEstimateGateway(t, nil)
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: group}

	est, priced := svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{Model: "custom-per-request", BodyBytes: 100})
	require.True(t, priced)
	require.InDelta(t, 1.0, est, 1e-12, "per_request 0.5 × group rate 2")

	// Wildcard group token pricing is honored as well.
	in, out := 1e-6, 2e-6
	group.ModelPricing = []ChannelModelPricing{{Models: []string{"wild-*"}, BillingMode: BillingModeToken, InputPrice: &in, OutputPrice: &out}}
	est, priced = svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{Model: "wild-x", BodyBytes: 4000, MaxTokens: 500})
	require.True(t, priced)
	require.InDelta(t, (1000*in+500*out)*2, est, 1e-12)
}

func TestInflightEstimate_UnpricedIsReportedAndNonMeteredIsNot(t *testing.T) {
	svc := newInflightEstimateGateway(t, nil)
	apiKey := &APIKey{User: &User{ID: 1}}
	est, priced := svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{Model: "totally-unknown-model-xyz"})
	require.False(t, priced)
	require.Zero(t, est)

	est, priced = svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{})
	require.True(t, priced, "non-metered requests (e.g. media status lookups) are not 'unpriced'")
	require.Zero(t, est)
}

func TestInflightEstimate_MediaKinds(t *testing.T) {
	groupID := int64(30)
	p4k := 0.3
	search := 1000.0
	group := &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1, ImagePrice4K: &p4k, SearchPricePer1k: &search}
	svc := newInflightEstimateGateway(t, nil)
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: group}

	est, priced := svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{Model: "gpt-image-1", Kind: InflightEstimateImage, Units: 2})
	require.True(t, priced)
	require.GreaterOrEqual(t, est, 0.6, "image estimate uses the highest size tier × n")

	est, priced = svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{Model: "grok-web-search", Kind: InflightEstimatePerRequest, SearchCalls: 1})
	require.True(t, priced)
	require.InDelta(t, 1.0, est, 1e-12)

	est, priced = svc.EstimateInflightReservation(context.Background(), apiKey, InflightEstimateRequest{Model: "realtime", Kind: InflightEstimateAudio, AudioMode: "realtime", AudioUnits: 1})
	require.True(t, priced)
	require.Greater(t, est, 0.0)
}

// ---------------------------------------------------------------------------
// reservation handle: hand-off to billing task, renewal
// ---------------------------------------------------------------------------

type memInflightCache struct {
	BillingCache
	mu                   sync.Mutex
	balance              float64
	res                  map[string]float64
	exp                  map[string]time.Time
	renews               atomic.Int32
	deducts              atomic.Int32
	releaseCt            atomic.Int32
	scopes               map[string]time.Time
	scopeAmounts         map[string]float64
	scopeRenews          atomic.Int32
	scopeContextCanceled atomic.Bool
}

type failingScopeRestoreInflightCache struct {
	*memInflightCache
	failingScope string
}

func (m *failingScopeRestoreInflightCache) RestoreInflightScope(ctx context.Context, scope, requestID string, amount float64, ttl time.Duration) error {
	if scope == m.failingScope {
		return errors.New("temporary scope restore failure")
	}
	return m.memInflightCache.RestoreInflightScope(ctx, scope, requestID, amount, ttl)
}

func newMemInflightCache(balance float64) *memInflightCache {
	return &memInflightCache{balance: balance, res: map[string]float64{}, exp: map[string]time.Time{}, scopes: map[string]time.Time{}, scopeAmounts: map[string]float64{}}
}

func (m *memInflightCache) GetUserBalance(context.Context, int64) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.balance, nil
}

func (*memInflightCache) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	return &SubscriptionCacheData{Status: SubscriptionStatusActive}, nil
}

func (m *memInflightCache) DeductUserBalance(_ context.Context, _ int64, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balance -= amount
	m.deducts.Add(1)
	return nil
}

func (m *memInflightCache) gc() {
	now := time.Now()
	for id, e := range m.exp {
		if !e.After(now) {
			delete(m.exp, id)
			delete(m.res, id)
		}
	}
}

func (m *memInflightCache) ReserveInflightBalance(_ context.Context, _ int64, id string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gc()
	sum := 0.0
	for _, v := range m.res {
		sum += v
	}
	if len(m.res) > 0 && balance-sum < amount {
		return false, sum, nil
	}
	m.res[id] = amount
	m.exp[id] = time.Now().Add(ttl)
	return true, sum, nil
}

func (m *memInflightCache) ReleaseInflightBalance(_ context.Context, _ int64, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.res, id)
	delete(m.exp, id)
	m.releaseCt.Add(1)
	return nil
}

func (m *memInflightCache) RenewInflightBalance(_ context.Context, _ int64, id string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renews.Add(1)
	if _, ok := m.res[id]; !ok {
		return false, nil
	}
	m.exp[id] = time.Now().Add(ttl)
	return true, nil
}

func (m *memInflightCache) RestoreInflightBalance(_ context.Context, _ int64, id string, amount float64, ttl time.Duration) error {
	m.mu.Lock()
	m.res[id] = amount
	m.exp[id] = time.Now().Add(ttl)
	m.mu.Unlock()
	return nil
}

func (m *memInflightCache) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gc()
	return len(m.res)
}

func inflightScopeKey(scope, requestID string) string {
	return scope + "\x00" + requestID
}

func (m *memInflightCache) ReserveInflightScope(_ context.Context, scope, requestID string, amount float64, ttl time.Duration) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending := 0.0
	for key, value := range m.scopeAmounts {
		if key != inflightScopeKey(scope, requestID) && m.scopes[key].After(time.Now()) {
			pending += value
		}
	}
	key := inflightScopeKey(scope, requestID)
	m.scopes[key] = time.Now().Add(ttl)
	m.scopeAmounts[key] = amount
	return pending, nil
}

func (m *memInflightCache) RenewInflightScope(ctx context.Context, scope, requestID string, ttl time.Duration) (bool, error) {
	if ctx.Err() != nil {
		m.scopeContextCanceled.Store(true)
		return false, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scopeRenews.Add(1)
	key := inflightScopeKey(scope, requestID)
	if _, ok := m.scopes[key]; !ok {
		return false, nil
	}
	m.scopes[key] = time.Now().Add(ttl)
	return true, nil
}

func (m *memInflightCache) ReleaseInflightScope(_ context.Context, scope, requestID string) error {
	m.mu.Lock()
	key := inflightScopeKey(scope, requestID)
	delete(m.scopes, key)
	delete(m.scopeAmounts, key)
	m.mu.Unlock()
	return nil
}

func (m *memInflightCache) RestoreInflightScope(_ context.Context, scope, requestID string, amount float64, ttl time.Duration) error {
	m.mu.Lock()
	key := inflightScopeKey(scope, requestID)
	m.scopes[key] = time.Now().Add(ttl)
	m.scopeAmounts[key] = amount
	m.mu.Unlock()
	return nil
}

func (m *memInflightCache) scopeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for key, expiry := range m.scopes {
		if !expiry.After(now) {
			delete(m.scopes, key)
		}
	}
	return len(m.scopes)
}

func newInflightSvc(t *testing.T, cache BillingCache, ttlSeconds int) *BillingCacheService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: ttlSeconds}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	return svc
}

func TestInflightReservation_HeldUntilBillingTaskDone(t *testing.T) {
	cache := newMemInflightCache(1)
	svc := newInflightSvc(t, cache, 60)
	user := &User{ID: 1}

	res, err := svc.ReserveInflight(context.Background(), user, nil, nil, 0.9)
	require.NoError(t, err)
	require.NotNil(t, res)
	taskDone := res.Acquire() // billing task submitted
	res.HandlerDone()         // handler returned
	require.Equal(t, 1, cache.count(), "reservation must survive handler return while billing is pending")

	_, err = svc.ReserveInflight(context.Background(), user, nil, nil, 0.9)
	require.ErrorIs(t, err, ErrInsufficientBalance)

	taskDone()
	taskDone() // idempotent
	require.Equal(t, 0, cache.count())
	require.Equal(t, int32(1), cache.releaseCt.Load(), "released exactly once")

	// No billing task → HandlerDone releases immediately; Acquire after release is a no-op.
	res2, err := svc.ReserveInflight(context.Background(), user, nil, nil, 0.1)
	require.NoError(t, err)
	res2.HandlerDone()
	res2.HandlerDone()
	require.Equal(t, 0, cache.count())
	res2.Acquire()()
	require.Equal(t, int32(2), cache.releaseCt.Load())

	// Nil handle is safe.
	var nilRes *InflightReservation
	nilRes.Acquire()()
	nilRes.HandlerDone()
	nilRes.Release()
}

func TestInflightReservation_RenewedWhileHandlerActive(t *testing.T) {
	cache := newMemInflightCache(1)
	svc := newInflightSvc(t, cache, 1)
	user := &User{ID: 2}

	res, err := svc.ReserveInflight(context.Background(), user, nil, nil, 0.9)
	require.NoError(t, err)
	time.Sleep(2500 * time.Millisecond) // > 2 × TTL
	require.Equal(t, 1, cache.count(), "renewal keeps the reservation alive past its TTL")
	require.Greater(t, cache.renews.Load(), int32(3))
	_, err = svc.ReserveInflight(context.Background(), user, nil, nil, 0.9)
	require.ErrorIs(t, err, ErrInsufficientBalance)

	done := res.Acquire()
	res.HandlerDone()
	renewsBeforeSessionRelease := cache.renews.Load()
	time.Sleep(1200 * time.Millisecond)
	require.Greater(t, cache.renews.Load(), renewsBeforeSessionRelease, "a long-lived session reference must keep renewing after the handler returns")
	require.Equal(t, 1, cache.count())
	done()
	require.Equal(t, 0, cache.count())
}

func TestInflightReservation_SubscriptionAndAPIKeyScopesRenewPastTTL(t *testing.T) {
	cache := newMemInflightCache(1)
	svc := newInflightSvc(t, cache, 1)
	limit := 10.0
	group := &Group{ID: 4, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
	apiKey := &APIKey{ID: 8, User: &User{ID: 3}, Quota: 10}
	subscription := &UserSubscription{ID: 6}

	res, err := svc.ReserveInflightForAPIKey(context.Background(), apiKey.User, apiKey, group, subscription, 0.9)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, 2, cache.scopeCount(), "subscription and API key scopes must be reserved independently")

	time.Sleep(2500 * time.Millisecond)
	require.Equal(t, 2, cache.scopeCount(), "long-lived scope reservations must survive their TTL")
	require.Greater(t, cache.scopeRenews.Load(), int32(3))
	require.False(t, cache.scopeContextCanceled.Load(), "scope renewal must receive a live timeout context")

	res.HandlerDone()
	require.Equal(t, 0, cache.scopeCount())
}

func TestInflightReservation_APIKeyScopeRejectsConcurrentOverageAndReleasesFailedReservation(t *testing.T) {
	cache := newMemInflightCache(10)
	svc := newInflightSvc(t, cache, 60)
	apiKey := &APIKey{ID: 8, User: &User{ID: 3}, Quota: 1}

	first, err := svc.ReserveInflightForAPIKey(context.Background(), apiKey.User, apiKey, nil, nil, 0.6)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, 1, cache.scopeCount())

	second, err := svc.ReserveInflightForAPIKey(context.Background(), apiKey.User, apiKey, nil, nil, 0.6)
	require.ErrorIs(t, err, ErrAPIKeyQuotaExhausted)
	require.Nil(t, second)
	require.Equal(t, 1, cache.scopeCount(), "the rejected request must release its scope reservation")
	first.Release()
	require.Equal(t, 0, cache.scopeCount())
}

func TestInflightReservation_RestoreRealtimeReservationRenewsAllRecordedDimensions(t *testing.T) {
	cache := newMemInflightCache(10)
	svc := newInflightSvc(t, cache, 1)
	limit := 10.0
	group := &Group{ID: 4, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
	apiKey := &APIKey{ID: 8, User: &User{ID: 3}, Quota: 10}
	reservation, err := svc.ReserveInflightForAPIKey(context.Background(), apiKey.User, apiKey, group, &UserSubscription{ID: 6}, 0.9)
	require.NoError(t, err)
	requestID, hasBalance, scopes := reservation.Snapshot()
	require.False(t, hasBalance)
	require.Len(t, scopes, 2)
	reservation.Release()

	// Realtime 账本可同时记录余额与两个 scope 的预留。
	// 恢复必须以同一 request ID 重建并续期全部维度。
	release, err := svc.RestoreInflightReservation(apiKey.User.ID, requestID, 0.9, true, scopes)
	require.NoError(t, err)
	time.Sleep(2500 * time.Millisecond)
	require.Equal(t, 1, cache.count(), "recovered balance reservation must outlive its original TTL")
	require.Equal(t, 2, cache.scopeCount(), "recovered subscription and API key reservations must outlive their original TTL")
	require.Greater(t, cache.renews.Load(), int32(1))
	require.Greater(t, cache.scopeRenews.Load(), int32(3))
	release()
	require.Equal(t, 0, cache.count())
	require.Equal(t, 0, cache.scopeCount())
}

func TestInflightReservation_RestoreFailureReleasesPartialDimensions(t *testing.T) {
	firstScope := inflightSubscriptionScope(6)
	secondScope := inflightAPIKeyScope(8)
	cache := &failingScopeRestoreInflightCache{memInflightCache: newMemInflightCache(10), failingScope: secondScope}
	svc := newInflightSvc(t, cache, 60)

	release, err := svc.RestoreInflightReservation(3, "partial-restore", 0.9, true, []string{firstScope, secondScope})
	require.Error(t, err)
	require.Nil(t, release)
	require.Equal(t, 0, cache.count(), "恢复失败必须释放已恢复的余额预留")
	require.Equal(t, 0, cache.scopeCount(), "恢复失败必须释放此前成功恢复的 scope")
	require.GreaterOrEqual(t, cache.releaseCt.Load(), int32(1))
}

func TestRecoverOfficialRealtimeFinalizingCallRenewsAndReleasesReservation(t *testing.T) {
	cache := newMemInflightCache(10)
	billing := newInflightSvc(t, cache, 1)
	store := &realtimeLifecycleStore{}
	record := &RealtimeCallRecord{
		CallID: "call-recover-reservation", CallHash: hashRealtimeCallID("call-recover-reservation"), AccountID: 1, APIKeyID: 8, UserID: 3,
		LeaseID: "lease-recover-reservation", Model: "gpt-realtime", PricingAt: time.Now(), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
		Controller: RealtimeControllerFinalizing, ReservationUSD: 0.9, ReservationRequestID: "recover-reservation",
		ReservationHasBalance: true, ReservationScopes: []string{inflightSubscriptionScope(6), inflightAPIKeyScope(8)},
	}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store, billingCacheService: billing}
	svc.SetOfficialRealtimeLifecycleCallbacks(nil, func(*RealtimeCallRecord) error { return errors.New("temporary settlement failure") })
	svc.RecoverOfficialRealtimeCalls()

	time.Sleep(2500 * time.Millisecond)
	require.Equal(t, 1, cache.count(), "恢复后的 finalizing call 必须持续续期余额预留")
	require.Equal(t, 2, cache.scopeCount(), "恢复后的 finalizing call 必须持续续期全部 scope")

	claimed, err := store.ClaimRealtimeUsage(context.Background(), record.CallHash, "actual", "owner", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	committed, err := store.CommitRealtimeUsageClaim(context.Background(), record.CallHash, "actual", "owner", time.Hour)
	require.NoError(t, err)
	require.True(t, committed)
	svc.FinalizeOfficialRealtimeCall(record)
	require.Eventually(t, func() bool {
		controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
		return err == nil && controller == RealtimeControllerClosed
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, 0, cache.count())
	require.Equal(t, 0, cache.scopeCount())
}

func TestSyncBalanceCacheAfterDeduction_SynchronousWhenInflightEnabled(t *testing.T) {
	cache := newMemInflightCache(1)
	svc := newInflightSvc(t, cache, 60)
	p := &postUsageBillingParams{Cost: &CostBreakdown{ActualCost: 0.25}, User: &User{ID: 3}}
	syncBalanceCacheAfterDeduction(context.Background(), p, &billingDeps{billingCacheService: svc}, nil)
	require.Equal(t, int32(1), cache.deducts.Load(), "cache deduction must land before the billing task returns")
	bal, _ := cache.GetUserBalance(context.Background(), 3)
	require.InDelta(t, 0.75, bal, 1e-12)
}

// 计费侧：requested 来源下别名本身无价 → billableModelWithFallback 回退到映射模型。
// 准入估算必须同口径地 > 0，且等于按回退模型的估算。
func TestInflightEstimate_RequestedSourceUnpricedAliasFallsBackLikeBilling(t *testing.T) {
	groupID := int64(30)
	ch := Channel{
		ID:                 3,
		Status:             StatusActive,
		GroupIDs:           []int64{groupID},
		BillingModelSource: BillingModelSourceRequested,
		ModelMapping: map[string]map[string]string{
			"anthropic": {"req-alias": "claude-sonnet-4-5"},
		},
	}
	cs := newTestChannelService(makeStandardRepo(ch, map[int64]string{groupID: "anthropic"}))
	svc := newInflightEstimateGateway(t, cs)
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 1}}
	ctx := context.Background()

	billed := svc.billableModelWithFallback(ctx, apiKey, "req-alias", "claude-sonnet-4-5", "req-alias")
	require.Equal(t, "claude-sonnet-4-5", billed, "precondition: billing falls back to the mapped model")

	est, priced := svc.EstimateInflightReservation(ctx, apiKey, InflightEstimateRequest{Model: "req-alias", BodyBytes: 4000, MaxTokens: 1000})
	require.True(t, priced)
	require.Greater(t, est, 0.0)
	direct, _ := svc.EstimateInflightReservation(ctx, apiKey, InflightEstimateRequest{Model: billed, BodyBytes: 4000, MaxTokens: 1000})
	require.InDelta(t, direct, est, 1e-12)
}

// 计费侧：别名仅在账号级映射（无渠道映射）→ UpstreamModel 为账号映射模型，计费回退到它。
// 准入时账号未选定：按分组内候选账号映射模型的最高估算。
func TestInflightEstimate_AccountLevelMappingFallsBackLikeBilling(t *testing.T) {
	groupID := int64(31)
	svc := newInflightEstimateGateway(t, nil)
	snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(groupID, PlatformAnthropic): {
		{ID: 1, Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"acct-alias-31": "claude-sonnet-4-5"}}},
		{ID: 2, Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"acct-alias-31": "claude-opus-4-1"}}},
	}}}
	attachInflightSnapshot(svc, snap)
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 1}}
	ctx := context.Background()

	_, ok := estimateInflightReservationCostForTest(svc.billingService, svc.cfg.Billing.InflightReservation, "acct-alias-31", 4000, 1000, 1)
	require.False(t, ok, "precondition: alias has no pricing")

	est, priced := svc.EstimateInflightReservation(ctx, apiKey, InflightEstimateRequest{Model: "acct-alias-31", BodyBytes: 4000, MaxTokens: 1000})
	require.True(t, priced)
	req := InflightEstimateRequest{BodyBytes: 4000, MaxTokens: 1000}
	best := 0.0
	for _, upstream := range []string{"claude-sonnet-4-5", "claude-opus-4-1"} {
		billed := svc.billableModelWithFallback(ctx, apiKey, "acct-alias-31", upstream, "acct-alias-31")
		require.Equal(t, upstream, billed, "precondition: billing charges the account-mapped model")
		req.Model = billed
		c, _ := svc.EstimateInflightReservation(ctx, apiKey, req)
		require.Greater(t, c, 0.0)
		best = math.Max(best, c)
	}
	require.InDelta(t, best, est, 1e-12, "estimate = max over candidate account-mapped billing models")
}

type inflightSnapshotCacheStub struct {
	SchedulerCache
	mu       sync.Mutex
	byBucket map[string][]Account
	reads    atomic.Int64
}

func inflightBucketKey(groupID int64, platform string) string {
	return fmt.Sprintf("%d|%s", groupID, platform)
}

func (c *inflightSnapshotCacheStub) set(groupID int64, platform string, accounts []Account) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byBucket[inflightBucketKey(groupID, platform)] = accounts
}

func (c *inflightSnapshotCacheStub) GetSnapshot(ctx context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	c.reads.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	src := c.byBucket[inflightBucketKey(bucket.GroupID, bucket.Platform)]
	out := make([]*Account, 0, len(src))
	for i := range src {
		a := src[i]
		out = append(out, &a)
	}
	return out, true, nil
}

// inflightCountingAccountRepo 统计请求路径上的任何直接查库。
type inflightCountingAccountRepo struct {
	AccountRepository
	dbCalls atomic.Int64
}

func (r *inflightCountingAccountRepo) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	r.dbCalls.Add(1)
	return nil, nil
}

func attachInflightSnapshot(svc *GatewayService, snap *inflightSnapshotCacheStub) *inflightCountingAccountRepo {
	repo := &inflightCountingAccountRepo{}
	svc.accountRepo = repo
	svc.schedulerSnapshot = NewSchedulerSnapshotService(snap, nil, repo, nil, svc.cfg)
	return repo
}

// 已定价模型永不查账号映射；随机未定价模型名不直接查库、不产生按模型名的缓存（内存有界）。
func TestInflightEstimate_AccountMappingNoDBAndBoundedMemory(t *testing.T) {
	groupID := int64(40)
	svc := newInflightEstimateGateway(t, nil)
	snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(groupID, PlatformAnthropic): {
		{ID: 1, Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"acct-alias-40": "claude-sonnet-4-5"}}},
	}}}
	repo := attachInflightSnapshot(svc, snap)
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 1}}
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		_, priced := svc.EstimateInflightReservation(ctx, apiKey, InflightEstimateRequest{Model: "claude-sonnet-4-5", BodyBytes: 4000, MaxTokens: 1000})
		require.True(t, priced)
	}
	require.Zero(t, snap.reads.Load(), "priced models must never trigger account-mapping lookup")

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	const n = 20000
	for i := 0; i < n; i++ {
		_, priced := svc.EstimateInflightReservation(ctx, apiKey, InflightEstimateRequest{Model: fmt.Sprintf("rand-%d-%d", i, time.Now().UnixNano()), BodyBytes: 4000, MaxTokens: 1000})
		require.False(t, priced)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	require.Zero(t, repo.dbCalls.Load(), "no direct DB query on the request path")
	require.Equal(t, int64(n), snap.reads.Load(), "unpriced lookups read the scheduler snapshot only")
	require.Less(t, int64(after.HeapAlloc)-int64(before.HeapAlloc), int64(8<<20), "no per-model cache growth")
}

// 无分组 API Key：使用调度器的未分组账号池，估算与计费回退的账号映射模型同口径。
func TestInflightEstimate_NoGroupKeyUsesUngroupedAccountMapping(t *testing.T) {
	svc := newInflightEstimateGateway(t, nil)
	snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(0, PlatformAnthropic): {
		{ID: 1, Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"acct-alias-nog": "claude-opus-4-1"}}},
	}}}
	attachInflightSnapshot(svc, snap)
	apiKey := &APIKey{User: &User{ID: 1}}
	ctx := context.Background()

	est, priced := svc.EstimateInflightReservation(ctx, apiKey, InflightEstimateRequest{Model: "acct-alias-nog", BodyBytes: 4000, MaxTokens: 1000})
	require.True(t, priced)
	direct, ok := svc.EstimateInflightReservation(ctx, apiKey, InflightEstimateRequest{Model: "claude-opus-4-1", BodyBytes: 4000, MaxTokens: 1000})
	require.True(t, ok)
	require.InDelta(t, direct, est, 1e-12)
}

// 管理员新增账号映射后立即生效（不缓存负结果）。
func TestInflightEstimate_AccountMappingNoNegativeCaching(t *testing.T) {
	groupID := int64(41)
	svc := newInflightEstimateGateway(t, nil)
	snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{}}
	attachInflightSnapshot(svc, snap)
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 1}}
	ctx := context.Background()
	req := InflightEstimateRequest{Model: "acct-alias-41", BodyBytes: 4000, MaxTokens: 1000}

	_, priced := svc.EstimateInflightReservation(ctx, apiKey, req)
	require.False(t, priced)
	snap.set(groupID, PlatformAnthropic, []Account{{ID: 9, Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"acct-*": "claude-sonnet-4-5"}}}})
	est, priced := svc.EstimateInflightReservation(ctx, apiKey, req)
	require.True(t, priced, "new mapping (incl. wildcard) visible on next request")
	require.Greater(t, est, 0.0)
}
