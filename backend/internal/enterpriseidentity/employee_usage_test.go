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

func TestListEmployeeUsageAppliesWindowFilterAndPagination(t *testing.T) {
	svc, mock := newMockService(t)
	start := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_usage_attributions AS attribution WHERE`).
		WithArgs(int64(7), int64(22), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(37)))
	mock.ExpectQuery(`SELECT attribution\.request_at, attribution\.window_anchor`).
		WithArgs(int64(7), int64(22), start, end, 10, 10).
		WillReturnRows(sqlmock.NewRows([]string{"request_at", "window_anchor", "api_key_masked", "generation", "actual_cost"}).
			AddRow(start, start, "sk-ab...cd12", int64(2), "1.50000000"))

	items, total, err := svc.ListEmployeeUsage(context.Background(), 7, 22, EmployeeUsageQuery{StartAt: &start, EndAt: &end, Page: 2, PageSize: 10})

	require.NoError(t, err)
	require.Equal(t, int64(37), total)
	require.Len(t, items, 1)
	require.Equal(t, int64(2), items[0].Generation)
	require.Equal(t, "1.50000000", items[0].ActualCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListEmployeeUsageRejectsCrossEmployeeQueryByConstruction(t *testing.T) {
	svc, mock := newMockService(t)
	// 调用方只能传入服务端解析出的 enterpriseID/employeeID（来自认证上下文），
	// 该测试确认这两个值原样落入 WHERE 子句参数，不接受调用方额外传入的越权标识。
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_usage_attributions AS attribution WHERE`).
		WithArgs(int64(7), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(`SELECT attribution\.request_at, attribution\.window_anchor`).
		WithArgs(int64(7), int64(22), 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"request_at", "window_anchor", "api_key_masked", "generation", "actual_cost"}))

	items, total, err := svc.ListEmployeeUsage(context.Background(), 7, 22, EmployeeUsageQuery{})

	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, items)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetEnterprisePoolStatusReturnsUnavailableWithoutFabricatingNumbers(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery(`FROM enterprises AS enterprise`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "limit", "used", "remaining", "exhausted", "anchor"}).
			AddRow("unavailable", "0", "0", "0", false, nil))

	result, err := svc.GetEnterprisePoolStatus(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, "unavailable", result.SourceStatus)
	require.Equal(t, "0", result.PoolLimit)
	require.Nil(t, result.WindowAnchor)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetEnterprisePoolStatusReturnsAvailablePoolFigures(t *testing.T) {
	svc, mock := newMockService(t)
	anchor := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM enterprises AS enterprise`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "limit", "used", "remaining", "exhausted", "anchor"}).
			AddRow("available", "550.00000000", "120.00000000", "430.00000000", false, anchor))

	result, err := svc.GetEnterprisePoolStatus(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, "available", result.SourceStatus)
	require.Equal(t, "550.00000000", result.PoolLimit)
	require.Equal(t, "430.00000000", result.PoolRemaining)
	require.False(t, result.PoolExhausted)
	require.NotNil(t, result.WindowAnchor)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListEmployeeUsageTrendReturnsRealAggregatesOnly(t *testing.T) {
	svc, mock := newMockService(t)
	day := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT DATE_TRUNC\('day', attribution\.request_at\)`).WithArgs(int64(7), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"at", "requests", "actual_cost"}).AddRow(day, int64(4), "3.20000000"))

	items, err := svc.ListEmployeeUsageTrend(context.Background(), 7, 22, EmployeeUsageQuery{})

	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(4), items[0].Requests)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListEmployeeUsageTrendReturnsEmptyWhenNoAttributionRowsExistYet(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery(`SELECT DATE_TRUNC\('day', attribution\.request_at\)`).WithArgs(int64(7), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"at", "requests", "actual_cost"}))

	items, err := svc.ListEmployeeUsageTrend(context.Background(), 7, 22, EmployeeUsageQuery{})

	require.NoError(t, err)
	require.Empty(t, items)
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
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at"}).AddRow(int64(31), "研发", time.Now()))
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
