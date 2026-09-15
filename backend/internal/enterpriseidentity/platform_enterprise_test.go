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
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "dedicated", "status", "created_at"}).
			AddRow(int64(7), "Acme", "acme.example.com", int64(99), "active", createdAt))
	mock.ExpectQuery(`(?s)SELECT COALESCE.*FROM enterprises AS e WHERE e.id = \$1`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"admin_email", "subscriptions"}).AddRow("admin@example.com", subscriptions))
	mock.ExpectQuery(`(?s)SELECT.*COUNT\(\*\).*enterprise_employees`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"employees", "active_employees", "sessions", "keys"}).AddRow(int64(12), int64(10), int64(3), int64(8)))

	item, err := svc.GetPlatformEnterprise(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, "admin@example.com", item.AdminEmail)
	require.Equal(t, int64(12), item.EmployeeCount)
	require.Equal(t, int64(10), item.ActiveEmployeeCount)
	require.Equal(t, int64(3), item.ActiveSessionCount)
	require.Equal(t, int64(8), item.ActiveKeyCount)
	require.Len(t, item.Subscriptions, 2)
	require.Equal(t, "550.00000000", item.Subscriptions[0].WeeklyLimit)
	require.Equal(t, "scheduled", item.Subscriptions[1].Status)
	require.NoError(t, mock.ExpectationsWereMet())
}
