package enterpriseidentity

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type enterpriseSubscriptionAssignmentCacheInvalidator struct {
	userIDs []int64
}

func (*enterpriseSubscriptionAssignmentCacheInvalidator) InvalidateAuthCacheByKey(context.Context, string) {
}

func (i *enterpriseSubscriptionAssignmentCacheInvalidator) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	i.userIDs = append(i.userIDs, userID)
}

func TestRefreshPlatformEnterpriseSubscriptionsForDedicatedUserLinksReplacementSubscription(t *testing.T) {
	svc, mock := newMockService(t)
	invalidator := &enterpriseSubscriptionAssignmentCacheInvalidator{}
	svc.authCacheInvalidator = invalidator
	anchor := time.Date(2026, 11, 27, 10, 49, 13, 0, time.UTC)
	mock.ExpectQuery(`SELECT id\s+FROM enterprises\s+WHERE dedicated_upstream_user_id = \$1 AND status = 'active'`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT dedicated_upstream_user_id\s+FROM enterprises\s+WHERE id = \$1\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"dedicated_upstream_user_id"}).AddRow(int64(99)))
	mock.ExpectQuery(`SELECT id, upstream_user_subscription_id\s+FROM enterprise_subscriptions\s+WHERE enterprise_id = \$1 AND status = 'active'\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "upstream_user_subscription_id"}).AddRow(int64(88), int64(41)))
	mock.ExpectQuery(`(?s)SELECT subscription\.id, subscription\.weekly_window_start.*FROM user_subscriptions AS subscription.*subscription\.user_id = \$1.*FOR SHARE OF subscription`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}).AddRow(int64(42), anchor))
	mock.ExpectExec(`(?s)UPDATE enterprise_subscriptions.*SET status = 'ended'.*WHERE id = \$1 AND status = 'active'`).
		WithArgs(int64(88), "platform_user:12").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`(?s)INSERT INTO enterprise_subscriptions.*RETURNING id`).
		WithArgs(int64(7), int64(42), anchor, "platform_user:12").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(89)))
	mock.ExpectExec(`(?s)INSERT INTO enterprise_audit_events.*subscription\.refreshed`).
		WithArgs(int64(7), int64(89), sqlmock.AnyArg(), "platform_user:12").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := svc.RefreshPlatformEnterpriseSubscriptionsForDedicatedUser(context.Background(), 99, 12)

	require.NoError(t, err)
	require.Equal(t, []int64{99}, invalidator.userIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRefreshPlatformEnterpriseSubscriptionsForDedicatedUserIgnoresOrdinaryUsers(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery(`SELECT id\s+FROM enterprises\s+WHERE dedicated_upstream_user_id = \$1 AND status = 'active'`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	err := svc.RefreshPlatformEnterpriseSubscriptionsForDedicatedUser(context.Background(), 99, 12)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
