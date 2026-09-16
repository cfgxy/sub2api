package enterprise

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestListSubscriptionAllocationsAggregatesPoolAndPerEmployeeStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repository{db: db}
	anchor := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT upstream_subscription.weekly_window_start.*FROM enterprise_subscriptions`).
		WithArgs(int64(7), int64(11), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"current_anchor", "limit"}).AddRow(anchor, "2400.00000000"))

	mock.ExpectQuery(`(?s)WITH latest_configurations AS.*FROM enterprise_employees`).
		WithArgs(int64(7), int64(11), anchor, WindowTypeWeek).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "email", "department_id", "allocation_id", "allocation_version",
			"configured", "used", "remaining", "overage",
		}).
			AddRow(int64(1), "a@example.com", int64(10), int64(101), int64(3), "2200.00000000", "1980.00000000", "220.00000000", "32.00000000").
			AddRow(int64(2), "b@example.com", nil, int64(0), int64(0), "0.00000000", "0.00000000", "0.00000000", "0.00000000"))

	result, err := repo.ListSubscriptionAllocations(context.Background(), ListSubscriptionAllocationsQuery{
		EnterpriseID: 7, SubscriptionID: 11, RequesterUserID: 0,
		WindowType: WindowTypeWeek, WindowAnchor: anchor,
	})

	require.NoError(t, err)
	require.Equal(t, "available", result.PoolSourceStatus)
	require.NotNil(t, result.AuthoritativeLimit)
	require.Equal(t, "2400.00000000", *result.AuthoritativeLimit)
	require.Equal(t, "2200.00000000", result.AllocatedTotal)
	require.Equal(t, "200.00000000", result.UnallocatedTotal)
	require.Equal(t, "0.00000000", result.OverallocatedBy)
	require.Empty(t, result.Warning)
	require.Len(t, result.Items, 2)
	require.Equal(t, AllocationStatusOverage, result.Items[0].Status)
	require.Equal(t, AllocationStatusNormal, result.Items[1].Status)
	require.Nil(t, result.Items[1].DepartmentID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListSubscriptionAllocationsReportsOverallocationWarning(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repository{db: db}
	anchor := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT upstream_subscription.weekly_window_start.*FROM enterprise_subscriptions`).
		WithArgs(int64(7), int64(11), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"current_anchor", "limit"}).AddRow(anchor, "100.00000000"))

	mock.ExpectQuery(`(?s)WITH latest_configurations AS.*FROM enterprise_employees`).
		WithArgs(int64(7), int64(11), anchor, WindowTypeWeek).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "email", "department_id", "allocation_id", "allocation_version",
			"configured", "used", "remaining", "overage",
		}).
			AddRow(int64(1), "a@example.com", nil, int64(101), int64(1), "120.00000000", "0.00000000", "0.00000000", "0.00000000"))

	result, err := repo.ListSubscriptionAllocations(context.Background(), ListSubscriptionAllocationsQuery{
		EnterpriseID: 7, SubscriptionID: 11, RequesterUserID: 0,
		WindowType: WindowTypeWeek, WindowAnchor: anchor,
	})

	require.NoError(t, err)
	require.Equal(t, "0.00000000", result.UnallocatedTotal)
	require.Equal(t, "20.00000000", result.OverallocatedBy)
	require.Equal(t, AllocationWarningOverallocated, result.Warning)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListSubscriptionAllocationsRejectsCrossEnterpriseAccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repository{db: db}
	anchor := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT upstream_subscription.weekly_window_start.*FROM enterprise_subscriptions`).
		WithArgs(int64(7), int64(11), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"current_anchor", "limit"}))

	_, err = repo.ListSubscriptionAllocations(context.Background(), ListSubscriptionAllocationsQuery{
		EnterpriseID: 7, SubscriptionID: 11, RequesterUserID: 0,
		WindowType: WindowTypeWeek, WindowAnchor: anchor,
	})

	require.ErrorIs(t, err, ErrEnterpriseAccessDenied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListSubscriptionAllocationsRejectsNonWeeklyWindow(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repository{db: db}

	_, err = repo.ListSubscriptionAllocations(context.Background(), ListSubscriptionAllocationsQuery{
		EnterpriseID: 7, SubscriptionID: 11, RequesterUserID: 0,
		WindowType: "month", WindowAnchor: time.Now(),
	})

	require.Error(t, err)
}
