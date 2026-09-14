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

func TestBuildEmployeeSummaryQueryPostgreSQLAggregatesSubscriptionWindows(t *testing.T) {
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

	for _, statement := range []string{
		`CREATE TABLE enterprise_employees (
			enterprise_id BIGINT NOT NULL,
			id BIGINT NOT NULL,
			current_email TEXT,
			email TEXT NOT NULL,
			department_id BIGINT,
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
	} {
		_, err = db.ExecContext(ctx, statement)
		require.NoError(t, err)
	}

	anchor := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, id, current_email, email, department_id)
		VALUES (7, 22, NULL, 'employee@example.com', 3)
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO usage_logs (id, actual_cost) VALUES
			(1, 1.25), (2, 0.75), (3, 5.00), (4, 9.00)
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions
			(enterprise_id, employee_id, subscription_id, window_type, window_anchor, usage_log_id, request_at)
		VALUES
			(7, 22, 101, 'week', $1, 1, $1),
			(7, 22, 101, 'week', $1, 2, $1),
			(7, 22, 202, 'week', $1, 3, $1),
			(7, 22, 101, 'day', $1, 4, $1)
	`, anchor)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_weekly_allocations
			(enterprise_id, subscription_id, window_type, window_anchor, employee_id, amount)
		VALUES
			(7, 101, 'week', $1, 22, 10.50),
			(7, 202, 'week', $1, 22, 20.25),
			(7, 101, 'day', $1, 22, 99.00)
	`, anchor)
	require.NoError(t, err)

	scope := buildWorkbenchUsageScope(7, workbenchQuery{WindowType: "week"})
	rows, err := db.QueryContext(ctx, buildEmployeeSummaryQuery(scope), scope.args...)
	require.NoError(t, err)
	defer rows.Close()

	require.True(t, rows.Next())
	var employeeID, requests int64
	var email, configuredCredit, usageCredit string
	var departmentID *int64
	require.NoError(t, rows.Scan(&employeeID, &email, &departmentID, &requests, &configuredCredit, &usageCredit))
	require.Equal(t, int64(22), employeeID)
	require.Equal(t, "employee@example.com", email)
	require.Equal(t, int64(3), *departmentID)
	require.Equal(t, int64(3), requests)
	require.Equal(t, "30.7500000000", configuredCredit)
	require.Equal(t, "7.0000000000", usageCredit)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}
