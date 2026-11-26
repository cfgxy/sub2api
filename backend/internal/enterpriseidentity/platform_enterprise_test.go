package enterpriseidentity

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestGetPlatformEnterpriseIncludesSubscriptionAndImpactSummary(t *testing.T) {
	svc, mock := newMockService(t)
	createdAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	subscriptions, err := json.Marshal([]map[string]any{
		{"id": 41, "status": "active", "plan": "周订阅", "weekly_limit": "550.00000000",
			"weekly_window_start": anchor, "starts_at": createdAt, "expires_at": anchor.AddDate(0, 0, 7)},
		{"id": 42, "status": "scheduled", "plan": "周订阅", "weekly_limit": "600.00000000",
			"weekly_window_start": nil, "starts_at": anchor.AddDate(0, 0, 7), "expires_at": anchor.AddDate(0, 0, 14)},
	})
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT id, name, LOWER.*FROM enterprises WHERE id = \$1`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "dedicated", "status", "created_at", "seat_count"}).
			AddRow(int64(7), "Acme", "acme.example.com", int64(99), "active", createdAt, 5))
	mock.ExpectQuery(`(?s)SELECT COALESCE.*FROM enterprises AS e WHERE e.id = \$1`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"admin_email", "concurrency", "subscriptions"}).AddRow("admin@example.com", 5, subscriptions))
	mock.ExpectQuery(`(?s)SELECT.*COUNT\(\*\).*enterprise_employees`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"employees", "active_employees", "sessions", "keys"}).AddRow(int64(12), int64(10), int64(3), int64(8)))

	item, err := svc.GetPlatformEnterprise(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, "a***@example.com", item.AdminEmail)
	require.Equal(t, int64(12), item.EmployeeCount)
	require.Equal(t, int64(10), item.ActiveEmployeeCount)
	require.Equal(t, int64(3), item.ActiveSessionCount)
	require.Equal(t, int64(8), item.ActiveKeyCount)
	require.Len(t, item.Subscriptions, 2)
	require.Equal(t, "550.00000000", item.Subscriptions[0].WeeklyLimit)
	require.Equal(t, "scheduled", item.Subscriptions[1].Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListPlatformEnterprisesIncludesSubscriptionsSameAsDetail(t *testing.T) {
	svc, mock := newMockService(t)
	createdAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	subscriptions, err := json.Marshal([]map[string]any{
		{"id": 41, "status": "active", "plan": "周订阅", "weekly_limit": "550.00000000",
			"weekly_window_start": anchor, "starts_at": createdAt, "expires_at": anchor.AddDate(0, 0, 7)},
	})
	require.NoError(t, err)
	columns := []string{"id", "name", "host", "dedicated", "status", "created_at", "seat_count", "admin_email", "concurrency", "employees", "active_employees", "sessions", "keys", "subscriptions"}
	mock.ExpectQuery(`(?s)SELECT e\.id.*jsonb_agg.*es\.enterprise_id = e\.id.*FROM enterprises AS e`).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(int64(7), "Acme", "acme.example.com", int64(99), "active", createdAt, 5, "admin@example.com", 5, int64(12), int64(10), int64(3), int64(8), subscriptions).
			AddRow(int64(8), "Beta", "beta.example.com", int64(98), "active", createdAt, 1, "", 1, int64(0), int64(0), int64(0), int64(0), []byte("[]")))

	items, err := svc.ListPlatformEnterprises(context.Background(), "", "")

	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Len(t, items[0].Subscriptions, 1)
	require.Equal(t, int64(41), items[0].Subscriptions[0].ID)
	require.Equal(t, "active", items[0].Subscriptions[0].Status)
	require.Equal(t, "550.00000000", items[0].Subscriptions[0].WeeklyLimit)
	require.NotNil(t, items[0].Subscriptions[0].WeeklyWindowStart)
	require.Empty(t, items[1].Subscriptions)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRefreshPlatformEnterpriseSubscriptionsLinksCurrentUpstreamSubscription(t *testing.T) {
	svc, mock := newMockService(t)
	anchor := time.Date(2026, 9, 26, 12, 30, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT dedicated_upstream_user_id\s+FROM enterprises\s+WHERE id = \$1\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"dedicated_upstream_user_id"}).AddRow(int64(99)))
	mock.ExpectQuery(`SELECT id, upstream_user_subscription_id\s+FROM enterprise_subscriptions\s+WHERE enterprise_id = \$1 AND status = 'active'\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "upstream_user_subscription_id"}))
	mock.ExpectQuery(`(?s)SELECT subscription\.id, subscription\.weekly_window_start.*FROM user_subscriptions AS subscription.*subscription\.user_id = \$1.*FOR SHARE OF subscription`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}).AddRow(int64(42), anchor))
	mock.ExpectQuery(`(?s)INSERT INTO enterprise_subscriptions.*RETURNING id`).
		WithArgs(int64(7), int64(42), anchor, "platform_user:12").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(88)))
	mock.ExpectExec(`(?s)INSERT INTO enterprise_audit_events.*subscription\.refreshed`).
		WithArgs(int64(7), int64(88), sqlmock.AnyArg(), "platform_user:12").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := svc.RefreshPlatformEnterpriseSubscriptions(context.Background(), 7, 12)

	require.NoError(t, err)
	require.Equal(t, "linked", result.Action)
	require.EqualValues(t, 42, *result.UpstreamUserSubscriptionID)
	require.EqualValues(t, 88, *result.EnterpriseSubscriptionID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRefreshPlatformEnterpriseSubscriptionsEndsStaleRelationshipWhenNoCurrentSource(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT dedicated_upstream_user_id\s+FROM enterprises\s+WHERE id = \$1\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"dedicated_upstream_user_id"}).AddRow(int64(99)))
	mock.ExpectQuery(`SELECT id, upstream_user_subscription_id\s+FROM enterprise_subscriptions\s+WHERE enterprise_id = \$1 AND status = 'active'\s+FOR UPDATE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "upstream_user_subscription_id"}).AddRow(int64(88), int64(42)))
	mock.ExpectQuery(`(?s)SELECT subscription\.id, subscription\.weekly_window_start.*FROM user_subscriptions AS subscription.*subscription\.user_id = \$1.*FOR SHARE OF subscription`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}))
	mock.ExpectExec(`(?s)UPDATE enterprise_subscriptions.*SET status = 'ended'.*WHERE id = \$1 AND status = 'active'`).
		WithArgs(int64(88), "platform_user:12").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?s)INSERT INTO enterprise_audit_events.*subscription\.refreshed`).
		WithArgs(int64(7), int64(88), sqlmock.AnyArg(), "platform_user:12").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := svc.RefreshPlatformEnterpriseSubscriptions(context.Background(), 7, 12)

	require.NoError(t, err)
	require.Equal(t, "unlinked", result.Action)
	require.Nil(t, result.UpstreamUserSubscriptionID)
	require.Nil(t, result.EnterpriseSubscriptionID)
	require.NoError(t, mock.ExpectationsWereMet())
}
