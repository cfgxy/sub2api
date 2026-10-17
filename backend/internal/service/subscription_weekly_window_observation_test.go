package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type weeklyWindowObservationRepo struct {
	userSubRepoNoop
	resetCount int
	expected   *time.Time
	next       time.Time
}

func (r *weeklyWindowObservationRepo) ResetWeeklyUsage(_ context.Context, _ int64, expected *time.Time, next time.Time) error {
	r.resetCount++
	r.expected, r.next = expected, next
	return nil
}

func TestWeeklyWindowMaintenanceAdvancesTwoElapsedPeriodsOnce(t *testing.T) {
	oldAnchor := time.Date(2026, 9, 27, 16, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 17, 15, 6, 31, 0, time.UTC)
	wantAnchor := time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)
	repo := &weeklyWindowObservationRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	t.Cleanup(svc.Stop)
	svc.now = func() time.Time { return now }
	sub := &UserSubscription{
		ID: 1, UserID: 10, GroupID: 20,
		StartsAt: oldAnchor, ExpiresAt: oldAnchor.Add(60 * 24 * time.Hour),
		WeeklyWindowStart: &oldAnchor, WeeklyUsageUSD: 0.002137,
	}

	// 维护推进两个完整周，而非把首次观察时间作为新的周起点。
	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))
	require.Equal(t, 1, repo.resetCount)
	require.Equal(t, oldAnchor, *repo.expected)
	require.Equal(t, wantAnchor, repo.next)
	require.Equal(t, wantAnchor, *sub.WeeklyWindowStart)
	require.Zero(t, sub.WeeklyUsageUSD)

	// 同一窗口再次维护不得重复重置期间新增的用量。
	sub.WeeklyUsageUSD = 0.004
	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))
	require.Equal(t, 1, repo.resetCount)
	require.Equal(t, 0.004, sub.WeeklyUsageUSD)
}
