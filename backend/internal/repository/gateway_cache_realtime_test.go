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
