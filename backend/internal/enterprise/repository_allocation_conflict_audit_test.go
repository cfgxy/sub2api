package enterprise

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSetAllocationVersionMismatchOnExistingRowWritesRejectedAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repository{db: db}
	anchor := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT CASE \$1::text.*FROM enterprise_subscriptions`).
		WithArgs(WindowTypeWeek, int64(7), int64(11), int64(0), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"current_anchor"}).AddRow(anchor))
	mock.ExpectQuery(`(?s)SELECT id, amount::text, version, created_at, updated_at\s+FROM enterprise_weekly_allocations`).
		WithArgs(int64(7), int64(11), int64(22), WindowTypeWeek, anchor).
		WillReturnRows(sqlmock.NewRows([]string{"id", "amount", "version", "created_at", "updated_at"}).
			AddRow(int64(31), "25.00000000", int64(4), anchor, anchor))
	mock.ExpectExec(`(?s)INSERT INTO enterprise_audit_events.*'allocation.version_conflict'`).
		WithArgs(int64(7), sqlmock.AnyArg(), "user:0").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectRollback()

	_, err = repo.SetAllocation(context.Background(), SetAllocationParams{
		RequesterUserID: 0, EnterpriseID: 7, SubscriptionID: 11, EmployeeID: 22,
		WindowType: WindowTypeWeek, WindowAnchor: anchor, Credit: "10",
		ExpectedVersion: 1, Reason: "quarterly top-up",
	})

	require.ErrorIs(t, err, ErrAllocationVersionConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSetAllocationVersionMismatchOnMissingRowWritesRejectedAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repository{db: db}
	anchor := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT CASE \$1::text.*FROM enterprise_subscriptions`).
		WithArgs(WindowTypeWeek, int64(7), int64(11), int64(0), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"current_anchor"}).AddRow(anchor))
	mock.ExpectQuery(`(?s)SELECT id, amount::text, version, created_at, updated_at\s+FROM enterprise_weekly_allocations`).
		WithArgs(int64(7), int64(11), int64(22), WindowTypeWeek, anchor).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`(?s)SELECT amount::text, version\s+FROM enterprise_weekly_allocations`).
		WithArgs(int64(7), int64(11), int64(22), WindowTypeWeek, anchor).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`(?s)INSERT INTO enterprise_audit_events.*'allocation.version_conflict'`).
		WithArgs(int64(7), sqlmock.AnyArg(), "user:0").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectRollback()

	_, err = repo.SetAllocation(context.Background(), SetAllocationParams{
		RequesterUserID: 0, EnterpriseID: 7, SubscriptionID: 11, EmployeeID: 22,
		WindowType: WindowTypeWeek, WindowAnchor: anchor, Credit: "10",
		ExpectedVersion: 3, Reason: "quarterly top-up",
	})

	require.ErrorIs(t, err, ErrAllocationVersionConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}
