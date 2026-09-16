//go:build integration

package enterpriseidentity

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	postgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestListEmployeeUsagePostgreSQLFiltersWindowAndPaginates 在真实 PostgreSQL 上验证
// e-03 新增的时间窗口筛选 + 分页 SQL 路径：assignment_generation 是 235/237 迁移新增列，
// sqlmock 无法暴露列不存在或类型不匹配类缺陷（SHAN-267 教训），必须用真实 schema 校验。
func TestListEmployeeUsagePostgreSQLFiltersWindowAndPaginates(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(
		ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("employee_usage_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(ctx))

	// 表结构裁剪自 235/237 迁移后的真实列集：window_anchor（237 由 weekly_window_anchor 重命名）、
	// classification（235）、assignment_generation/api_key_id/request_at（237 新增，NOT NULL）。
	for _, statement := range []string{
		`CREATE TABLE usage_logs (
			id BIGINT PRIMARY KEY,
			actual_cost NUMERIC(20, 10) NOT NULL
		)`,
		`CREATE TABLE api_keys (
			id BIGINT PRIMARY KEY,
			key TEXT NOT NULL
		)`,
		`CREATE TABLE enterprise_usage_attributions (
			id BIGSERIAL PRIMARY KEY,
			enterprise_id BIGINT NOT NULL,
			employee_id BIGINT,
			subscription_id BIGINT NOT NULL,
			window_type VARCHAR(10) NOT NULL,
			window_anchor TIMESTAMPTZ NOT NULL,
			usage_log_id BIGINT NOT NULL,
			request_at TIMESTAMPTZ NOT NULL,
			api_key_id BIGINT NOT NULL,
			assignment_generation BIGINT NOT NULL CHECK (assignment_generation >= 0),
			classification VARCHAR(40) NOT NULL CHECK (classification IN ('employee', 'controlled_external'))
		)`,
	} {
		_, err = db.ExecContext(ctx, statement)
		require.NoError(t, err)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO api_keys (id, key) VALUES (501, 'sk-abcdef1234567890')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO usage_logs (id, actual_cost) VALUES (1, 1.00), (2, 2.00), (3, 3.00), (4, 4.00)
	`)
	require.NoError(t, err)

	anchor := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	inWindow1 := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	inWindow2 := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	outOfWindow := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions
			(enterprise_id, employee_id, subscription_id, window_type, window_anchor, usage_log_id, request_at, api_key_id, assignment_generation, classification)
		VALUES
			(7, 22, 101, 'week', $1, 1, $2, 501, 1, 'employee'),
			(7, 22, 101, 'week', $1, 2, $3, 501, 2, 'employee'),
			(7, 22, 101, 'week', $1, 3, $4, 501, 2, 'employee'),
			(7, 99, 101, 'week', $1, 4, $2, 501, 1, 'employee')
	`, anchor, inWindow1, inWindow2, outOfWindow)
	require.NoError(t, err)

	svc := &Service{db: db}
	start := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	items, total, err := svc.ListEmployeeUsage(ctx, 7, 22, EmployeeUsageQuery{StartAt: &start, EndAt: &end, Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(2), total, "employee 99 和窗口外的请求都不应计入")
	require.Len(t, items, 1, "page_size=1 只应返回一行")
	require.Equal(t, "sk-abc...7890", items[0].APIKeyMasked)
	require.Equal(t, inWindow2, items[0].RequestAt.UTC(), "按 request_at DESC 排序，最新一条在第一页")
	require.Equal(t, int64(2), items[0].Generation)

	page2, total2, err := svc.ListEmployeeUsage(ctx, 7, 22, EmployeeUsageQuery{StartAt: &start, EndAt: &end, Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(2), total2)
	require.Len(t, page2, 1)
	require.Equal(t, inWindow1, page2[0].RequestAt.UTC())

	trend, err := svc.ListEmployeeUsageTrend(ctx, 7, 22, EmployeeUsageQuery{StartAt: &start, EndAt: &end})
	require.NoError(t, err)
	require.Len(t, trend, 2, "两天各一个真实聚合点，不补齐无数据的日期")
}

// TestGetEmployeeUsagePostgreSQLResolvesWeeklyLimitViaGroups
// 在真实 PostgreSQL 上验证 GetEmployeeUsage 的 SQL 语义契约：
// weekly_limit_usd 只存在于 groups（003_subscription.sql ALTER TABLE groups ADD COLUMN），
// user_subscriptions 无此列——列引用错误必须在集成层暴露（sqlmock 无法捕获，见历史教训
// employee_usage_test.go：sqlmock 以含该列的行断言，掩盖了真实 schema 下的 500）。
func TestGetEmployeeUsagePostgreSQLResolvesWeeklyLimitViaGroups(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(
		ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("employee_usage_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(ctx))

	// 表结构按生产迁移裁剪：user_subscriptions 忠实于 003_subscription.sql——
	// 只有 weekly_usage_usd / weekly_window_start，没有 weekly_limit_usd；
	// weekly_limit_usd 只存在于 groups。
	for _, statement := range []string{
		`CREATE TABLE enterprises (
			id BIGSERIAL PRIMARY KEY,
			dedicated_upstream_user_id BIGINT NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'active'
		)`,
		`CREATE TABLE enterprise_employees (
			enterprise_id BIGINT NOT NULL,
			id BIGINT NOT NULL,
			current_email TEXT,
			email TEXT NOT NULL,
			department_id BIGINT,
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			PRIMARY KEY (enterprise_id, id)
		)`,
		`CREATE TABLE usage_logs (
			id BIGINT PRIMARY KEY,
			actual_cost NUMERIC(20, 10) NOT NULL
		)`,
		`CREATE TABLE enterprise_usage_attributions (
			id BIGSERIAL PRIMARY KEY,
			enterprise_id BIGINT NOT NULL,
			employee_id BIGINT,
			subscription_id BIGINT NOT NULL,
			window_type VARCHAR(10) NOT NULL,
			window_anchor TIMESTAMPTZ NOT NULL,
			usage_log_id BIGINT NOT NULL,
			request_at TIMESTAMPTZ NOT NULL,
			api_key_id BIGINT,
			classification VARCHAR(20) NOT NULL DEFAULT 'employee'
		)`,
		`CREATE TABLE enterprise_weekly_allocations (
			enterprise_id BIGINT NOT NULL,
			subscription_id BIGINT NOT NULL,
			window_type VARCHAR(10) NOT NULL,
			window_anchor TIMESTAMPTZ NOT NULL,
			employee_id BIGINT NOT NULL,
			amount NUMERIC(20, 10) NOT NULL,
			version INT NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE user_subscriptions (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL,
			group_id BIGINT,
			deleted_at TIMESTAMPTZ,
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			weekly_usage_usd DECIMAL(20, 10) NOT NULL DEFAULT 0,
			weekly_window_start TIMESTAMPTZ
		)`,
		`CREATE TABLE groups (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			weekly_limit_usd DECIMAL(20, 8) DEFAULT NULL
		)`,
		`CREATE TABLE enterprise_subscriptions (
			id BIGSERIAL PRIMARY KEY,
			enterprise_id BIGINT NOT NULL,
			upstream_user_subscription_id BIGINT NOT NULL,
			status VARCHAR(20) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	} {
		_, err = db.ExecContext(ctx, statement)
		require.NoError(t, err)
	}

	anchor := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	_, err = db.ExecContext(ctx, `INSERT INTO enterprises (id, dedicated_upstream_user_id, status) VALUES (7, 900, 'active')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO user_subscriptions (id, user_id, group_id, deleted_at, status, weekly_usage_usd, weekly_window_start)
		VALUES (55, 900, 31, NULL, 'active', 220.0000000000, $1)`, anchor)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO groups (id, name, weekly_limit_usd) VALUES (31, 'Business', 550.00000000)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_subscriptions (id, enterprise_id, upstream_user_subscription_id, status, created_at)
		VALUES (13, 7, 55, 'active', $1)`, anchor)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, id, current_email, email, department_id, status) VALUES
			(7, 22, NULL, 'employee-zero@example.com', 3, 'active')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_weekly_allocations (enterprise_id, subscription_id, window_type, window_anchor, employee_id, amount) VALUES
			(7, 13, 'week', $1, 22, 200.0000000000)`, anchor)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs (id, actual_cost) VALUES (1, 50.0000000000)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions
			(enterprise_id, employee_id, subscription_id, window_type, window_anchor, usage_log_id, request_at, classification) VALUES
			(7, 22, 13, 'week', $1, 1, $1, 'employee')`, anchor)
	require.NoError(t, err)

	service := NewService(db, &config.Config{JWT: config.JWTConfig{Secret: "0123456789abcdef0123456789abcdef"}}, nil, nil, nil)

	t.Run("employee with active subscription resolves without column-does-not-exist 500", func(t *testing.T) {
		result, err := service.GetEmployeeUsage(ctx, 7, 22)
		require.NoError(t, err)
		require.Equal(t, "available", result.SourceStatus)
		require.NotNil(t, result.SubscriptionID)
		require.Equal(t, int64(13), *result.SubscriptionID)
		require.Equal(t, "200.00000000", result.Allocation)
		require.Equal(t, "50.00000000", result.ActualCost)
	})

	t.Run("employee without any subscription does not 500", func(t *testing.T) {
		_, err = db.ExecContext(ctx, `
			INSERT INTO enterprises (id, dedicated_upstream_user_id, status) VALUES (8, 901, 'active')`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `
			INSERT INTO enterprise_employees (enterprise_id, id, current_email, email, department_id, status) VALUES
				(8, 24, NULL, 'employee-nosub@example.com', 3, 'active')`)
		require.NoError(t, err)

		result, err := service.GetEmployeeUsage(ctx, 8, 24)
		require.NoError(t, err)
		require.Equal(t, "unavailable", result.SourceStatus)
		require.Equal(t, "0", result.Allocation)
	})
}
