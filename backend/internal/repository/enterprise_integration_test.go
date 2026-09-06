//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type enterpriseFixture struct {
	enterpriseID           int64
	employeeID             int64
	secondEmployee         int64
	upstreamSubscriptionID int64
	subscriptionID         int64
	apiKeyID               int64
	accountID              int64
	groupID                int64
	anchor                 time.Time
}

type enterpriseNoopAuthCacheInvalidator struct{}

func (enterpriseNoopAuthCacheInvalidator) InvalidateAuthCacheByKey(context.Context, string) {}

func TestEnterpriseSchemaConstraints(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)

	expectEnterpriseConstraintError(t, ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, status)
		SELECT enterprise_id, ' ' || UPPER(email) || ' ', 'active'
		FROM enterprise_employees WHERE id = $1
	`, fixture.employeeID)
	expectEnterpriseConstraintName(t, ctx, "uq_enterprise_subscriptions_one_active", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			observed_weekly_window_start, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, NOW(), 'test:duplicate-active')
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID, fixture.anchor)
	expectEnterpriseConstraintName(t, ctx, "uq_enterprise_key_assignments_active_api_key", `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
			upstream_group_id, status, actor_ref
		) VALUES ($1, $2, $3, $4, $5, 'active', 'test:duplicate-key')
	`, fixture.enterpriseID, fixture.secondEmployee, fixture.apiKeyID,
		fixture.upstreamSubscriptionID, fixture.groupID)

	secondAPIKeyID := insertAPIKey(t, ctx, fixture.enterpriseID)
	expectEnterpriseConstraintName(t, ctx, "uq_enterprise_key_assignments_active_employee", `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
			upstream_group_id, status, actor_ref
		) VALUES ($1, $2, $3, $4, $5, 'active', 'test:duplicate-employee')
	`, fixture.enterpriseID, fixture.employeeID, secondAPIKeyID,
		fixture.upstreamSubscriptionID, fixture.groupID)

	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_employees_status_disabled_at", `
		INSERT INTO enterprise_employees (enterprise_id, email, status, disabled_at)
		VALUES ($1, 'invalid-employee@example.com', 'disabled', NULL)
	`, fixture.enterpriseID)
	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_subscriptions_status_timestamps", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			observed_weekly_window_start, actor_ref
		) VALUES ($1, $2, 'active', NULL, 'test:invalid-subscription')
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID)
	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_key_assignments_status_times", `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
			upstream_group_id, status, actor_ref
		) VALUES ($1, $2, $3, $4, $5, 'revoked', 'test:invalid-key')
	`, fixture.enterpriseID, fixture.secondEmployee, secondAPIKeyID,
		fixture.upstreamSubscriptionID, fixture.groupID)
}

func TestEnterpriseUsageAttributionUsageLogForeignKeyAndIdempotency(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	expectEnterpriseConstraintName(t, ctx, "enterprise_usage_attributions_usage_log_id_fkey", `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, 999999999, $4, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, fixture.anchor)

	usageID := insertUsageLog(t, ctx, fixture, "0.1000000000")
	attribution, err := repo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		EmployeeID:         &fixture.employeeID,
		UsageLogID:         usageID,
		WeeklyWindowAnchor: fixture.anchor,
		Classification:     "employee",
	})
	require.NoError(t, err)
	require.Equal(t, usageID, attribution.UsageLogID)
	stored, err := repo.GetUsageAttributionByUsageLogID(ctx, usageID)
	require.NoError(t, err)
	require.Equal(t, attribution.ID, stored.ID)
	replayed, err := repo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		EmployeeID:         &fixture.employeeID,
		UsageLogID:         usageID,
		WeeklyWindowAnchor: fixture.anchor,
		Classification:     "employee",
	})
	require.NoError(t, err)
	require.Equal(t, attribution.ID, replayed.ID)
	expectEnterpriseConstraintName(t, ctx, "enterprise_usage_attributions_usage_log_id_key", `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, usageID, fixture.anchor)
}

func TestEnterpriseAllocationRepositoryConcurrentRevision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	allocation, err := repo.CreateAllocation(ctx, enterprise.CreateAllocationParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: fixture.anchor,
		EmployeeID:         fixture.employeeID,
		Amount:             "10.12345678",
		Reason:             "initial allocation",
		ActorRef:           "test:create",
	})
	require.NoError(t, err)
	require.Equal(t, "10.12345678", allocation.Amount)
	require.Equal(t, int64(1), allocation.Version)

	start := make(chan struct{})
	type reviseResult struct {
		amount string
		err    error
	}
	results := make(chan reviseResult, 2)
	for _, amount := range []string{"11.00000001", "12.00000002"} {
		amount := amount
		go func() {
			<-start
			_, reviseErr := repo.ReviseAllocation(ctx, enterprise.ReviseAllocationParams{
				AllocationID:    allocation.ID,
				ExpectedVersion: 1,
				Amount:          amount,
				Reason:          "concurrent revision",
				ActorRef:        "test:concurrent",
			})
			results <- reviseResult{amount: amount, err: reviseErr}
		}()
	}
	close(start)

	var success, conflict int
	var winningAmount string
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			success++
			winningAmount = result.amount
		case errors.Is(result.err, enterprise.ErrAllocationVersionConflict):
			conflict++
		default:
			require.NoError(t, result.err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)

	var version int64
	var revisionCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT version FROM enterprise_weekly_allocations WHERE id = $1", allocation.ID).Scan(&version))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM enterprise_allocation_revisions WHERE allocation_id = $1", allocation.ID).Scan(&revisionCount))
	require.Equal(t, int64(2), version)
	require.Equal(t, 2, revisionCount)

	var previous, current string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT previous_amount::text, new_amount::text
		FROM enterprise_allocation_revisions
		WHERE allocation_id = $1 AND version = 2
	`, allocation.ID).Scan(&previous, &current))
	require.Equal(t, "10.12345678", previous)
	require.Equal(t, winningAmount, current)
	revisions, err := repo.ListAllocationRevisions(ctx, allocation.ID)
	require.NoError(t, err)
	require.Len(t, revisions, 2)
}

func TestEnterpriseAllocationRevisionFailureRollsBackUpdate(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	allocation, err := repo.CreateAllocation(ctx, enterprise.CreateAllocationParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: fixture.anchor,
		EmployeeID:         fixture.employeeID,
		Amount:             "3.00000000",
		Reason:             "initial allocation",
		ActorRef:           "test:create",
	})
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(ctx, `
		ALTER TABLE enterprise_allocation_revisions
		ADD CONSTRAINT shan151_reject_revision_reason CHECK (reason <> 'force-failure')
	`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `
			ALTER TABLE enterprise_allocation_revisions
			DROP CONSTRAINT IF EXISTS shan151_reject_revision_reason
		`)
	})

	_, err = repo.ReviseAllocation(ctx, enterprise.ReviseAllocationParams{
		AllocationID:    allocation.ID,
		ExpectedVersion: 1,
		Amount:          "9.00000000",
		Reason:          "force-failure",
		ActorRef:        "test:failure",
	})
	require.Error(t, err)

	var amount string
	var version int64
	var revisionCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT amount::text, version
		FROM enterprise_weekly_allocations
		WHERE id = $1
	`, allocation.ID).Scan(&amount, &version))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM enterprise_allocation_revisions WHERE allocation_id = $1", allocation.ID).Scan(&revisionCount))
	require.Equal(t, "3.00000000", amount)
	require.Equal(t, int64(1), version)
	require.Equal(t, 1, revisionCount)
}

func TestEnterpriseAllocationUsageSummaryAddsDecimalsWithoutFloatDrift(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err := repo.CreateAllocation(ctx, enterprise.CreateAllocationParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: fixture.anchor,
		EmployeeID:         fixture.employeeID,
		Amount:             "0.10000000",
		Reason:             "weekly allocation",
		ActorRef:           "test:create",
	})
	require.NoError(t, err)

	for _, actualCost := range []string{"0.1000000000", "0.2000000000"} {
		usageLogID := insertUsageLog(t, ctx, fixture, actualCost)
		_, err = integrationDB.ExecContext(ctx, `
			INSERT INTO enterprise_usage_attributions (
				enterprise_id, subscription_id, employee_id, usage_log_id,
				weekly_window_anchor, classification
			) VALUES ($1, $2, $3, $4, $5, 'employee')
		`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, usageLogID, fixture.anchor)
		require.NoError(t, err)
	}

	summary, err := repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: fixture.anchor,
		EmployeeID:         fixture.employeeID,
	})
	require.NoError(t, err)
	require.Equal(t, "0.10000000", summary.Allocation)
	require.Equal(t, "0.30000000", summary.ActualCost)
	require.Equal(t, "0.00000000", summary.Remaining)
	require.Equal(t, "0.20000000", summary.Overage)
}

func TestEnterpriseMigrationRunnerRepeatAndTransactionalRollback(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, integrationDB))
	require.NoError(t, ApplyMigrations(ctx, integrationDB))

	failingFS := fstest.MapFS{
		"236_failure_probe.sql": {Data: []byte(`
			CREATE TABLE shan151_partial_write_probe (id BIGINT PRIMARY KEY);
			THIS IS INVALID SQL;
		`)},
	}
	err := applyMigrationsFS(ctx, integrationDB, failingFS)
	require.Error(t, err)

	var tableExists bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT to_regclass('public.shan151_partial_write_probe') IS NOT NULL
	`).Scan(&tableExists))
	require.False(t, tableExists)
}

func TestEnterprise236RefusesNonEmptyEnterpriseMappings(t *testing.T) {
	ctx := context.Background()
	seedEnterpriseFixture(t, ctx)
	migration, err := dbmigrations.FS.ReadFile("236_enterprise_frozen_contract.sql")
	require.NoError(t, err)

	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.ErrorContains(t, err, "requires empty enterprise tables")
	require.NoError(t, tx.Rollback())
}

func seedEnterpriseFixture(t *testing.T, ctx context.Context) enterpriseFixture {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	anchor := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)

	var userID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id
	`, "enterprise-"+suffix+"@example.com").Scan(&userID))

	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO groups (name) VALUES ($1) RETURNING id
	`, "enterprise-group-"+suffix).Scan(&groupID))

	var userSubscriptionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO user_subscriptions (
			user_id, group_id, starts_at, expires_at, status, weekly_window_start
		) VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active', $3::timestamptz) RETURNING id
	`, userID, groupID, anchor).Scan(&userSubscriptionID))

	var enterpriseID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprises (name, dedicated_upstream_user_id)
		VALUES ($1, $2) RETURNING id
	`, "Enterprise "+suffix, userID).Scan(&enterpriseID))

	var employeeID, secondEmployeeID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, status)
		VALUES ($1, $2, 'active') RETURNING id
	`, enterpriseID, "member-"+suffix+"@example.com").Scan(&employeeID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, status)
		VALUES ($1, $2, 'active') RETURNING id
	`, enterpriseID, "second-"+suffix+"@example.com").Scan(&secondEmployeeID))

	var subscriptionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			observed_weekly_window_start, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, $3, 'test:seed') RETURNING id
	`, enterpriseID, userSubscriptionID, anchor).Scan(&subscriptionID))

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_subscription_windows (
			enterprise_id, subscription_id, upstream_user_subscription_id,
			observed_weekly_window_start, window_start, window_end
		) VALUES ($1, $2, $3, $4::timestamptz, $4::timestamptz, $4::timestamptz + INTERVAL '7 days')
	`, enterpriseID, subscriptionID, userSubscriptionID, anchor)
	require.NoError(t, err)

	apiKeyID := insertAPIKeyForUser(t, ctx, userID, groupID, suffix)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
			upstream_group_id, status, actor_ref, assigned_at
		) VALUES ($1, $2, $3, $4, $5, 'active', 'test:seed', $6)
	`, enterpriseID, employeeID, apiKeyID, userSubscriptionID, groupID, anchor)
	require.NoError(t, err)

	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO accounts (name, platform, type) VALUES ($1, 'anthropic', 'apikey') RETURNING id
	`, "enterprise-account-"+suffix).Scan(&accountID))

	t.Cleanup(func() {
		cleanupEnterpriseFixture(t, enterpriseID, userID, suffix)
	})

	return enterpriseFixture{
		enterpriseID:           enterpriseID,
		employeeID:             employeeID,
		secondEmployee:         secondEmployeeID,
		upstreamSubscriptionID: userSubscriptionID,
		subscriptionID:         subscriptionID,
		apiKeyID:               apiKeyID,
		accountID:              accountID,
		groupID:                groupID,
		anchor:                 anchor,
	}
}

func cleanupEnterpriseFixture(t *testing.T, enterpriseID, userID int64, suffix string) {
	t.Helper()
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM enterprise_usage_attributions WHERE enterprise_id = $1", []any{enterpriseID}},
		{"DELETE FROM usage_logs WHERE user_id = $1", []any{userID}},
		{"DELETE FROM enterprise_allocation_revisions WHERE enterprise_id = $1", []any{enterpriseID}},
		{"DELETE FROM enterprise_weekly_allocations WHERE enterprise_id = $1", []any{enterpriseID}},
		{"DELETE FROM enterprise_audit_events WHERE enterprise_id = $1", []any{enterpriseID}},
		{"DELETE FROM enterprise_key_assignments WHERE enterprise_id = $1", []any{enterpriseID}},
		{"DELETE FROM enterprise_subscription_windows WHERE enterprise_id = $1", []any{enterpriseID}},
		{"DELETE FROM enterprise_subscriptions WHERE enterprise_id = $1", []any{enterpriseID}},
		{"DELETE FROM enterprise_employees WHERE enterprise_id = $1", []any{enterpriseID}},
		{"DELETE FROM enterprise_departments WHERE enterprise_id = $1", []any{enterpriseID}},
		{"DELETE FROM enterprises WHERE id = $1", []any{enterpriseID}},
		{"DELETE FROM api_keys WHERE user_id = $1", []any{userID}},
		{`WITH deleted AS (
			DELETE FROM user_subscriptions WHERE user_id = $1 RETURNING group_id
		)
		DELETE FROM groups WHERE id IN (SELECT group_id FROM deleted)`, []any{userID}},
		{"DELETE FROM accounts WHERE name = $1", []any{"enterprise-account-" + suffix}},
		{"DELETE FROM users WHERE id = $1", []any{userID}},
	}
	for _, statement := range statements {
		_, err = tx.ExecContext(ctx, statement.query, statement.args...)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
}

func insertAPIKey(t *testing.T, ctx context.Context, enterpriseID int64) int64 {
	t.Helper()
	var userID, groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT enterprise.dedicated_upstream_user_id, subscription.group_id
		FROM enterprises AS enterprise
		JOIN user_subscriptions AS subscription
		  ON subscription.user_id = enterprise.dedicated_upstream_user_id
		WHERE enterprise.id = $1
		ORDER BY subscription.id
		LIMIT 1
	`, enterpriseID).Scan(&userID, &groupID))
	return insertAPIKeyForUser(t, ctx, userID, groupID, fmt.Sprintf("%d", time.Now().UnixNano()))
}

func insertAPIKeyForUser(t *testing.T, ctx context.Context, userID, groupID int64, suffix string) int64 {
	t.Helper()
	var apiKeyID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, group_id, key, name)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, userID, groupID, "sk-enterprise-"+suffix, "enterprise-key-"+suffix).Scan(&apiKeyID))
	return apiKeyID
}

func insertUsageLog(t *testing.T, ctx context.Context, fixture enterpriseFixture, actualCost string) int64 {
	return insertUsageLogForSubscription(t, ctx, fixture, fixture.upstreamSubscriptionID, actualCost)
}

func insertUsageLogForSubscription(
	t *testing.T,
	ctx context.Context,
	fixture enterpriseFixture,
	upstreamSubscriptionID int64,
	actualCost string,
) int64 {
	t.Helper()
	var userID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT dedicated_upstream_user_id FROM enterprises WHERE id = $1", fixture.enterpriseID).Scan(&userID))
	var usageLogID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO usage_logs (user_id, api_key_id, account_id, subscription_id, model, actual_cost)
		VALUES ($1, $2, $3, $4, 'enterprise-test', $5::numeric) RETURNING id
	`, userID, fixture.apiKeyID, fixture.accountID, upstreamSubscriptionID, actualCost).Scan(&usageLogID))
	return usageLogID
}

func updateEnterpriseSubscriptionSource(
	t *testing.T,
	ctx context.Context,
	fixture enterpriseFixture,
	upstreamSubscriptionID int64,
) {
	t.Helper()
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE enterprise_subscriptions
		SET upstream_user_subscription_id = $2
		WHERE id = $1
	`, fixture.subscriptionID, upstreamSubscriptionID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE enterprise_subscription_windows
		SET upstream_user_subscription_id = $2
		WHERE subscription_id = $1
	`, fixture.subscriptionID, upstreamSubscriptionID)
	require.NoError(t, err)
}

func expectEnterpriseConstraintError(t *testing.T, ctx context.Context, query string, args ...any) {
	t.Helper()
	tx, err := integrationDB.BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, query, args...)
	require.Error(t, err)
	require.NoError(t, tx.Rollback())
}

func expectEnterpriseConstraintName(t *testing.T, ctx context.Context, constraint, query string, args ...any) {
	t.Helper()
	tx, err := integrationDB.BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, query, args...)
	var pqErr *pq.Error
	require.ErrorAs(t, err, &pqErr)
	require.Equal(t, constraint, pqErr.Constraint)
	require.NoError(t, tx.Rollback())
}
