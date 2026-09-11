//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/enterpriseidentity"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
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
	require.False(t, columnExists(t, ctx, db, "enterprise_subscriptions", "effective_window_anchor"))
	require.True(t, columnExists(t, ctx, db, "enterprise_subscriptions", "observed_weekly_window_start"))
	require.Equal(t, len(enterprise235TriggerNames), countNamedEnterpriseTriggers(t, ctx, db))
	require.Equal(t, len(enterprise235FunctionNames), countNamedEnterpriseFunctions(t, ctx, db))

	var migrationCount, enterpriseCount int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = '236_enterprise_frozen_contract.sql'").Scan(&migrationCount))
	require.Zero(t, migrationCount)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM enterprises").Scan(&enterpriseCount))
	require.Equal(t, 1, enterpriseCount)
}

func TestEnterprise237IdentitySchemaAllowsScopedEmailAndRehire(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "237_enterprise_identity.sql")))

	for _, table := range []string{"enterprise_sessions", "enterprise_refresh_tokens", "enterprise_password_reset_tokens", "enterprise_branding"} {
		require.True(t, relationExists(t, ctx, db, table), table)
	}
	for _, column := range []string{"portal_host", "admin_user_id"} {
		require.Equal(t, "NO", columnNullable(t, ctx, db, "enterprises", column), column)
	}

	createEnterprise := func(email, host string) int64 {
		var userID, enterpriseID int64
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO users (email, password_hash) VALUES ($1, '$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu') RETURNING id
		`, email).Scan(&userID))
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO enterprises (name, dedicated_upstream_user_id, admin_user_id, portal_host)
			VALUES ($1, $2, $2, $3) RETURNING id
		`, email, userID, host).Scan(&enterpriseID))
		return enterpriseID
	}
	one := createEnterprise("admin-one@example.com", "one.example.com")
	two := createEnterprise("admin-two@example.com", "two.example.com")

	createEmployee := func(enterpriseID int64) int64 {
		var employeeID int64
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, initial_password_expires_at)
			VALUES ($1, 'same@example.com', 'same@example.com', '$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu', NOW() + INTERVAL '24 hours')
			RETURNING id
		`, enterpriseID).Scan(&employeeID))
		return employeeID
	}
	firstEmployeeID := createEmployee(one)
	require.NotZero(t, createEmployee(two), "same email must be valid in another enterprise")
	_, err := db.ExecContext(ctx, `
		UPDATE enterprise_employees
		SET status = 'terminated', current_email = NULL, terminated_at = NOW(), updated_at = NOW()
		WHERE enterprise_id = $1 AND id = $2
	`, one, firstEmployeeID)
	require.NoError(t, err)
	rehiredEmployeeID := createEmployee(one)
	require.NotEqual(t, firstEmployeeID, rehiredEmployeeID)
}

func TestEnterprise237RejectsExistingEnterpriseDataBeforeIdentityDDL(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "236_enterprise_frozen_contract.sql")))

	var userID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ('shan152-preflight@example.com', 'test')
		RETURNING id
	`).Scan(&userID))
	_, err := db.ExecContext(ctx, `
		INSERT INTO enterprises (name, dedicated_upstream_user_id)
		VALUES ('SHAN-152 preflight probe', $1)
	`, userID)
	require.NoError(t, err)

	err = applyMigrationsFS(ctx, db, migrationOnly(t, "237_enterprise_identity.sql"))
	require.ErrorContains(t, err, "SHAN-152 237 refuses existing enterprise data in enterprises")
	require.ErrorContains(t, err, "explicitly backfill portal_host, admin_user_id, and employee password_hash")
	require.False(t, columnExists(t, ctx, db, "enterprises", "portal_host"))
	require.False(t, columnExists(t, ctx, db, "enterprise_employees", "password_hash"))
	require.False(t, relationExists(t, ctx, db, "enterprise_sessions"))
	require.Zero(t, migrationRecordCount(t, ctx, db, "237_enterprise_identity.sql"))
}

func TestEnterprise238RollbackRestores235And236Foundation(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "238_enterprise_brand_object.sql")))

	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	rollbackSQL, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "deploy", "shan-152-enterprise-identity.rollback.sql"))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(rollbackSQL))
	require.NoError(t, err)

	for _, table := range []string{
		"enterprise_sessions",
		"enterprise_refresh_tokens",
		"enterprise_password_reset_tokens",
		"enterprise_branding",
	} {
		require.False(t, relationExists(t, ctx, db, table), table)
	}
	for _, column := range []string{
		"portal_host",
		"admin_user_id",
	} {
		require.False(t, columnExists(t, ctx, db, "enterprises", column), column)
	}
	for _, column := range []string{
		"current_email",
		"password_hash",
		"must_change_password",
		"initial_password_expires_at",
		"password_changed_at",
		"auth_version",
		"terminated_at",
	} {
		require.False(t, columnExists(t, ctx, db, "enterprise_employees", column), column)
	}

	for _, table := range []string{
		"enterprises",
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
		require.True(t, relationExists(t, ctx, db, table), table)
	}
	require.False(t, columnExists(t, ctx, db, "enterprise_subscriptions", "effective_window_anchor"))
	require.True(t, columnExists(t, ctx, db, "enterprise_subscriptions", "observed_weekly_window_start"))
	for _, constraint := range []string{
		"ck_enterprises_admin_is_dedicated_user",
		"ck_enterprise_employees_lifecycle",
	} {
		require.False(t, constraintExists(t, ctx, db, constraint), constraint)
	}
	for _, index := range []string{
		"uq_enterprises_portal_host",
		"uq_enterprises_admin_user",
		"uq_enterprise_employees_current_email",
	} {
		require.False(t, relationExists(t, ctx, db, index), index)
	}
	require.True(t, constraintExists(t, ctx, db, "enterprise_employees_status_check"))
	require.True(t, constraintExists(t, ctx, db, "ck_enterprise_employees_status_disabled_at"))
	require.Zero(t, migrationRecordCount(t, ctx, db, "237_enterprise_identity.sql"))
	require.Zero(t, migrationRecordCount(t, ctx, db, "238_enterprise_brand_object.sql"))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, "235_enterprise_foundation.sql"))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, "236_enterprise_frozen_contract.sql"))
}

func TestEnterprise238UpgradesLegacyBrandMetadataWithoutRewriting237(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "237_enterprise_identity.sql")))

	var userID, enterpriseID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash) VALUES ('legacy-brand@example.com', 'test') RETURNING id
	`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprises (name, dedicated_upstream_user_id, admin_user_id, portal_host)
		VALUES ('Legacy Enterprise', $1, $1, 'legacy.example.com') RETURNING id
	`, userID).Scan(&enterpriseID))
	legacyURL := "https://legacy.example.com/background.gif"
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprise_branding (
			enterprise_id, title, background_url, background_content_type, background_sha256, background_size_bytes
		) VALUES ($1, 'Legacy title', $2, 'image/gif', 'legacy', 128)
		RETURNING enterprise_id
	`, enterpriseID, legacyURL).Scan(&enterpriseID))

	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, "238_enterprise_brand_object.sql")))

	var enterpriseName, objectKey, backgroundURL, contentType, digest string
	var size int64
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT enterprise_name, background_object_key, background_url,
		       background_content_type, background_sha256, background_size_bytes
		FROM enterprise_branding WHERE enterprise_id = $1
	`, enterpriseID).Scan(&enterpriseName, &objectKey, &backgroundURL, &contentType, &digest, &size))
	require.Equal(t, "Legacy Enterprise", enterpriseName)
	require.Empty(t, objectKey)
	require.Equal(t, legacyURL, backgroundURL)
	require.Equal(t, "image/gif", contentType)
	require.Equal(t, "legacy", strings.TrimSpace(digest))
	require.Equal(t, int64(128), size)

	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	rollbackSQL, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "deploy", "shan-152-enterprise-identity.rollback.sql"))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(rollbackSQL))
	require.ErrorContains(t, err, "SHAN-152 rollback requires empty table enterprise_branding")
	require.True(t, relationExists(t, ctx, db, "enterprise_branding"))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, "238_enterprise_brand_object.sql"))

	_, err = db.ExecContext(ctx, `
		UPDATE enterprise_branding SET background_object_key = 'enterprise/1/branding/invalid.gif'
		WHERE enterprise_id = $1
	`, enterpriseID)
	require.Error(t, err)
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, "238_enterprise_brand_object.sql"))
}

func TestEnterprise237UsesUserFingerprintAndRejectsRefreshReplay(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "237_enterprise_identity.sql")))
	require.False(t, columnExists(t, ctx, db, "users", "token_version"))

	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password-strong"), bcrypt.MinCost)
	require.NoError(t, err)
	var userID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, status)
		VALUES ('admin@example.com', $1, 'active') RETURNING id
	`, string(oldHash)).Scan(&userID))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO enterprises (name, dedicated_upstream_user_id, admin_user_id, portal_host, status)
		VALUES ('Acme', $1, $1, 'acme.example.com', 'active')
	`, userID)
	require.NoError(t, err)

	svc := enterpriseidentity.NewService(db, &config.Config{JWT: config.JWTConfig{Secret: "0123456789abcdef0123456789abcdef"}}, nil, nil)
	oldPair, err := svc.Login(ctx, "acme.example.com", "admin@example.com", "old-password-strong", "integration", "127.0.0.1")
	require.NoError(t, err)
	_, _, err = svc.Authenticate(ctx, "acme.example.com", oldPair.AccessToken)
	require.NoError(t, err)

	newHash, err := bcrypt.GenerateFromPassword([]byte("new-password-strong"), bcrypt.MinCost)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, string(newHash), userID)
	require.NoError(t, err)
	_, _, err = svc.Authenticate(ctx, "acme.example.com", oldPair.AccessToken)
	require.Error(t, err)
	_, err = svc.Refresh(ctx, "acme.example.com", oldPair.RefreshToken, "integration", "127.0.0.1")
	require.Error(t, err)

	logoutR0, err := svc.Login(ctx, "acme.example.com", "admin@example.com", "new-password-strong", "integration", "127.0.0.1")
	require.NoError(t, err)
	logoutR1, err := svc.Refresh(ctx, "acme.example.com", logoutR0.RefreshToken, "integration", "127.0.0.1")
	require.NoError(t, err)
	require.NoError(t, svc.Logout(ctx, "acme.example.com", logoutR1.RefreshToken))
	_, _, err = svc.Authenticate(ctx, "acme.example.com", logoutR1.AccessToken)
	require.Error(t, err)
	_, err = svc.Refresh(ctx, "acme.example.com", logoutR1.RefreshToken, "integration", "127.0.0.1")
	require.Error(t, err)

	r0, err := svc.Login(ctx, "acme.example.com", "admin@example.com", "new-password-strong", "integration", "127.0.0.1")
	require.NoError(t, err)
	r1, err := svc.Refresh(ctx, "acme.example.com", r0.RefreshToken, "integration", "127.0.0.1")
	require.NoError(t, err)
	_, err = svc.Refresh(ctx, "acme.example.com", r0.RefreshToken, "integration", "127.0.0.1")
	require.Error(t, err)
	_, err = svc.Refresh(ctx, "acme.example.com", r1.RefreshToken, "integration", "127.0.0.1")
	require.Error(t, err)
}

func TestEnterprise237UpdateEmployeeRejectsCrossEnterpriseTargetWithoutMutation(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "237_enterprise_identity.sql")))

	createEnterprise := func(email, host string) int64 {
		var userID, enterpriseID int64
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id
		`, email).Scan(&userID))
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO enterprises (name, dedicated_upstream_user_id, admin_user_id, portal_host)
			VALUES ($1, $2, $2, $3) RETURNING id
		`, email, userID, host).Scan(&enterpriseID))
		return enterpriseID
	}

	requestEnterpriseID := createEnterprise("admin-one@example.com", "one.example.com")
	targetEnterpriseID := createEnterprise("admin-two@example.com", "two.example.com")
	var targetEmployeeID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (
			enterprise_id, email, current_email, password_hash, initial_password_expires_at
		) VALUES ($1, 'target@example.com', 'target@example.com', 'test', NOW() + INTERVAL '24 hours')
		RETURNING id
	`, targetEnterpriseID).Scan(&targetEmployeeID))
	require.NoError(t, func() error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO enterprise_sessions (
				id, enterprise_id, principal_type, principal_id, refresh_family_id,
				auth_version, expires_at
			) VALUES (
				'00000000-0000-0000-0000-000000000001', $1, 'employee', $2,
				'00000000-0000-0000-0000-000000000002', 1, NOW() + INTERVAL '1 hour'
			)
		`, targetEnterpriseID, targetEmployeeID)
		return err
	}())

	type employeeSnapshot struct {
		Status       string
		DepartmentID sql.NullInt64
		DisabledAt   sql.NullTime
		AuthVersion  int64
		UpdatedAt    time.Time
	}
	type sessionSnapshot struct {
		RevokedAt sql.NullTime
		UpdatedAt time.Time
	}
	readEmployee := func() employeeSnapshot {
		var snapshot employeeSnapshot
		require.NoError(t, db.QueryRowContext(ctx, `
			SELECT status, department_id, disabled_at, auth_version, updated_at
			FROM enterprise_employees WHERE enterprise_id = $1 AND id = $2
		`, targetEnterpriseID, targetEmployeeID).Scan(
			&snapshot.Status, &snapshot.DepartmentID, &snapshot.DisabledAt,
			&snapshot.AuthVersion, &snapshot.UpdatedAt,
		))
		return snapshot
	}
	readSession := func() sessionSnapshot {
		var snapshot sessionSnapshot
		require.NoError(t, db.QueryRowContext(ctx, `
			SELECT revoked_at, updated_at FROM enterprise_sessions
			WHERE enterprise_id = $1 AND principal_type = 'employee' AND principal_id = $2
		`, targetEnterpriseID, targetEmployeeID).Scan(&snapshot.RevokedAt, &snapshot.UpdatedAt))
		return snapshot
	}

	employeeBefore := readEmployee()
	sessionBefore := readSession()
	svc := enterpriseidentity.NewService(db, &config.Config{}, nil, nil)
	err := svc.UpdateEmployee(ctx, requestEnterpriseID, targetEmployeeID, "disabled", nil)
	statusCode, body := infraerrors.ToHTTP(err)

	require.Equal(t, http.StatusNotFound, statusCode)
	require.Equal(t, "ENTERPRISE_OBJECT_NOT_FOUND", body.Reason)
	require.Equal(t, employeeBefore, readEmployee())
	require.Equal(t, sessionBefore, readSession())

	require.NoError(t, svc.UpdateEmployee(ctx, targetEnterpriseID, targetEmployeeID, "disabled", nil))
	employeeAfter := readEmployee()
	sessionAfter := readSession()
	require.Equal(t, "disabled", employeeAfter.Status)
	require.True(t, employeeAfter.DisabledAt.Valid)
	require.Equal(t, employeeBefore.AuthVersion+1, employeeAfter.AuthVersion)
	require.True(t, sessionAfter.RevokedAt.Valid)
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
