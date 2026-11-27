package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type enterpriseSubscriptionRefresherStub struct {
	calls [][2]int64
	err   error
}

func (s *enterpriseSubscriptionRefresherStub) RefreshPlatformEnterpriseSubscriptionsForDedicatedUser(_ context.Context, dedicatedUpstreamUserID, actorUserID int64) error {
	s.calls = append(s.calls, [2]int64{dedicatedUpstreamUserID, actorUserID})
	return s.err
}

func TestAssignSubscriptionRefreshesEnterpriseMapping(t *testing.T) {
	subscriptions := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: 7, SubscriptionType: SubscriptionTypeSubscription},
	}, newSubscriptionUserSubRepoStub(), nil, nil, nil)
	refresher := &enterpriseSubscriptionRefresherStub{}
	subscriptions.SetEnterpriseSubscriptionRefresher(refresher)

	subscription, err := subscriptions.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 42, GroupID: 7, ValidityDays: 30, AssignedBy: 9,
	})

	require.NoError(t, err)
	require.Equal(t, int64(42), subscription.UserID)
	require.Equal(t, [][2]int64{{42, 9}}, refresher.calls)
}

func TestBulkAssignSubscriptionReportsEnterpriseMappingRefreshFailure(t *testing.T) {
	subscriptions := NewSubscriptionService(&subscriptionGroupRepoStub{
		group: &Group{ID: 7, SubscriptionType: SubscriptionTypeSubscription},
	}, newSubscriptionUserSubRepoStub(), nil, nil, nil)
	refresher := &enterpriseSubscriptionRefresherStub{err: errors.New("database unavailable")}
	subscriptions.SetEnterpriseSubscriptionRefresher(refresher)

	result, err := subscriptions.BulkAssignSubscription(context.Background(), &BulkAssignSubscriptionInput{
		UserIDs: []int64{42}, GroupID: 7, ValidityDays: 30, AssignedBy: 9,
	})

	require.NoError(t, err)
	require.Equal(t, 0, result.SuccessCount)
	require.Equal(t, 1, result.FailedCount)
	require.Equal(t, "failed", result.Statuses[42])
	require.Equal(t, [][2]int64{{42, 9}}, refresher.calls)
	require.Contains(t, result.Errors[0], "subscription assigned but enterprise mapping refresh failed")
}
