//go:build integration

package enterpriseidentity

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	postgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestGetWorkbenchSummaryPostgreSQLResolvesPoolLimitAndOverageRecommendation
// 在真实 PostgreSQL 上验证 getSummary 的两条 SQL 语义契约：
//  1. 权威池上限经 groups.weekly_limit_usd 取值（与 m-06 ListSubscriptionAllocations 同口径），
//     user_subscriptions 无 weekly_limit_usd 列，列引用错误必须在集成层暴露（sqlmock 无法捕获）。
//  2. 员工 overage 由 GREATEST(...)::text 生成，零值是定长小数字符串（如 "0.0000000000"），
//     Recommendation 必须按数值语义判断，零 overage 员工不得误标「核对个人超用」。
func TestGetWorkbenchSummaryPostgreSQLResolvesPoolLimitAndOverageRecommendation(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(
		ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("workbench_test"),
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
	// weekly_limit_usd 只存在于 groups（003 迁移 ALTER TABLE groups ADD COLUMN）。
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
			api_key_id BIGINT
		)`,
		`CREATE TABLE enterprise_weekly_allocations (
			enterprise_id BIGINT NOT NULL,
			subscription_id BIGINT NOT NULL,
			window_type VARCHAR(10) NOT NULL,
			window_anchor TIMESTAMPTZ NOT NULL,
			employee_id BIGINT NOT NULL,
			amount NUMERIC(20, 10) NOT NULL
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
		`CREATE TABLE enterprise_subscription_windows (
			id BIGSERIAL PRIMARY KEY,
			enterprise_id BIGINT NOT NULL,
			subscription_id BIGINT NOT NULL,
			observed_at TIMESTAMPTZ NOT NULL,
			observed_weekly_window_start TIMESTAMPTZ
		)`,
	} {
		_, err = db.ExecContext(ctx, statement)
		require.NoError(t, err)
	}

	anchor := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	observedAt := time.Date(2026, 9, 8, 10, 32, 0, 0, time.UTC)
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
		INSERT INTO enterprise_subscription_windows (enterprise_id, subscription_id, observed_at, observed_weekly_window_start)
		VALUES (7, 13, $1, $2)`, observedAt, anchor)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, id, current_email, email, department_id, status) VALUES
			(7, 22, NULL, 'employee-zero@example.com', 3, 'active'),
			(7, 23, NULL, 'employee-over@example.com', 3, 'active')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_weekly_allocations (enterprise_id, subscription_id, window_type, window_anchor, employee_id, amount) VALUES
			(7, 13, 'week', $1, 22, 200.0000000000),
			(7, 13, 'week', $1, 23, 100.0000000000)`, anchor)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs (id, actual_cost) VALUES (1, 50.0000000000), (2, 150.0000000000)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions
			(enterprise_id, employee_id, subscription_id, window_type, window_anchor, usage_log_id, request_at) VALUES
			(7, 22, 13, 'week', $1, 1, $2),
			(7, 23, 13, 'week', $1, 2, $2)`, anchor, anchor)
	require.NoError(t, err)

	service := NewService(db, &config.Config{JWT: config.JWTConfig{Secret: "0123456789abcdef0123456789abcdef"}}, nil, nil, nil)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}
	result, err := handler.getSummary(ctx, 7, workbenchQuery{})
	require.NoError(t, err)

	t.Run("authoritative pool limit resolves via groups.weekly_limit_usd", func(t *testing.T) {
		require.Equal(t, "available", result.PoolSourceStatus)
		require.Equal(t, "550.00000000", result.EnterprisePoolLimit)
		require.Equal(t, "220.00000000", result.EnterprisePoolUsed)
		require.Equal(t, "330.00000000", result.EnterprisePoolRemaining)
		require.NotNil(t, result.SubscriptionID)
		require.Equal(t, int64(13), *result.SubscriptionID)
		require.NotNil(t, result.PoolWindowAnchor)
		require.True(t, result.PoolWindowAnchor.UTC().Equal(anchor))
		require.NotNil(t, result.PoolObservedAt)
		require.True(t, result.PoolObservedAt.UTC().Equal(observedAt))
		require.False(t, result.EnterprisePoolExhausted)
	})

	t.Run("zero overage employee is not flagged for personal overage", func(t *testing.T) {
		byEmployee := map[int64]WorkbenchEmployeeSummary{}
		for _, item := range result.EmployeeSummaries {
			byEmployee[item.EmployeeID] = item
		}
		zeroOverage, ok := byEmployee[22]
		require.True(t, ok, "employee 22 (allocation 200 / usage 50) 应出现在员工摘要中")
		// 锁定实测形态：GREATEST(负差值, 0) 取整数字面量分支时 text 输出为 "0"（dscale=0），
		// 非零值输出为定长小数字符串（"50.0000000000"）——零值形态依赖类型推导，字符串比较不可靠，
		// Recommendation 必须按数值语义判断；该断言同时防止未来把输出归一成定长形态后回退为字符串比较。
		require.Equal(t, "0", zeroOverage.OverageCredit)
		require.Equal(t, "当前 allocation 范围内", zeroOverage.Recommendation)

		overaged, ok := byEmployee[23]
		require.True(t, ok, "employee 23 (allocation 100 / usage 150) 应出现在员工摘要中")
		require.Equal(t, "50.0000000000", overaged.OverageCredit)
		require.Equal(t, "核对个人超用，并按业务需要调整 allocation", overaged.Recommendation)
	})
}
