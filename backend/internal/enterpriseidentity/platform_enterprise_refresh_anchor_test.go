package enterpriseidentity

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

func TestRefreshPlatformEnterpriseSubscriptionsInitializesMissingAnchor(t *testing.T) {
	svc, mock := newMockService(t)
	fixed := time.Date(2026, 10, 2, 12, 30, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT dedicated_upstream_user_id\s+FROM enterprises\s+WHERE id = \$1\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"dedicated_upstream_user_id"}).AddRow(int64(99)))
	mock.ExpectQuery(`SELECT id, upstream_user_subscription_id\s+FROM enterprise_subscriptions\s+WHERE enterprise_id = \$1 AND status = 'active'\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "upstream_user_subscription_id"}))
	mock.ExpectQuery(`(?s)SELECT subscription\.id, subscription\.weekly_window_start.*FROM user_subscriptions AS subscription.*subscription\.user_id = \$1.*FOR SHARE OF subscription`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}).AddRow(int64(42), nil))
	mock.ExpectQuery(`UPDATE user_subscriptions\s+SET daily_window_start = \$2, weekly_window_start = \$3, monthly_window_start = \$3`).
		WithArgs(int64(42), timezone.StartOfDay(fixed), fixed).
		WillReturnRows(sqlmock.NewRows([]string{"weekly_window_start"}).AddRow(fixed))
	mock.ExpectQuery(`(?s)INSERT INTO enterprise_subscriptions.*RETURNING id`).
		WithArgs(int64(7), int64(42), fixed, "platform_user:12").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(88)))
	mock.ExpectExec(`(?s)INSERT INTO enterprise_audit_events.*subscription\.refreshed`).
		WithArgs(int64(7), int64(88), sqlmock.AnyArg(), "platform_user:12").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := svc.RefreshPlatformEnterpriseSubscriptions(context.Background(), 7, 12)

	require.NoError(t, err)
	require.Equal(t, "linked", result.Action)
	require.EqualValues(t, 88, *result.EnterpriseSubscriptionID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRefreshPlatformEnterpriseSubscriptionsReadsAnchorAfterConditionalUpdateMiss(t *testing.T) {
	svc, mock := newMockService(t)
	fixed := time.Date(2026, 10, 2, 12, 30, 0, 0, time.UTC)
	existing := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT dedicated_upstream_user_id\s+FROM enterprises\s+WHERE id = \$1\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"dedicated_upstream_user_id"}).AddRow(int64(99)))
	mock.ExpectQuery(`SELECT id, upstream_user_subscription_id\s+FROM enterprise_subscriptions\s+WHERE enterprise_id = \$1 AND status = 'active'\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "upstream_user_subscription_id"}))
	mock.ExpectQuery(`(?s)SELECT subscription\.id, subscription\.weekly_window_start.*FROM user_subscriptions AS subscription.*subscription\.user_id = \$1.*FOR SHARE OF subscription`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}).AddRow(int64(42), nil))
	mock.ExpectQuery(`UPDATE user_subscriptions\s+SET daily_window_start = \$2, weekly_window_start = \$3, monthly_window_start = \$3`).
		WithArgs(int64(42), timezone.StartOfDay(fixed), fixed).
		WillReturnRows(sqlmock.NewRows([]string{"weekly_window_start"}))
	mock.ExpectQuery(`SELECT weekly_window_start FROM user_subscriptions WHERE id = \$1`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"weekly_window_start"}).AddRow(existing))
	mock.ExpectQuery(`(?s)INSERT INTO enterprise_subscriptions.*RETURNING id`).
		WithArgs(int64(7), int64(42), existing, "platform_user:12").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(88)))
	mock.ExpectExec(`(?s)INSERT INTO enterprise_audit_events.*subscription\.refreshed`).
		WithArgs(int64(7), int64(88), sqlmock.AnyArg(), "platform_user:12").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := svc.RefreshPlatformEnterpriseSubscriptions(context.Background(), 7, 12)

	require.NoError(t, err)
	require.Equal(t, "linked", result.Action)
	require.EqualValues(t, 88, *result.EnterpriseSubscriptionID)
	require.NoError(t, mock.ExpectationsWereMet())
}
