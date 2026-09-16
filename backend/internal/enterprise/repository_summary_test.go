package enterprise

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestGetAllocationUsageSummaryReturnsCurrentAllocationRevision(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repository{db: db}
	anchor := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT CASE \$4::text.*FROM enterprise_subscriptions`).WithArgs(int64(7), int64(11), int64(22), "week", int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"current_anchor", "limit"}).AddRow(anchor, "100.00000000"))
	mock.ExpectQuery(`(?s)WITH latest_configurations.*SELECT configured.id, configured.version`).WithArgs(int64(7), int64(11), anchor, int64(22), "week", "100.00000000").
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "configured", "used", "remaining", "overage", "allocated", "overallocated"}).
			AddRow(int64(31), int64(4), "25.00000000", "2.00000000", "23.00000000", "0.00000000", "25.00000000", "0.00000000"))

	result, err := repo.GetAllocationUsageSummary(context.Background(), AllocationUsageSummaryQuery{
		EnterpriseID: 7, SubscriptionID: 11, EmployeeID: 22, RequesterUserID: 0,
		WindowType: WindowTypeWeek, WindowAnchor: anchor,
	})

	require.NoError(t, err)
	require.Equal(t, int64(31), result.AllocationID)
	require.Equal(t, int64(4), result.AllocationVersion)
	require.NoError(t, mock.ExpectationsWereMet())
}
