//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestEnterprise237MigratesNonEmptyLegacyAttribution(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "236_enterprise_frozen_contract.sql")))
	usageLogID, apiKeyID, generation, createdAt := seedEnterprise237LegacyAttribution(t, ctx, db)

	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, "237_enterprise_subscription_allocations.sql")))

	var windowType, classification string
	var migratedAPIKeyID, migratedGeneration int64
	var requestAt time.Time
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT window_type, api_key_id, assignment_generation, request_at, classification
		FROM enterprise_usage_attributions WHERE usage_log_id = $1
	`, usageLogID).Scan(&windowType, &migratedAPIKeyID, &migratedGeneration, &requestAt, &classification))
	require.Equal(t, "week", windowType)
	require.Equal(t, apiKeyID, migratedAPIKeyID)
	require.Equal(t, generation, migratedGeneration)
	require.Equal(t, createdAt, requestAt)
	require.Equal(t, "employee", classification)
}

func TestEnterprise237MaintainsAPIKeyAttributionCandidate(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "236_enterprise_frozen_contract.sql")))

	var enterpriseUserID, laterEnterpriseUserID, ordinaryUserID, groupID int64
	for _, target := range []*int64{&enterpriseUserID, &laterEnterpriseUserID, &ordinaryUserID} {
		require.NoError(t, db.QueryRowContext(ctx,
			"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
			fmt.Sprintf("shan154-candidate-%d@example.com", time.Now().UnixNano())).Scan(target))
	}
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO groups (name) VALUES ($1) RETURNING id",
		fmt.Sprintf("shan154-candidate-%d", time.Now().UnixNano())).Scan(&groupID))
	_, err := db.ExecContext(ctx,
		"INSERT INTO enterprises (name, dedicated_upstream_user_id) VALUES ('Candidate Existing', $1)",
		enterpriseUserID)
	require.NoError(t, err)

	insertKey := func(userID int64, suffix string) int64 {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO api_keys (user_id, group_id, key, name)
			VALUES ($1, $2, $3, $4) RETURNING id
		`, userID, groupID, fmt.Sprintf("sk-candidate-%s-%d", suffix, time.Now().UnixNano()), suffix).Scan(&id))
		return id
	}
	existingEnterpriseKeyID := insertKey(enterpriseUserID, "existing-enterprise")
	laterEnterpriseKeyID := insertKey(laterEnterpriseUserID, "later-enterprise")
	ordinaryKeyID := insertKey(ordinaryUserID, "ordinary")

	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, "237_enterprise_subscription_allocations.sql")))
	assertCandidate := func(keyID int64, expected bool) {
		var actual bool
		require.NoError(t, db.QueryRowContext(ctx,
			"SELECT enterprise_attribution_candidate FROM api_keys WHERE id = $1", keyID).Scan(&actual))
		require.Equal(t, expected, actual)
	}
	assertCandidate(existingEnterpriseKeyID, true)
	assertCandidate(laterEnterpriseKeyID, false)
	assertCandidate(ordinaryKeyID, false)

	postMigrationKeyID := insertKey(enterpriseUserID, "post-migration")
	assertCandidate(postMigrationKeyID, true)
	var laterEnterpriseKey string
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT key FROM api_keys WHERE id = $1", laterEnterpriseKeyID).Scan(&laterEnterpriseKey))
	_, err = db.ExecContext(ctx, `
		DELETE FROM auth_cache_invalidation_outbox
		WHERE cache_key = encode(sha256(convert_to($1, 'UTF8')), 'hex')
	`, laterEnterpriseKey)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		"INSERT INTO enterprises (name, dedicated_upstream_user_id) VALUES ('Candidate Later', $1)",
		laterEnterpriseUserID)
	require.NoError(t, err)
	assertCandidate(laterEnterpriseKeyID, true)
	var invalidationCount int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM auth_cache_invalidation_outbox
		WHERE cache_key = encode(sha256(convert_to($1, 'UTF8')), 'hex')
	`, laterEnterpriseKey).Scan(&invalidationCount))
	require.Equal(t, 1, invalidationCount)
}

func TestEnterprise237RollbackFailsClosedWithDayState(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "237_enterprise_subscription_allocations.sql")))
	seedEnterprise237MigrationAllocation(t, ctx, db, "day")

	_, err := db.ExecContext(ctx, readEnterprise237Rollback(t))
	require.ErrorContains(t, err, "cannot rollback SHAN-154 while day/month allocation state exists")
}

func TestEnterprise237RollbackRestoresPureWeekSchemaAndData(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "237_enterprise_subscription_allocations.sql")))
	allocationID := seedEnterprise237MigrationAllocation(t, ctx, db, "week")

	_, err := db.ExecContext(ctx, readEnterprise237Rollback(t))
	require.NoError(t, err)
	var legacyAnchorExists, windowTypeExists, candidateColumnExists bool
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'enterprise_weekly_allocations'
			  AND column_name = 'weekly_window_anchor'
		), EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'enterprise_weekly_allocations'
			  AND column_name = 'window_type'
			), EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'public' AND table_name = 'api_keys'
				  AND column_name = 'enterprise_attribution_candidate'
			)
		`).Scan(&legacyAnchorExists, &windowTypeExists, &candidateColumnExists))
	require.True(t, legacyAnchorExists)
	require.False(t, windowTypeExists)
	require.False(t, candidateColumnExists)
	var amount string
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT amount::text FROM enterprise_weekly_allocations WHERE id = $1", allocationID).Scan(&amount))
	require.Equal(t, "3.00000000", amount)
}

func TestEnterprise237RollbackRejectsMissingUsageRowBeforeSchemaChanges(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "236_enterprise_frozen_contract.sql")))
	usageLogID, _, _, _ := seedEnterprise237LegacyAttribution(t, ctx, db)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, "237_enterprise_subscription_allocations.sql")))
	_, err := db.ExecContext(ctx, "DELETE FROM usage_logs WHERE id = $1", usageLogID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, readEnterprise237Rollback(t))
	require.ErrorContains(t, err, "cannot rollback SHAN-154 while enterprise usage attribution references a missing usage log; settle or clean up dangling attributions first")

	var windowTypeExists, weeklyWindowAnchorExists bool
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'enterprise_usage_attributions'
			  AND column_name = 'window_type'
		), EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'enterprise_usage_attributions'
			  AND column_name = 'weekly_window_anchor'
		)
	`).Scan(&windowTypeExists, &weeklyWindowAnchorExists))
	require.True(t, windowTypeExists)
	require.False(t, weeklyWindowAnchorExists)
}

func seedEnterprise237LegacyAttribution(t *testing.T, ctx context.Context, db *sql.DB) (int64, int64, int64, time.Time) {
	t.Helper()
	suffix := time.Now().UnixNano()
	anchor := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	createdAt := anchor.Add(time.Hour)
	var userID, groupID, upstreamSubscriptionID, enterpriseID, employeeID, subscriptionID, apiKeyID, accountID, usageLogID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
		fmt.Sprintf("shan154-legacy-%d@example.com", suffix)).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO groups (name) VALUES ($1) RETURNING id",
		fmt.Sprintf("shan154-legacy-%d", suffix)).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO user_subscriptions (user_id, group_id, starts_at, expires_at, status, weekly_window_start)
		VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active', $3::timestamptz) RETURNING id
	`, userID, groupID, anchor).Scan(&upstreamSubscriptionID))
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO enterprises (name, dedicated_upstream_user_id) VALUES ('Legacy Attribution', $1) RETURNING id",
		userID).Scan(&enterpriseID))
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO enterprise_employees (enterprise_id, email) VALUES ($1, $2) RETURNING id",
		enterpriseID, fmt.Sprintf("legacy-employee-%d@example.com", suffix)).Scan(&employeeID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status, observed_weekly_window_start, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, $3, 'test:legacy') RETURNING id
	`, enterpriseID, upstreamSubscriptionID, anchor).Scan(&subscriptionID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, group_id, key, name)
		VALUES ($1, $2, $3, 'legacy-key') RETURNING id
	`, userID, groupID, fmt.Sprintf("sk-legacy-%d", suffix)).Scan(&apiKeyID))
	const generation int64 = 7
	_, err := db.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
			upstream_group_id, generation, status, actor_ref, assigned_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'active', 'test:legacy', $7)
	`, enterpriseID, employeeID, apiKeyID, upstreamSubscriptionID, groupID, generation, anchor)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO accounts (name, platform, type) VALUES ($1, 'anthropic', 'apikey') RETURNING id",
		fmt.Sprintf("legacy-account-%d", suffix)).Scan(&accountID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO usage_logs (user_id, api_key_id, account_id, subscription_id, model, actual_cost, created_at)
		VALUES ($1, $2, $3, $4, 'legacy-test', 1, $5) RETURNING id
	`, userID, apiKeyID, accountID, upstreamSubscriptionID, createdAt).Scan(&usageLogID))
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id, weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, enterpriseID, subscriptionID, employeeID, usageLogID, anchor)
	require.NoError(t, err)
	return usageLogID, apiKeyID, generation, createdAt
}

func seedEnterprise237MigrationAllocation(t *testing.T, ctx context.Context, db *sql.DB, windowType string) int64 {
	t.Helper()
	anchor := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	var userID, groupID, upstreamSubscriptionID, enterpriseID, employeeID, subscriptionID, allocationID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
		fmt.Sprintf("shan154-migration-%d@example.com", time.Now().UnixNano())).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO groups (name) VALUES ($1) RETURNING id",
		fmt.Sprintf("shan154-migration-%d", time.Now().UnixNano())).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO user_subscriptions (user_id, group_id, starts_at, expires_at, status, weekly_window_start)
		VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active', $3::timestamptz) RETURNING id
	`, userID, groupID, anchor).Scan(&upstreamSubscriptionID))
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO enterprises (name, dedicated_upstream_user_id) VALUES ('Migration Test', $1) RETURNING id",
		userID).Scan(&enterpriseID))
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO enterprise_employees (enterprise_id, email) VALUES ($1, $2) RETURNING id",
		enterpriseID, fmt.Sprintf("employee-%d@example.com", time.Now().UnixNano())).Scan(&employeeID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status, observed_weekly_window_start, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, $3, 'test:migration') RETURNING id
	`, enterpriseID, upstreamSubscriptionID, anchor).Scan(&subscriptionID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprise_weekly_allocations (
			enterprise_id, subscription_id, window_type, window_anchor, employee_id, amount
		) VALUES ($1, $2, $3, $4, $5, 3) RETURNING id
	`, enterpriseID, subscriptionID, windowType, anchor, employeeID).Scan(&allocationID))
	return allocationID
}

func readEnterprise237Rollback(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	content, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "migrations", "rollback", "237_enterprise_subscription_allocations.sql"))
	require.NoError(t, err)
	return string(content)
}

var enterprise235TriggerNames = []string{
	"validate_enterprise_subscription_owner",
	"protect_enterprise_subscription_update",
	"validate_enterprise_key_owner",
	"validate_enterprise_usage_attribution",
	"enterprise_subscription_windows_immutable",
	"enterprise_allocation_revisions_immutable",
	"enterprise_usage_attributions_immutable",
	"enterprise_audit_events_immutable",
	"enterprise_subscriptions_no_delete",
	"enterprise_employees_no_delete",
	"enterprise_weekly_allocations_no_delete",
	"enterprise_key_assignments_history",
}

var enterprise235FunctionNames = []string{
	"validate_enterprise_subscription_owner",
	"protect_enterprise_subscription_update",
	"validate_enterprise_key_owner",
	"validate_enterprise_usage_attribution",
	"reject_enterprise_history_mutation",
	"protect_enterprise_subscription_history",
	"protect_enterprise_key_assignment_history",
}

const enterprisePreflightMigration = "234_enterprise_foundation_preflight.sql"

func TestEnterprisePreflightRejectsPreexistingEmptyTargetTable(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsBefore(t, enterprisePreflightMigration)))
	_, err := db.ExecContext(ctx, "CREATE TABLE enterprises (id BIGSERIAL PRIMARY KEY)")
	require.NoError(t, err)

	err = applyMigrationsFS(ctx, db, migrationsThrough(t, "236_enterprise_frozen_contract.sql"))
	require.ErrorContains(t, err, "pre-existing enterprise table")
	require.True(t, relationExists(t, ctx, db, "enterprises"))
	for _, table := range []string{
		"enterprise_employees",
		"enterprise_subscriptions",
		"enterprise_subscription_windows",
		"enterprise_key_assignments",
		"enterprise_weekly_allocations",
		"enterprise_allocation_revisions",
		"enterprise_usage_attributions",
		"enterprise_audit_events",
		"enterprise_departments",
	} {
		require.False(t, relationExists(t, ctx, db, table), table)
	}
	require.Zero(t, migrationRecordCount(t, ctx, db, enterprisePreflightMigration))
	require.Zero(t, migrationRecordCount(t, ctx, db, "235_enterprise_foundation.sql"))
}

func TestEnterprisePreflightAllowsCleanDatabaseThrough236(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "236_enterprise_frozen_contract.sql")))

	for _, migration := range []string{
		enterprisePreflightMigration,
		"235_enterprise_foundation.sql",
		"236_enterprise_frozen_contract.sql",
	} {
		require.Equal(t, 1, migrationRecordCount(t, ctx, db, migration), migration)
	}
	require.True(t, relationExists(t, ctx, db, "enterprise_departments"))
}

func TestEnterprisePreflightBackfillsDatabaseWith235And236AlreadyApplied(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db,
		migrationsThroughExcluding(t, "236_enterprise_frozen_contract.sql", enterprisePreflightMigration)))
	require.Zero(t, migrationRecordCount(t, ctx, db, enterprisePreflightMigration))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, "235_enterprise_foundation.sql"))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, "236_enterprise_frozen_contract.sql"))

	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, enterprisePreflightMigration)))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, enterprisePreflightMigration))
	require.True(t, relationExists(t, ctx, db, "enterprise_departments"))
}

func TestEnterprise236MigratesIndependent235Database(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "235_enterprise_foundation.sql")))
	require.Equal(t, len(enterprise235TriggerNames), countNamedEnterpriseTriggers(t, ctx, db))
	require.Equal(t, len(enterprise235FunctionNames), countNamedEnterpriseFunctions(t, ctx, db))

	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, "236_enterprise_frozen_contract.sql")))

	require.True(t, relationExists(t, ctx, db, "enterprise_departments"))
	require.False(t, columnExists(t, ctx, db, "enterprise_subscriptions", "effective_window_anchor"))
	require.True(t, columnExists(t, ctx, db, "enterprise_subscriptions", "observed_weekly_window_start"))
	require.Equal(t, "YES", columnNullable(t, ctx, db, "enterprise_subscriptions", "observed_weekly_window_start"))
	for _, column := range []string{"upstream_user_subscription_id", "upstream_group_id", "generation"} {
		require.Equal(t, "NO", columnNullable(t, ctx, db, "enterprise_key_assignments", column), column)
	}
	for _, constraint := range []string{
		"ck_enterprise_key_assignments_status_times",
		"enterprise_usage_attributions_usage_log_id_fkey",
	} {
		require.True(t, constraintExists(t, ctx, db, constraint), constraint)
	}
	for _, index := range []string{
		"uq_enterprise_subscriptions_one_scheduled",
		"uq_enterprise_key_assignments_active_api_key",
		"uq_enterprise_subscription_windows_upstream_window",
	} {
		require.True(t, relationExists(t, ctx, db, index), index)
	}
	require.Zero(t, countNamedEnterpriseTriggers(t, ctx, db))
	require.Zero(t, countNamedEnterpriseFunctions(t, ctx, db))
}

func TestEnterprise236NonEmptyDatabaseRollsBackWithoutPartialSchema(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "235_enterprise_foundation.sql")))

	var userID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ('shan151-migration-nonempty@example.com', 'test')
		RETURNING id
	`).Scan(&userID))
	_, err := db.ExecContext(ctx, `
		INSERT INTO enterprises (name, dedicated_upstream_user_id)
		VALUES ('SHAN-151 nonempty probe', $1)
	`, userID)
	require.NoError(t, err)

	err = applyMigrationsFS(ctx, db, migrationOnly(t, "236_enterprise_frozen_contract.sql"))
	require.ErrorContains(t, err, "requires empty enterprise tables")

	require.False(t, relationExists(t, ctx, db, "enterprise_departments"))
	require.True(t, columnExists(t, ctx, db, "enterprise_subscriptions", "effective_window_anchor"))
	require.False(t, columnExists(t, ctx, db, "enterprise_subscriptions", "observed_weekly_window_start"))
	require.Equal(t, len(enterprise235TriggerNames), countNamedEnterpriseTriggers(t, ctx, db))
	require.Equal(t, len(enterprise235FunctionNames), countNamedEnterpriseFunctions(t, ctx, db))

	var migrationCount, enterpriseCount int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = '236_enterprise_frozen_contract.sql'").Scan(&migrationCount))
	require.Zero(t, migrationCount)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM enterprises").Scan(&enterpriseCount))
	require.Equal(t, 1, enterpriseCount)
}

func newIndependentMigrationDatabase(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("shan151_migration_%d", time.Now().UnixNano())
	_, err := integrationDB.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(t, err)

	parsed, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	parsed.Path = "/" + name
	db, err := openSQLWithRetry(ctx, parsed.String(), 30*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = integrationDB.ExecContext(context.Background(),
			"DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
	})
	return db
}

func migrationsThrough(t *testing.T, last string) fstest.MapFS {
	return migrationsThroughExcluding(t, last)
}

func migrationsThroughExcluding(t *testing.T, last string, excluded ...string) fstest.MapFS {
	t.Helper()
	files, err := fs.Glob(dbmigrations.FS, "*.sql")
	require.NoError(t, err)
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		excludedSet[name] = struct{}{}
	}
	result := fstest.MapFS{}
	for _, name := range files {
		if name > last {
			continue
		}
		if _, skip := excludedSet[name]; skip {
			continue
		}
		data, readErr := fs.ReadFile(dbmigrations.FS, name)
		require.NoError(t, readErr)
		result[name] = &fstest.MapFile{Data: data}
	}
	return result
}

func migrationsBefore(t *testing.T, first string) fstest.MapFS {
	t.Helper()
	files, err := fs.Glob(dbmigrations.FS, "*.sql")
	require.NoError(t, err)
	result := fstest.MapFS{}
	for _, name := range files {
		if name >= first {
			continue
		}
		data, readErr := fs.ReadFile(dbmigrations.FS, name)
		require.NoError(t, readErr)
		result[name] = &fstest.MapFile{Data: data}
	}
	return result
}

func migrationOnly(t *testing.T, name string) fstest.MapFS {
	t.Helper()
	data, err := fs.ReadFile(dbmigrations.FS, name)
	require.NoError(t, err)
	return fstest.MapFS{name: &fstest.MapFile{Data: data}}
}

func countNamedEnterpriseTriggers(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pg_trigger
		WHERE NOT tgisinternal AND tgname = ANY($1)
	`, pq.Array(enterprise235TriggerNames)).Scan(&count))
	return count
}

func countNamedEnterpriseFunctions(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pg_proc
		JOIN pg_namespace ON pg_namespace.oid = pg_proc.pronamespace
		WHERE pg_namespace.nspname = 'public' AND pg_proc.proname = ANY($1)
	`, pq.Array(enterprise235FunctionNames)).Scan(&count))
	return count
}

func relationExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('public.' || $1) IS NOT NULL", name).Scan(&exists))
	return exists
}

func columnExists(t *testing.T, ctx context.Context, db *sql.DB, table, column string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)
	`, table, column).Scan(&exists))
	return exists
}

func columnNullable(t *testing.T, ctx context.Context, db *sql.DB, table, column string) string {
	t.Helper()
	var nullable string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
	`, table, column).Scan(&nullable))
	return nullable
}

func constraintExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)", name).Scan(&exists))
	return exists
}

func migrationRecordCount(t *testing.T, ctx context.Context, db *sql.DB, name string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = $1", name).Scan(&count))
	return count
}
