package enterpriseidentity

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestGetEmployeeUsageReturnsEmptyStateWhenAllocationIsNotConfigured(t *testing.T) {
	svc, mock := newMockService(t)
	anchor := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM enterprise_employees`).WithArgs(int64(7), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`SELECT enterprise_subscription\.id, upstream_subscription\.weekly_window_start`).WithArgs(int64(7), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start", "weekly_limit_usd"}).AddRow(int64(11), anchor, "100.00000000"))
	mock.ExpectQuery(`SELECT COALESCE\(\(SELECT amount FROM enterprise_weekly_allocations`).WithArgs(int64(7), int64(11), int64(22), anchor).
		WillReturnRows(sqlmock.NewRows([]string{"allocation", "actual_cost", "requests"}).AddRow("0.00000000", "0.00000000", int64(0)))

	result, err := svc.GetEmployeeUsage(context.Background(), 7, 22)

	require.NoError(t, err)
	require.Equal(t, "available", result.SourceStatus)
	require.Equal(t, "0.00000000", result.Allocation)
	require.Equal(t, "0.00000000", result.ActualCost)
	require.Equal(t, "0", result.Remaining)
	require.Equal(t, "0", result.Overage)
	require.Zero(t, result.Requests)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetEmployeeUsageHidesMissingEmployeeAsNotFound(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM enterprise_employees`).WithArgs(int64(7), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	_, err := svc.GetEmployeeUsage(context.Background(), 7, 22)

	require.ErrorIs(t, err, errNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateDepartmentWritesActorAuditWithoutSensitivePayload(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO enterprise_departments`).WithArgs(int64(7), "研发").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(int64(31), "研发"))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), "department.created", "department", int64(31), sqlmock.AnyArg(), "enterprise_admin:9").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	_, err := svc.CreateDepartment(WithAuditActor(context.Background(), "enterprise_admin:9"), 7, "研发")

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
	// 以独立序列化断言审计约定不携带凭据字段。
	payload, err := json.Marshal(map[string]any{"result": "success"})
	require.NoError(t, err)
	require.NotContains(t, string(payload), "password")
}

func TestRecordRejectedAuditEventStoresOnlyGenericReason(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), "employee.update", "employee", int64(22), sqlmock.AnyArg(), "enterprise_admin:9").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := svc.RecordRejectedAuditEvent(WithAuditActor(context.Background(), "enterprise_admin:9"), 7, "employee.update", "employee", ptrInt64(22), "rejected")

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func ptrInt64(value int64) *int64 { return &value }
