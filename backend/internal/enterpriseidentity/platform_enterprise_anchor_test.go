package enterpriseidentity

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// SHAN-382 缺口①：候选订阅锚点未物化（weekly_window_start IS NULL）时，
// 创建企业事务内按"首次消费"语义做条件锚点初始化，观测锚点取写入值。
func TestCreateEnterpriseInitializesMissingAnchorAsFirstConsumption(t *testing.T) {
	svc, mock := newMockService(t)
	fixed := time.Date(2026, 9, 26, 12, 30, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT email, status FROM users WHERE id = \$1 FOR SHARE`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"email", "status"}).AddRow("owner@example.com", "active"))
	mock.ExpectQuery(`SELECT subscription\.id, subscription\.weekly_window_start`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}).AddRow(int64(5), nil))
	mock.ExpectQuery(`UPDATE user_subscriptions\s+SET daily_window_start = \$2, weekly_window_start = \$3, monthly_window_start = \$3`).
		WithArgs(int64(5), timezone.StartOfDay(fixed), fixed).
		WillReturnRows(sqlmock.NewRows([]string{"weekly_window_start"}).AddRow(fixed))
	mock.ExpectQuery(`INSERT INTO enterprises`).WithArgs("Acme", int64(99), "acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "dedicated", "status", "created_at"}).AddRow(int64(7), "Acme", "acme.example.com", int64(99), "active", fixed))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), sqlmock.AnyArg(), "platform_user:1").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`INSERT INTO enterprise_subscriptions`).WithArgs(int64(7), int64(5), fixed, "platform_user:1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), int64(11), sqlmock.AnyArg(), "platform_user:1").WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()

	item, err := svc.CreateEnterprise(context.Background(), CreateEnterpriseInput{Name: "Acme", Host: "acme.example.com", DedicatedUpstreamUser: 99, Reason: "new tenant"}, 1)

	require.NoError(t, err)
	require.Equal(t, int64(7), item.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口①：锚点已被并发激活（UPDATE 零行命中）时保留既有值，不覆盖原锚点。
func TestCreateEnterprisePreservesAnchorWhenConcurrentlyActivated(t *testing.T) {
	svc, mock := newMockService(t)
	fixed := time.Date(2026, 9, 26, 12, 30, 0, 0, time.UTC)
	existing := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT email, status FROM users WHERE id = \$1 FOR SHARE`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"email", "status"}).AddRow("owner@example.com", "active"))
	mock.ExpectQuery(`SELECT subscription\.id, subscription\.weekly_window_start`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}).AddRow(int64(5), nil))
	mock.ExpectQuery(`UPDATE user_subscriptions\s+SET daily_window_start = \$2, weekly_window_start = \$3, monthly_window_start = \$3`).
		WithArgs(int64(5), timezone.StartOfDay(fixed), fixed).
		WillReturnRows(sqlmock.NewRows([]string{"weekly_window_start"}))
	mock.ExpectQuery(`SELECT weekly_window_start FROM user_subscriptions WHERE id = \$1`).WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"weekly_window_start"}).AddRow(existing))
	mock.ExpectQuery(`INSERT INTO enterprises`).WithArgs("Acme", int64(99), "acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "dedicated", "status", "created_at"}).AddRow(int64(7), "Acme", "acme.example.com", int64(99), "active", fixed))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), sqlmock.AnyArg(), "platform_user:1").WillReturnResult(sqlmock.NewResult(1, 1))
	// 观测锚点 = 既有值，而非当前时刻
	mock.ExpectQuery(`INSERT INTO enterprise_subscriptions`).WithArgs(int64(7), int64(5), existing, "platform_user:1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), int64(11), sqlmock.AnyArg(), "platform_user:1").WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()

	_, err := svc.CreateEnterprise(context.Background(), CreateEnterpriseInput{Name: "Acme", Host: "acme.example.com", DedicatedUpstreamUser: 99, Reason: "new tenant"}, 1)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-382 缺口①：锚点初始化本身失败时整单回滚，企业零写入。
func TestCreateEnterpriseRollsBackWhenAnchorInitializationFails(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT email, status FROM users WHERE id = \$1 FOR SHARE`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"email", "status"}).AddRow("owner@example.com", "active"))
	mock.ExpectQuery(`SELECT subscription\.id, subscription\.weekly_window_start`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}).AddRow(int64(5), nil))
	mock.ExpectQuery(`UPDATE user_subscriptions\s+SET daily_window_start = \$2, weekly_window_start = \$3, monthly_window_start = \$3`).
		WithArgs(int64(5), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(context.DeadlineExceeded)
	mock.ExpectRollback()

	_, err := svc.CreateEnterprise(context.Background(), CreateEnterpriseInput{Name: "Acme", Host: "acme.example.com", DedicatedUpstreamUser: 99, Reason: "new tenant"}, 1)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, mock.ExpectationsWereMet())
}
