package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheRealtimeCallControllerAndIdempotency(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	cache, ok := NewGatewayCache(client).(service.RealtimeCallStore)
	require.True(t, ok)

	record := &service.RealtimeCallRecord{
		CallID: "call_secret", CallHash: "call_hash", AccountID: 11, APIKeyID: 22,
		UserID: 33, GroupID: 44, LeaseID: "lease", Model: "gpt-realtime",
		PricingAt: time.Now(), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
		Controller: service.RealtimeControllerPending,
	}
	require.NoError(t, cache.SaveRealtimeCall(context.Background(), record, time.Hour))

	loaded, err := cache.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, record.CallID, loaded.CallID)
	require.Equal(t, record.PricingAt.UnixMilli(), loaded.PricingAt.UnixMilli())
	claimed, err := cache.ClaimRealtimeController(context.Background(), record.CallHash, service.RealtimeControllerObserver, "observer-1")
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = cache.ClaimRealtimeController(context.Background(), record.CallHash, service.RealtimeControllerProxy, "proxy-1")
	require.NoError(t, err)
	require.True(t, claimed)
	released, err := cache.ReleaseRealtimeController(context.Background(), record.CallHash, "proxy-1")
	require.NoError(t, err)
	require.True(t, released)

	claimed, err = cache.BeginRealtimeUsage(context.Background(), record.CallHash, "resp_123", time.Hour)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = cache.BeginRealtimeUsage(context.Background(), record.CallHash, "resp_123", time.Hour)
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, cache.ReleaseRealtimeUsage(context.Background(), record.CallHash, "resp_123"))
	claimed, err = cache.BeginRealtimeUsage(context.Background(), record.CallHash, "resp_123", time.Hour)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, cache.CommitRealtimeUsage(context.Background(), record.CallHash, "resp_123", time.Hour))
	claimed, err = cache.BeginRealtimeUsage(context.Background(), record.CallHash, "resp_123", time.Hour)
	require.NoError(t, err)
	require.False(t, claimed)
	claimed, err = cache.BeginRealtimeWebhook(context.Background(), "evt_123", time.Hour)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = cache.BeginRealtimeWebhook(context.Background(), "evt_123", time.Hour)
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, cache.ReleaseRealtimeWebhook(context.Background(), "evt_123"))
	claimed, err = cache.BeginRealtimeWebhook(context.Background(), "evt_123", time.Hour)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, cache.CommitRealtimeWebhook(context.Background(), "evt_123", time.Hour))

	closed, err := cache.MarkRealtimeCallClosed(context.Background(), record.CallHash, time.Hour)
	require.NoError(t, err)
	require.True(t, closed)
	closed, err = cache.MarkRealtimeCallClosed(context.Background(), record.CallHash, time.Hour)
	require.NoError(t, err)
	require.False(t, closed)
}

func TestGatewayCacheRealtimeReplayClaimsExpireAndTransferSafely(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	cache, ok := NewGatewayCache(client).(service.RealtimeUsageClaimStore)
	require.True(t, ok)
	ctx := context.Background()

	claimed, err := cache.ClaimRealtimeUsage(ctx, "call", "response", "owner-a", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = cache.ClaimRealtimeUsage(ctx, "call", "response", "owner-b", time.Second)
	require.NoError(t, err)
	require.False(t, claimed)

	redisServer.FastForward(time.Second)
	claimed, err = cache.ClaimRealtimeUsage(ctx, "call", "response", "owner-b", time.Second)
	require.NoError(t, err)
	require.True(t, claimed, "expired pending claim must be recoverable after a process exits")

	committed, err := cache.CommitRealtimeUsageClaim(ctx, "call", "response", "owner-a", time.Hour)
	require.NoError(t, err)
	require.False(t, committed, "previous owner cannot commit a reclaimed claim")
	released, err := cache.ReleaseRealtimeUsageClaim(ctx, "call", "response", "owner-a")
	require.NoError(t, err)
	require.False(t, released, "previous owner cannot release a reclaimed claim")
	committed, err = cache.CommitRealtimeUsageClaim(ctx, "call", "response", "owner-b", time.Hour)
	require.NoError(t, err)
	require.True(t, committed)

	used, err := cache.HasCommittedRealtimeUsage(ctx, "call")
	require.NoError(t, err)
	require.True(t, used)
	claimed, err = cache.ClaimRealtimeUsage(ctx, "call", "response", "owner-c", time.Second)
	require.NoError(t, err)
	require.False(t, claimed, "committed usage must remain replay-safe")

	claimed, err = cache.ClaimRealtimeWebhook(ctx, "event", "owner-a", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	redisServer.FastForward(time.Second)
	claimed, err = cache.ClaimRealtimeWebhook(ctx, "event", "owner-b", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	committed, err = cache.CommitRealtimeWebhookClaim(ctx, "event", "owner-a", time.Hour)
	require.NoError(t, err)
	require.False(t, committed)
	committed, err = cache.CommitRealtimeWebhookClaim(ctx, "event", "owner-b", time.Hour)
	require.NoError(t, err)
	require.True(t, committed)
}

func TestGatewayCacheRealtimeFinalizationSeparatesActualAndConservativeUsage(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	cache, ok := NewGatewayCache(client).(interface {
		service.RealtimeCallStore
		service.RealtimeUsageClaimStore
		service.RealtimeConservativeUsageClaimStore
		service.RealtimeFinalizationStore
	})
	require.True(t, ok)
	ctx := context.Background()
	record := &service.RealtimeCallRecord{
		CallID: "call-finalize", CallHash: "call-finalize-hash", AccountID: 11, APIKeyID: 22,
		UserID: 33, Model: "gpt-realtime", PricingAt: time.Now(), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
		Controller: service.RealtimeControllerPending,
	}
	require.NoError(t, cache.SaveRealtimeCall(ctx, record, time.Hour))

	claimed, err := cache.ClaimRealtimeFinalization(ctx, record.CallHash, "finalizer", time.Hour)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = cache.ClaimRealtimeUsage(ctx, record.CallHash, "actual", "actual-owner", time.Second)
	require.NoError(t, err)
	require.False(t, claimed, "finalizing 状态必须拒绝新的实际 usage claim")
	claimed, err = cache.ClaimRealtimeConservativeUsage(ctx, record.CallHash, "conservative", "other-finalizer", "conservative-owner", time.Second)
	require.NoError(t, err)
	require.False(t, claimed, "仅 finalizer owner 可创建保守 usage")
	claimed, err = cache.ClaimRealtimeConservativeUsage(ctx, record.CallHash, "conservative", "finalizer", "conservative-owner", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	committed, err := cache.CommitRealtimeUsageClaim(ctx, record.CallHash, "conservative", "conservative-owner", time.Hour)
	require.NoError(t, err)
	require.True(t, committed)
	closed, err := cache.CommitRealtimeFinalization(ctx, record.CallHash, "finalizer", time.Hour)
	require.NoError(t, err)
	require.True(t, closed)

	controller, err := cache.GetRealtimeController(ctx, record.CallHash)
	require.NoError(t, err)
	require.Equal(t, service.RealtimeControllerClosed, controller)
}

func TestGatewayCacheListActiveRealtimeCallsSkipsClosedRecords(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	cache, ok := NewGatewayCache(client).(interface {
		service.RealtimeCallStore
		service.RealtimeCallRecoveryStore
	})
	require.True(t, ok)
	ctx := context.Background()
	for _, record := range []*service.RealtimeCallRecord{
		{CallID: "active-call", CallHash: "active", AccountID: 1, APIKeyID: 2, UserID: 3, Model: "gpt-realtime", PricingAt: time.Now(), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Controller: service.RealtimeControllerPending},
		{CallID: "closed-call", CallHash: "closed", AccountID: 1, APIKeyID: 2, UserID: 3, Model: "gpt-realtime", PricingAt: time.Now(), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Controller: service.RealtimeControllerPending},
	} {
		require.NoError(t, cache.SaveRealtimeCall(ctx, record, time.Hour))
	}
	closed, err := cache.MarkRealtimeCallClosed(ctx, "closed", time.Hour)
	require.NoError(t, err)
	require.True(t, closed)

	records, err := cache.ListActiveRealtimeCalls(ctx)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "active", records[0].CallHash)
}
