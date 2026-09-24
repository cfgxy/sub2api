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
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	anchor := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	var userID, groupID, upstreamSubscriptionID, enterpriseID, employeeID, subscriptionID, allocationID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
		"shan154-migration-"+suffix+"@example.com").Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO groups (name) VALUES ($1) RETURNING id",
		"shan154-migration-"+suffix).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO user_subscriptions (user_id, group_id, starts_at, expires_at, status, weekly_window_start)
		VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active', $3::timestamptz) RETURNING id
	`, userID, groupID, anchor).Scan(&upstreamSubscriptionID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprises (name, dedicated_upstream_user_id, admin_user_id, portal_host)
		VALUES ('Migration Test', $1, $1, $2) RETURNING id
	`, userID, "shan154-migration-"+suffix+".example.com").Scan(&enterpriseID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, status)
		VALUES ($1, $2, $2, '$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu', 'active') RETURNING id
	`, enterpriseID, "employee-"+suffix+"@example.com").Scan(&employeeID))
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
	return readEnterpriseRollback(t, "237_enterprise_subscription_allocations.sql")
}

const (
	enterpriseCredentialRevocationMigration = "240_enterprise_key_credential_revocation.sql"
	enterpriseAttributionSnapshotMigration  = "241_batch_image_enterprise_attribution_snapshot.sql"
)

func TestEnterprise240RejectsUnrecoverable239OnlyTombstones(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "239_enterprise_employee_key_lifecycle.sql")))
	apiKeyID := seedEnterprise239TombstoneFixture(t, ctx, db)
	_, err := db.ExecContext(ctx, `
		UPDATE enterprise_key_assignments
		SET status = 'revoked', ended_at = NOW(), revoked_at = NOW(), actor_ref = 'test:239-only'
		WHERE api_key_id = $1 AND status = 'active'
	`, apiKeyID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		UPDATE api_keys
		SET key = 'revoked-' || id::text, status = 'disabled'
		WHERE id = $1
	`, apiKeyID)
	require.NoError(t, err)

	err = applyMigrationsFS(ctx, db, migrationOnly(t, enterpriseCredentialRevocationMigration))
	require.ErrorContains(t, err, "cannot apply SHAN-153 credential guard after unrecoverable 239-only enterprise key tombstones")
	require.False(t, relationExists(t, ctx, db, "api_key_revoked_credential_reservations"))
	require.Zero(t, migrationRecordCount(t, ctx, db, enterpriseCredentialRevocationMigration))
}

func seedEnterprise239TombstoneFixture(t *testing.T, ctx context.Context, db *sql.DB) int64 {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	anchor := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	var userID, groupID, upstreamSubscriptionID, enterpriseID, employeeID, apiKeyID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
		"shan153-239-tombstone-"+suffix+"@example.com").Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO groups (name) VALUES ($1) RETURNING id",
		"shan153-239-tombstone-"+suffix).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO user_subscriptions (
			user_id, group_id, starts_at, expires_at, status,
			daily_window_start, weekly_window_start, monthly_window_start
		) VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active', $3::timestamptz, $3::timestamptz, $3::timestamptz)
		RETURNING id
	`, userID, groupID, anchor).Scan(&upstreamSubscriptionID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprises (name, dedicated_upstream_user_id, admin_user_id, portal_host)
		VALUES ($1, $2, $2, $3) RETURNING id
	`, "SHAN-153 239 tombstone", userID, "shan153-239-tombstone-"+suffix+".example.com").Scan(&enterpriseID))
	legacyEmployeeEmail := "employee-" + suffix + "@example.com"
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, status)
		VALUES ($1, $2, $2, '$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu', 'active') RETURNING id
	`, enterpriseID, legacyEmployeeEmail).Scan(&employeeID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			observed_weekly_window_start, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, $3, 'test:239-tombstone') RETURNING id
	`, enterpriseID, upstreamSubscriptionID, anchor).Scan(new(int64)))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, group_id, key, name)
		VALUES ($1, $2, $3, '239-tombstone-key') RETURNING id
	`, userID, groupID, "sk-shan153-239-"+suffix).Scan(&apiKeyID))
	_, err := db.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
			upstream_group_id, generation, status, actor_ref, assigned_at
		) VALUES ($1, $2, $3, $4, $5, 1, 'active', 'test:239-tombstone', $6)
	`, enterpriseID, employeeID, apiKeyID, upstreamSubscriptionID, groupID, anchor)
	require.NoError(t, err)
	return apiKeyID
}

func TestEnterprise240PreflightWaitsForLegacyMutation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "239_enterprise_employee_key_lifecycle.sql")))
	apiKeyID := seedEnterprise239TombstoneFixture(t, ctx, db)

	writerTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = writerTx.Rollback() }()
	_, err = writerTx.ExecContext(ctx, `
		UPDATE enterprise_key_assignments
		SET status = 'revoked', ended_at = NOW(), revoked_at = NOW(), actor_ref = 'test:239-race'
		WHERE api_key_id = $1 AND status = 'active'
	`, apiKeyID)
	require.NoError(t, err)
	_, err = writerTx.ExecContext(ctx, `
		UPDATE api_keys
		SET key = 'revoked-' || id::text, status = 'disabled'
		WHERE id = $1
	`, apiKeyID)
	require.NoError(t, err)

	migrationFS := migrationOnly(t, enterpriseCredentialRevocationMigration)
	migrationDone := make(chan error, 1)
	go func() {
		migrationDone <- applyMigrationsFS(ctx, db, migrationFS)
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_locks
				WHERE relation = 'api_keys'::regclass
				  AND mode = 'ShareRowExclusiveLock'
				  AND NOT granted
			)
		`).Scan(&waiting)
		return queryErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, writerTx.Commit())
	err = <-migrationDone
	require.ErrorContains(t, err, "cannot apply SHAN-153 credential guard after unrecoverable 239-only enterprise key tombstones")
	require.False(t, relationExists(t, ctx, db, "api_key_revoked_credential_reservations"))
	require.Zero(t, migrationRecordCount(t, ctx, db, enterpriseCredentialRevocationMigration))
}

func TestEnterprise240Through242MigrationRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "239_enterprise_employee_key_lifecycle.sql")))
	assertEnterprise240Through242MigrationState(t, ctx, db, false)

	for _, migration := range []string{
		enterpriseCredentialRevocationMigration,
		enterpriseAttributionSnapshotMigration,
		enterpriseKeyLifecycleProvisionLimitIndexMigration,
	} {
		require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, migration)), migration)
	}
	assertEnterprise240Through242MigrationState(t, ctx, db, true)

	for _, migration := range []string{
		enterpriseKeyLifecycleProvisionLimitIndexMigration,
		enterpriseAttributionSnapshotMigration,
		enterpriseCredentialRevocationMigration,
	} {
		require.NoError(t, executeEnterpriseRollback(t, ctx, db, migration), migration)
	}
	assertEnterprise240Through242MigrationState(t, ctx, db, false)

	// A completed rollback must be safe to retry without touching the 239 baseline.
	for _, migration := range []string{
		enterpriseKeyLifecycleProvisionLimitIndexMigration,
		enterpriseAttributionSnapshotMigration,
		enterpriseCredentialRevocationMigration,
	} {
		require.NoError(t, executeEnterpriseRollback(t, ctx, db, migration), migration)
	}

	for _, migration := range []string{
		enterpriseCredentialRevocationMigration,
		enterpriseAttributionSnapshotMigration,
		enterpriseKeyLifecycleProvisionLimitIndexMigration,
	} {
		require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, migration)), migration)
	}
	assertEnterprise240Through242MigrationState(t, ctx, db, true)
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, "239_enterprise_employee_key_lifecycle.sql"))
}

func TestEnterprise240RollbackRejectsReservationsWithoutClearingTracking(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "239_enterprise_employee_key_lifecycle.sql")))
	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, enterpriseCredentialRevocationMigration)))

	suffix := time.Now().UnixNano()
	var userID, groupID, apiKeyID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
		fmt.Sprintf("shan153-rollback-%d@example.com", suffix)).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO groups (name) VALUES ($1) RETURNING id",
		fmt.Sprintf("shan153-rollback-%d", suffix)).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, group_id, key, name)
		VALUES ($1, $2, $3, 'rollback-reservation') RETURNING id
	`, userID, groupID, fmt.Sprintf("sk-shan153-rollback-%d", suffix)).Scan(&apiKeyID))
	_, err := db.ExecContext(ctx, `
		INSERT INTO api_key_revoked_credential_reservations (fingerprint, api_key_id)
		VALUES ($1, $2)
	`, strings.Repeat("a", 64), apiKeyID)
	require.NoError(t, err)

	err = executeEnterpriseRollback(t, ctx, db, enterpriseCredentialRevocationMigration)
	require.ErrorContains(t, err, "cannot rollback SHAN-153 credential guard while revoked credential reservations exist")
	require.True(t, relationExists(t, ctx, db, "api_key_revoked_credential_reservations"))
	require.True(t, triggerExists(t, ctx, db, "trg_guard_enterprise_revoked_api_key_credential"))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, enterpriseCredentialRevocationMigration))
}

func TestEnterprise240RollbackIgnoresBatchAttributionSnapshots(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "239_enterprise_employee_key_lifecycle.sql")))
	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, enterpriseCredentialRevocationMigration)))
	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, enterpriseAttributionSnapshotMigration)))

	var userID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
		fmt.Sprintf("shan153-independent-rollback-%d@example.com", time.Now().UnixNano())).Scan(&userID))
	_, err := db.ExecContext(ctx, `
		INSERT INTO batch_image_jobs (
			batch_id, user_id, provider, model, item_count, enterprise_attribution_snapshot
		) VALUES ($1, $2, 'gemini', 'migration-test', 1, '{"enterprise_id":1}'::jsonb)
	`, fmt.Sprintf("shan153-independent-rollback-%d", time.Now().UnixNano()), userID)
	require.NoError(t, err)

	require.NoError(t, executeEnterpriseRollback(t, ctx, db, enterpriseCredentialRevocationMigration))
	require.False(t, relationExists(t, ctx, db, "api_key_revoked_credential_reservations"))
	require.False(t, triggerExists(t, ctx, db, "trg_guard_enterprise_revoked_api_key_credential"))
	require.True(t, columnExists(t, ctx, db, "batch_image_jobs", "enterprise_attribution_snapshot"))
	require.Equal(t, 0, migrationRecordCount(t, ctx, db, enterpriseCredentialRevocationMigration))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, enterpriseAttributionSnapshotMigration))
}

func TestEnterprise241RollbackRejectsFrozenSnapshotWithoutClearingTracking(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "239_enterprise_employee_key_lifecycle.sql")))
	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, enterpriseCredentialRevocationMigration)))
	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, enterpriseAttributionSnapshotMigration)))

	var userID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
		fmt.Sprintf("shan153-snapshot-%d@example.com", time.Now().UnixNano())).Scan(&userID))
	_, err := db.ExecContext(ctx, `
		INSERT INTO batch_image_jobs (
			batch_id, user_id, provider, model, item_count, enterprise_attribution_snapshot
		) VALUES ($1, $2, 'gemini', 'migration-test', 1, '{"enterprise_id":1}'::jsonb)
	`, fmt.Sprintf("shan153-snapshot-%d", time.Now().UnixNano()), userID)
	require.NoError(t, err)

	err = executeEnterpriseRollback(t, ctx, db, enterpriseAttributionSnapshotMigration)
	require.ErrorContains(t, err, "cannot rollback SHAN-153 batch image attribution while frozen snapshots exist")
	require.True(t, columnExists(t, ctx, db, "batch_image_jobs", "enterprise_attribution_snapshot"))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, enterpriseAttributionSnapshotMigration))
}

func TestEnterprise241RollbackSerializesWithInFlightSnapshotInsert(t *testing.T) {
	ctx := context.Background()
	db := newIndependentMigrationDatabase(t, ctx)
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "239_enterprise_employee_key_lifecycle.sql")))
	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, enterpriseCredentialRevocationMigration)))
	require.NoError(t, applyMigrationsFS(ctx, db, migrationOnly(t, enterpriseAttributionSnapshotMigration)))

	var userID int64
	require.NoError(t, db.QueryRowContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
		fmt.Sprintf("shan153-concurrent-rollback-%d@example.com", time.Now().UnixNano())).Scan(&userID))
	writerTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = writerTx.ExecContext(ctx, `
		INSERT INTO batch_image_jobs (
			batch_id, user_id, provider, model, item_count, enterprise_attribution_snapshot
		) VALUES ($1, $2, 'gemini', 'rollback-race', 1, '{"enterprise_id":1}'::jsonb)
	`, fmt.Sprintf("shan153-concurrent-rollback-%d", time.Now().UnixNano()), userID)
	require.NoError(t, err)

	rollbackSQL := readEnterpriseRollback(t, enterpriseAttributionSnapshotMigration)
	rollbackDone := make(chan error, 1)
	go func() {
		_, rollbackErr := db.ExecContext(ctx, rollbackSQL)
		rollbackDone <- rollbackErr
	}()
	select {
	case err := <-rollbackDone:
		t.Fatalf("rollback completed before the in-flight insert committed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, writerTx.Commit())
	err = <-rollbackDone
	require.ErrorContains(t, err, "cannot rollback SHAN-153 batch image attribution while frozen snapshots exist")
	require.True(t, columnExists(t, ctx, db, "batch_image_jobs", "enterprise_attribution_snapshot"))
	require.Equal(t, 1, migrationRecordCount(t, ctx, db, enterpriseAttributionSnapshotMigration))
}

func assertEnterprise240Through242MigrationState(t *testing.T, ctx context.Context, db *sql.DB, exists bool) {
	t.Helper()
	require.Equal(t, exists, relationExists(t, ctx, db, "api_key_revoked_credential_reservations"))
	require.Equal(t, exists, triggerExists(t, ctx, db, "trg_guard_enterprise_revoked_api_key_credential"))
	require.Equal(t, exists, columnExists(t, ctx, db, "batch_image_jobs", "enterprise_attribution_snapshot"))
	require.Equal(t, exists, relationExists(t, ctx, db, enterpriseKeyLifecycleProvisionLimitIndex))

	expectedRecordCount := 0
	if exists {
		expectedRecordCount = 1
	}
	for _, migration := range []string{
		enterpriseCredentialRevocationMigration,
		enterpriseAttributionSnapshotMigration,
		enterpriseKeyLifecycleProvisionLimitIndexMigration,
	} {
		require.Equal(t, expectedRecordCount, migrationRecordCount(t, ctx, db, migration), migration)
	}
}

func executeEnterpriseRollback(t *testing.T, ctx context.Context, db *sql.DB, migration string) error {
	t.Helper()
	content := readEnterpriseRollback(t, migration)
	if !strings.HasSuffix(migration, nonTransactionalMigrationSuffix) {
		_, err := db.ExecContext(ctx, content)
		return err
	}

	for i, statement := range splitSQLStatements(content) {
		trimmed := strings.TrimSpace(statement)
		if stripSQLLineComment(trimmed) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, trimmed); err != nil {
			return fmt.Errorf("rollback migration %s (non-tx statement %d): %w", migration, i+1, err)
		}
	}
	return nil
}

func readEnterpriseRollback(t *testing.T, migration string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	content, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "migrations", "rollback", migration))
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

	svc := enterpriseidentity.NewService(db, &config.Config{JWT: config.JWTConfig{Secret: "0123456789abcdef0123456789abcdef"}}, nil, nil, nil)
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
	// SHAN-322: migrate through 244 — SHAN-242's optimistic-lock SQL reads
	// enterprise_employees.version, which only exists from migration 244.
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "244_enterprise_employee_optimistic_lock.sql")))

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
	svc := enterpriseidentity.NewService(db, &config.Config{}, nil, nil, nil)
	err := svc.UpdateEmployee(ctx, requestEnterpriseID, targetEmployeeID, "disabled", nil, 1)
	statusCode, body := infraerrors.ToHTTP(err)

	require.Equal(t, http.StatusNotFound, statusCode)
	require.Equal(t, "ENTERPRISE_OBJECT_NOT_FOUND", body.Reason)
	require.Equal(t, employeeBefore, readEmployee())
	require.Equal(t, sessionBefore, readSession())

	require.NoError(t, svc.UpdateEmployee(ctx, targetEnterpriseID, targetEmployeeID, "disabled", nil, 1))
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

func triggerExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_trigger WHERE NOT tgisinternal AND tgname = $1
		)
	`, name).Scan(&exists))
	return exists
}

func migrationRecordCount(t *testing.T, ctx context.Context, db *sql.DB, name string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = $1", name).Scan(&count))
	return count
}
