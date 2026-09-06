//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"testing"
	"testing/fstest"
	"time"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

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
