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

func TestRealtimeLeaseReplacesRegularSlotsAndExpiresWithoutRefresh(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	regular := NewConcurrencyCache(client, 15, 900)
	realtime, ok := regular.(service.RealtimeConcurrencyCache)
	require.True(t, ok)
	ctx := context.Background()

	accountAcquired, err := regular.AcquireAccountSlot(ctx, 10, 1, "regular-account")
	require.NoError(t, err)
	require.True(t, accountAcquired)
	userAcquired, err := regular.AcquireUserSlot(ctx, 20, 1, "regular-user")
	require.NoError(t, err)
	require.True(t, userAcquired)
	acquired, err := realtime.AcquireRealtimeLease(ctx, 10, 1, 20, 1, 30, "realtime-lease", true)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, regular.ReleaseAccountSlot(ctx, 10, "regular-account"))
	require.NoError(t, regular.ReleaseUserSlot(ctx, 20, "regular-user"))
	accountCount, err := regular.GetAccountConcurrency(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, accountCount)
	require.NoError(t, realtime.ReleaseRealtimeLease(ctx, 10, 20, 30, "realtime-lease"))

	acquired, err = realtime.AcquireRealtimeLease(ctx, 10, 1, 20, 1, 30, "expired-realtime", false)
	require.NoError(t, err)
	require.True(t, acquired)
	redisServer.FastForward(61 * time.Second)
	accountAcquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-after-expiry")
	require.NoError(t, err)
	require.True(t, accountAcquired)
	refreshed, err := realtime.RefreshRealtimeLease(ctx, 10, 20, 30, "expired-realtime")
	require.NoError(t, err)
	require.False(t, refreshed)
}
