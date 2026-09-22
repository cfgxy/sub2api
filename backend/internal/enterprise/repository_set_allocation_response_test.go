package enterprise

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSetAllocationReturnsActorRefMatchingRevisionAndAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &Repository{db: db}
	anchor := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT CASE \$1::text.*FROM enterprise_subscriptions`).
		WithArgs(WindowTypeWeek, int64(7), int64(11), int64(42), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"current_anchor"}).AddRow(anchor))
	mock.ExpectQuery(`(?s)SELECT id, amount::text, version, created_at, updated_at\s+FROM enterprise_weekly_allocations`).
		WithArgs(int64(7), int64(11), int64(22), WindowTypeWeek, anchor).
		WillReturnRows(sqlmock.NewRows([]string{"id", "amount", "version", "created_at", "updated_at"}).
			AddRow(int64(31), "25.00000000", int64(4), anchor, anchor))
	mock.ExpectQuery(`(?s)UPDATE enterprise_weekly_allocations`).
		WithArgs("10.00000000", int64(31), int64(4)).
		WillReturnRows(sqlmock.NewRows([]string{"version", "updated_at"}).AddRow(int64(5), anchor))
	mock.ExpectExec(`(?s)INSERT INTO enterprise_allocation_revisions`).
		WithArgs(int64(7), int64(31), int64(5), "25.00000000", "10.00000000", "quarterly top-up", "user:42").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?s)INSERT INTO enterprise_audit_events.*'allocation.credit_changed'`).
		WithArgs(int64(7), int64(31), sqlmock.AnyArg(), "user:42").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	allocation, err := repo.SetAllocation(context.Background(), SetAllocationParams{
		RequesterUserID: 42, EnterpriseID: 7, SubscriptionID: 11, EmployeeID: 22,
		WindowType: WindowTypeWeek, WindowAnchor: anchor, Credit: "10",
		ExpectedVersion: 4, Reason: "quarterly top-up",
	})

	require.NoError(t, err)
	require.Equal(t, "user:42", allocation.ActorRef)
	require.Equal(t, int64(5), allocation.Version)
	require.Equal(t, "10.00000000", allocation.Credit)
	require.NoError(t, mock.ExpectationsWereMet())
}
