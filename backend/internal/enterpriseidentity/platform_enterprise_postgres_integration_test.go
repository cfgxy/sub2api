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

func TestGetPlatformEnterprisePostgreSQLReturnsSubscriptionsInStableOrder(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(
		ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("enterprise_identity_test"),
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
		`CREATE TABLE users (id BIGINT PRIMARY KEY, email TEXT NOT NULL)`,
		`CREATE TABLE groups (id BIGINT PRIMARY KEY, name TEXT NOT NULL, weekly_limit_usd NUMERIC(20, 8))`,
		`CREATE TABLE enterprises (
			id BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			portal_host TEXT NOT NULL,
			dedicated_upstream_user_id BIGINT NOT NULL,
			admin_user_id BIGINT NOT NULL,
			status TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE user_subscriptions (
			id BIGINT PRIMARY KEY,
			group_id BIGINT NOT NULL,
			starts_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE enterprise_subscriptions (
			id BIGINT PRIMARY KEY,
			enterprise_id BIGINT NOT NULL,
			upstream_user_subscription_id BIGINT NOT NULL,
			status TEXT NOT NULL,
			observed_weekly_window_start TIMESTAMPTZ
		)`,
		`CREATE TABLE enterprise_employees (enterprise_id BIGINT NOT NULL, status TEXT NOT NULL)`,
		`CREATE TABLE enterprise_sessions (enterprise_id BIGINT NOT NULL, revoked_at TIMESTAMPTZ, expires_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE api_keys (id BIGINT PRIMARY KEY, status TEXT NOT NULL)`,
		`CREATE TABLE enterprise_key_assignments (enterprise_id BIGINT NOT NULL, api_key_id BIGINT NOT NULL, status TEXT NOT NULL)`,
	} {
		_, err = db.ExecContext(ctx, statement)
		require.NoError(t, err)
	}

	activeStart := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	scheduledStart := activeStart.AddDate(0, 0, 7)
	for _, query := range []struct {
		statement string
		args      []any
	}{
		{`INSERT INTO users (id, email) VALUES (99, 'admin@example.com')`, nil},
		{`INSERT INTO groups (id, name, weekly_limit_usd) VALUES (1, '周订阅', 550), (2, '下周订阅', 600)`, nil},
		{`INSERT INTO enterprises (id, name, portal_host, dedicated_upstream_user_id, admin_user_id, status, created_at)
			VALUES (7, 'Acme', 'acme.example.com', 99, 99, 'active', $1)`, []any{activeStart}},
		{`INSERT INTO user_subscriptions (id, group_id, starts_at, expires_at) VALUES
			(41, 1, $1, $2), (42, 2, $2, $3)`, []any{activeStart, scheduledStart, scheduledStart.AddDate(0, 0, 7)}},
		{`INSERT INTO enterprise_subscriptions (id, enterprise_id, upstream_user_subscription_id, status, observed_weekly_window_start) VALUES
			(41, 7, 41, 'active', $1), (42, 7, 42, 'scheduled', NULL)`, []any{activeStart}},
	} {
		_, err = db.ExecContext(ctx, query.statement, query.args...)
		require.NoError(t, err)
	}

	svc := NewService(db, nil, nil, nil, nil)
	item, err := svc.GetPlatformEnterprise(ctx, 7)

	require.NoError(t, err)
	require.Len(t, item.Subscriptions, 2)
	require.Equal(t, int64(41), item.Subscriptions[0].ID)
	require.Equal(t, "active", item.Subscriptions[0].Status)
	require.Equal(t, int64(42), item.Subscriptions[1].ID)
	require.Equal(t, "scheduled", item.Subscriptions[1].Status)
}
