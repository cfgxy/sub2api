//go:build integration

package enterpriseidentity

import (
	"context"
	"database/sql"
	"testing"
	"time"

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
