//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestEnterprise236SchemaMatchesFrozenContract(t *testing.T) {
	ctx := context.Background()

	var departmentTable bool
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT to_regclass('public.enterprise_departments') IS NOT NULL").Scan(&departmentTable))
	require.True(t, departmentTable)

	var invalidMoneyColumns int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = ANY($1)
		  AND data_type = 'numeric'
		  AND (numeric_precision <> 20 OR numeric_scale <> 8)
	`, pq.Array([]string{
		"enterprise_subscription_windows",
		"enterprise_weekly_allocations",
		"enterprise_allocation_revisions",
	})).Scan(&invalidMoneyColumns))
	require.Zero(t, invalidMoneyColumns)

	for _, index := range []string{
		"uq_enterprise_subscriptions_one_active",
		"uq_enterprise_subscriptions_one_scheduled",
		"uq_enterprise_key_assignments_active_api_key",
		"uq_enterprise_key_assignments_active_employee",
		"uq_enterprise_departments_active_name",
		"uq_enterprise_subscription_windows_upstream_window",
	} {
		var exists bool
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT to_regclass('public.' || $1) IS NOT NULL", index).Scan(&exists))
		require.True(t, exists, index)
	}

	for table, columns := range map[string][]string{
		"enterprise_employees":            {"department_id"},
		"enterprise_subscriptions":        {"observed_weekly_window_start"},
		"enterprise_subscription_windows": {"upstream_user_subscription_id", "observed_weekly_window_start"},
		"enterprise_key_assignments":      {"upstream_user_subscription_id", "upstream_group_id", "generation", "ended_at"},
	} {
		for _, column := range columns {
			var exists bool
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
				)
			`, table, column).Scan(&exists))
			require.True(t, exists, table+"."+column)
		}
	}

	var usageLogFK bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_constraint
			WHERE conname = 'enterprise_usage_attributions_usage_log_id_fkey'
			  AND contype = 'f'
		)
	`).Scan(&usageLogFK))
	require.True(t, usageLogFK)

	triggerNames := []string{
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
	var enterpriseTriggers int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pg_trigger
		WHERE NOT tgisinternal
		  AND tgname = ANY($1)
	`, pq.Array(triggerNames)).Scan(&enterpriseTriggers))
	require.Zero(t, enterpriseTriggers)

	functionNames := []string{
		"validate_enterprise_subscription_owner",
		"protect_enterprise_subscription_update",
		"validate_enterprise_key_owner",
		"validate_enterprise_usage_attribution",
		"reject_enterprise_history_mutation",
		"protect_enterprise_subscription_history",
		"protect_enterprise_key_assignment_history",
	}
	var enterpriseFunctions int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pg_proc
		JOIN pg_namespace ON pg_namespace.oid = pg_proc.pronamespace
		WHERE pg_namespace.nspname = 'public'
		  AND pg_proc.proname = ANY($1)
	`, pq.Array(functionNames)).Scan(&enterpriseFunctions))
	require.Zero(t, enterpriseFunctions)
}

func TestEnterpriseReplaceScheduledSubscriptionIsAtomicAndAudited(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	firstUpstreamID, _ := insertEnterpriseUpstreamSubscription(t, ctx, fixture.enterpriseID)
	secondUpstreamID, _ := insertEnterpriseUpstreamSubscription(t, ctx, fixture.enterpriseID)

	first, err := repo.ReplaceScheduledSubscription(ctx, enterprise.ReplaceScheduledSubscriptionParams{
		EnterpriseID:           fixture.enterpriseID,
		UpstreamSubscriptionID: firstUpstreamID,
		ActorRef:               "test:schedule-first",
	})
	require.NoError(t, err)
	require.Nil(t, first.CancelledSubscriptionID)

	second, err := repo.ReplaceScheduledSubscription(ctx, enterprise.ReplaceScheduledSubscriptionParams{
		EnterpriseID:           fixture.enterpriseID,
		UpstreamSubscriptionID: secondUpstreamID,
		ActorRef:               "test:schedule-second",
	})
	require.NoError(t, err)
	require.NotNil(t, second.CancelledSubscriptionID)
	require.Equal(t, first.ScheduledSubscriptionID, *second.CancelledSubscriptionID)

	var activeCount, scheduledCount, cancelledCount, auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'active'),
			COUNT(*) FILTER (WHERE status = 'scheduled'),
			COUNT(*) FILTER (WHERE status = 'cancelled')
		FROM enterprise_subscriptions WHERE enterprise_id = $1
	`, fixture.enterpriseID).Scan(&activeCount, &scheduledCount, &cancelledCount))
	require.Equal(t, 1, activeCount)
	require.Equal(t, 1, scheduledCount)
	require.Equal(t, 1, cancelledCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND event_type = 'subscription.scheduled_replaced'
	`, fixture.enterpriseID).Scan(&auditCount))
	require.Equal(t, 2, auditCount)
	events, err := repo.ListAuditEvents(ctx, fixture.enterpriseID)
	require.NoError(t, err)
	require.Len(t, events, 2)
}

func TestEnterpriseObservedWindowCASIsIdempotentAndNeverRegresses(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	next := fixture.anchor.Add(7 * 24 * time.Hour)
	_, err := integrationDB.ExecContext(ctx,
		"UPDATE user_subscriptions SET weekly_window_start = $2 WHERE id = $1",
		fixture.upstreamSubscriptionID, next)
	require.NoError(t, err)

	result, err := repo.ObserveWeeklyWindow(ctx, enterprise.ObserveWeeklyWindowParams{
		EnterpriseID:           fixture.enterpriseID,
		UpstreamSubscriptionID: fixture.upstreamSubscriptionID,
		ExpectedWindowStart:    &fixture.anchor,
		ObservedWindowStart:    &next,
		ActorRef:               "test:window-advance",
	})
	require.NoError(t, err)
	require.True(t, result.Advanced)
	require.Equal(t, next, *result.CurrentWindowStart)
	var snapshotCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_subscription_windows
		WHERE upstream_user_subscription_id = $1 AND observed_weekly_window_start = $2
	`, fixture.upstreamSubscriptionID, next).Scan(&snapshotCount))
	require.Equal(t, 1, snapshotCount)

	for name, observed := range map[string]*time.Time{
		"duplicate": &next,
		"older":     &fixture.anchor,
		"null":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			retry, retryErr := repo.ObserveWeeklyWindow(ctx, enterprise.ObserveWeeklyWindowParams{
				EnterpriseID:           fixture.enterpriseID,
				UpstreamSubscriptionID: fixture.upstreamSubscriptionID,
				ExpectedWindowStart:    &fixture.anchor,
				ObservedWindowStart:    observed,
				ActorRef:               "test:window-retry",
			})
			require.NoError(t, retryErr)
			require.False(t, retry.Advanced)
			require.Equal(t, next, *retry.CurrentWindowStart)
		})
	}

	future := next.Add(7 * 24 * time.Hour)
	_, err = integrationDB.ExecContext(ctx,
		"UPDATE user_subscriptions SET weekly_window_start = $2 WHERE id = $1",
		fixture.upstreamSubscriptionID, future)
	require.NoError(t, err)
	_, err = repo.ObserveWeeklyWindow(ctx, enterprise.ObserveWeeklyWindowParams{
		EnterpriseID:           fixture.enterpriseID,
		UpstreamSubscriptionID: fixture.upstreamSubscriptionID,
		ExpectedWindowStart:    &fixture.anchor,
		ObservedWindowStart:    &future,
		ActorRef:               "test:stale-cas",
	})
	require.ErrorIs(t, err, enterprise.ErrObservedWindowConflict)
}

func TestEnterpriseKeyAssignmentSegmentsAndGenerationRevocation(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	secondSubscriptionID, secondGroupID := insertEnterpriseUpstreamSubscription(t, ctx, fixture.enterpriseID)
	var invalidatedGroupID int64
	var invalidatedStatuses []string
	invalidator := &enterpriseAuthCacheInvalidatorStub{onInvalidate: func(ctx context.Context) {
		var status string
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT group_id, status FROM api_keys WHERE id = $1", fixture.apiKeyID).Scan(&invalidatedGroupID, &status))
		invalidatedStatuses = append(invalidatedStatuses, status)
	}}
	repo := enterprise.NewRepository(integrationDB, invalidator)

	var apiKey string
	var previousAssignmentID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT key FROM api_keys WHERE id = $1", fixture.apiKeyID).Scan(&apiKey))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT id FROM enterprise_key_assignments
		WHERE api_key_id = $1 AND status = 'active'
	`, fixture.apiKeyID).Scan(&previousAssignmentID))
	assignment, err := repo.RebindKeyAssignment(ctx, enterprise.RebindKeyAssignmentParams{
		EnterpriseID:           fixture.enterpriseID,
		EmployeeID:             fixture.employeeID,
		APIKeyID:               fixture.apiKeyID,
		UpstreamSubscriptionID: secondSubscriptionID,
		ActorRef:               "test:key-rebind",
	})
	require.NoError(t, err)
	require.Equal(t, secondSubscriptionID, assignment.UpstreamSubscriptionID)
	require.Equal(t, secondGroupID, assignment.UpstreamGroupID)
	var currentGroupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT group_id FROM api_keys WHERE id = $1", fixture.apiKeyID).Scan(&currentGroupID))
	require.Equal(t, secondGroupID, currentGroupID)
	require.Equal(t, []string{apiKey}, invalidator.keys)
	require.Equal(t, secondGroupID, invalidatedGroupID)
	require.Equal(t, []string{"active"}, invalidatedStatuses)

	rows, err := integrationDB.QueryContext(ctx, `
		SELECT status, upstream_user_subscription_id, upstream_group_id,
		       assigned_at, ended_at, revoked_at
		FROM enterprise_key_assignments
		WHERE api_key_id = $1 ORDER BY assigned_at, id
	`, fixture.apiKeyID)
	require.NoError(t, err)
	defer rows.Close()
	var statuses []string
	var previousEndedAt, currentAssignedAt time.Time
	for rows.Next() {
		var status string
		var subscriptionID, groupID int64
		var assignedAt time.Time
		var endedAt, revokedAt sql.NullTime
		require.NoError(t, rows.Scan(&status, &subscriptionID, &groupID, &assignedAt, &endedAt, &revokedAt))
		statuses = append(statuses, status)
		if status == "ended" {
			require.True(t, endedAt.Valid)
			require.False(t, revokedAt.Valid)
			previousEndedAt = endedAt.Time
		} else if status == "active" {
			currentAssignedAt = assignedAt
		}
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"ended", "active"}, statuses)
	require.Equal(t, previousEndedAt, currentAssignedAt)
	var auditCount int
	var auditPreviousID, auditNewID, auditSubscriptionID, auditGroupID int64
	var auditBoundary time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       MAX((payload->>'previous_assignment_id')::bigint),
		       MAX((payload->>'new_assignment_id')::bigint),
		       MAX((payload->>'upstream_subscription_id')::bigint),
		       MAX((payload->>'upstream_group_id')::bigint),
		       MAX((payload->>'assignment_segment_boundary')::timestamptz)
		FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND event_type = 'key.assignment_segment_rebound'
	`, fixture.enterpriseID).Scan(
		&auditCount,
		&auditPreviousID,
		&auditNewID,
		&auditSubscriptionID,
		&auditGroupID,
		&auditBoundary,
	))
	require.Equal(t, 1, auditCount)
	require.Equal(t, previousAssignmentID, auditPreviousID)
	require.Equal(t, assignment.ID, auditNewID)
	require.Equal(t, secondSubscriptionID, auditSubscriptionID)
	require.Equal(t, secondGroupID, auditGroupID)
	require.Equal(t, previousEndedAt, auditBoundary)

	require.NoError(t, repo.RevokeKeyGeneration(ctx, enterprise.RevokeKeyGenerationParams{
		EnterpriseID: fixture.enterpriseID,
		APIKeyID:     fixture.apiKeyID,
		ActorRef:     "test:key-revoke",
	}))
	var keyStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT status FROM api_keys WHERE id = $1", fixture.apiKeyID).Scan(&keyStatus))
	require.Equal(t, "disabled", keyStatus)
	require.Equal(t, []string{apiKey, apiKey}, invalidator.keys)
	require.Equal(t, []string{"active", "disabled"}, invalidatedStatuses)

	_, err = repo.RebindKeyAssignment(ctx, enterprise.RebindKeyAssignmentParams{
		EnterpriseID:           fixture.enterpriseID,
		EmployeeID:             fixture.employeeID,
		APIKeyID:               fixture.apiKeyID,
		UpstreamSubscriptionID: secondSubscriptionID,
		ActorRef:               "test:key-reassign",
	})
	require.ErrorIs(t, err, enterprise.ErrKeyGenerationRevoked)
}

func TestEnterpriseControlledExternalAttributionRejectsAssignmentFromAnotherSubscription(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	secondSubscriptionID, _ := insertEnterpriseUpstreamSubscription(t, ctx, fixture.enterpriseID)
	updateEnterpriseSubscriptionSource(t, ctx, fixture, secondSubscriptionID)
	usageID := insertUsageLogForSubscription(t, ctx, fixture, secondSubscriptionID, "0.1000000000")

	_, err := repo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		UsageLogID:         usageID,
		WeeklyWindowAnchor: fixture.anchor,
		Classification:     "controlled_external",
	})
	require.ErrorIs(t, err, enterprise.ErrUsageAttributionMismatch)
}

func TestEnterpriseControlledExternalAttributionRejectsAssignedAPIKey(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	usageID := insertUsageLog(t, ctx, fixture, "0.1000000000")

	_, err := repo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		UsageLogID:         usageID,
		WeeklyWindowAnchor: fixture.anchor,
		Classification:     "controlled_external",
	})
	require.ErrorIs(t, err, enterprise.ErrUsageAttributionMismatch)

	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM enterprise_usage_attributions WHERE usage_log_id = $1", usageID).Scan(&count))
	require.Zero(t, count)
}

func TestEnterpriseRevokeKeyGenerationRecoversDisabledKeyAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	var committedAssignmentStatuses []string
	invalidator := &enterpriseAuthCacheInvalidatorStub{onInvalidate: func(ctx context.Context) {
		var status string
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT status FROM enterprise_key_assignments WHERE api_key_id = $1
		`, fixture.apiKeyID).Scan(&status))
		committedAssignmentStatuses = append(committedAssignmentStatuses, status)
	}}
	repo := enterprise.NewRepository(integrationDB, invalidator)
	var apiKey string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT key FROM api_keys WHERE id = $1", fixture.apiKeyID).Scan(&apiKey))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		UPDATE api_keys SET status = 'disabled' WHERE id = $1 RETURNING id
	`, fixture.apiKeyID).Scan(&fixture.apiKeyID))

	require.NoError(t, repo.RevokeKeyGeneration(ctx, enterprise.RevokeKeyGenerationParams{
		EnterpriseID: fixture.enterpriseID,
		APIKeyID:     fixture.apiKeyID,
		ActorRef:     "test:key-revoke-recovery",
	}))

	var status string
	var endedAt, revokedAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status, ended_at, revoked_at
		FROM enterprise_key_assignments
		WHERE api_key_id = $1
	`, fixture.apiKeyID).Scan(&status, &endedAt, &revokedAt))
	require.Equal(t, "revoked", status)
	require.Equal(t, endedAt, revokedAt)

	require.NoError(t, repo.RevokeKeyGeneration(ctx, enterprise.RevokeKeyGenerationParams{
		EnterpriseID: fixture.enterpriseID,
		APIKeyID:     fixture.apiKeyID,
		ActorRef:     "test:key-revoke-retry",
	}))

	var retryEndedAt, retryRevokedAt time.Time
	var auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT ended_at, revoked_at
		FROM enterprise_key_assignments
		WHERE api_key_id = $1
	`, fixture.apiKeyID).Scan(&retryEndedAt, &retryRevokedAt))
	require.Equal(t, endedAt, retryEndedAt)
	require.Equal(t, revokedAt, retryRevokedAt)
	require.Equal(t, []string{apiKey, apiKey}, invalidator.keys)
	require.Equal(t, []string{"revoked", "revoked"}, committedAssignmentStatuses)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND event_type = 'key.generation_revoked' AND entity_id = $2
	`, fixture.enterpriseID, fixture.apiKeyID).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

type enterpriseAuthCacheInvalidatorStub struct {
	keys         []string
	onInvalidate func(context.Context)
}

func (s *enterpriseAuthCacheInvalidatorStub) InvalidateAuthCacheByKey(ctx context.Context, key string) {
	s.keys = append(s.keys, key)
	if s.onInvalidate != nil {
		s.onInvalidate(ctx)
	}
}

func TestEnterpriseDepartmentActiveNamesAreUniquePerEnterprise(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	var departmentID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_departments (enterprise_id, name)
		VALUES ($1, ' Finance ') RETURNING id
	`, fixture.enterpriseID).Scan(&departmentID))
	expectEnterpriseConstraintName(t, ctx, "uq_enterprise_departments_active_name", `
		INSERT INTO enterprise_departments (enterprise_id, name)
		VALUES ($1, 'finance')
	`, fixture.enterpriseID)
	_, err := integrationDB.ExecContext(ctx,
		"UPDATE enterprise_departments SET status = 'disabled', disabled_at = NOW() WHERE id = $1", departmentID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_departments (enterprise_id, name)
		VALUES ($1, 'finance')
	`, fixture.enterpriseID)
	require.NoError(t, err)
}

func TestUsageCleanupSkipsEnterpriseAttributedUsageWithoutFailingBatch(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	enterpriseRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	attributedUsageID := insertUsageLog(t, ctx, fixture, "0.1000000000")
	unattributedUsageID := insertUsageLog(t, ctx, fixture, "0.2000000000")
	attributedAt := fixture.anchor.Add(10 * time.Minute)
	unattributedAt := fixture.anchor.Add(11 * time.Minute)
	_, err := integrationDB.ExecContext(ctx,
		"UPDATE usage_logs SET created_at = $2 WHERE id = $1", attributedUsageID, attributedAt)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx,
		"UPDATE usage_logs SET created_at = $2 WHERE id = $1", unattributedUsageID, unattributedAt)
	require.NoError(t, err)
	_, err = enterpriseRepo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		EmployeeID:         &fixture.employeeID,
		UsageLogID:         attributedUsageID,
		WeeklyWindowAnchor: fixture.anchor,
		Classification:     "employee",
	})
	require.NoError(t, err)

	cleanupRepo := &usageCleanupRepository{sql: integrationDB}
	deleted, err := cleanupRepo.DeleteUsageLogsBatch(ctx, service.UsageCleanupFilters{
		StartTime: fixture.anchor,
		EndTime:   fixture.anchor.Add(30 * time.Minute),
	}, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	var attributedExists, unattributedExists bool
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM usage_logs WHERE id = $1)", attributedUsageID).Scan(&attributedExists))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM usage_logs WHERE id = $1)", unattributedUsageID).Scan(&unattributedExists))
	require.True(t, attributedExists)
	require.False(t, unattributedExists)
}

func TestDashboardAggregationCleanupSkipsEnterpriseAttributedUsageWithoutFailingBatch(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	enterpriseRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	attributedUsageID := insertUsageLog(t, ctx, fixture, "0.1000000000")
	unattributedUsageID := insertUsageLog(t, ctx, fixture, "0.2000000000")
	attributedAt := fixture.anchor.Add(10 * time.Minute)
	unattributedAt := fixture.anchor.Add(11 * time.Minute)
	cutoff := fixture.anchor.Add(30 * time.Minute)
	_, err := integrationDB.ExecContext(ctx,
		"UPDATE usage_logs SET created_at = $2 WHERE id = $1", attributedUsageID, attributedAt)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx,
		"UPDATE usage_logs SET created_at = $2 WHERE id = $1", unattributedUsageID, unattributedAt)
	require.NoError(t, err)
	_, err = enterpriseRepo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		EmployeeID:         &fixture.employeeID,
		UsageLogID:         attributedUsageID,
		WeeklyWindowAnchor: fixture.anchor,
		Classification:     "employee",
	})
	require.NoError(t, err)

	cleanupRepo := newDashboardAggregationRepositoryWithSQL(integrationDB)
	cleanupRepo.clock = func() time.Time { return fixture.anchor.Add(2 * time.Hour) }
	require.NoError(t, cleanupRepo.CleanupUsageLogs(ctx, cutoff))

	var attributedExists, unattributedExists, attributionExists bool
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM usage_logs WHERE id = $1)", attributedUsageID).Scan(&attributedExists))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM usage_logs WHERE id = $1)", unattributedUsageID).Scan(&unattributedExists))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM enterprise_usage_attributions WHERE usage_log_id = $1)", attributedUsageID).
		Scan(&attributionExists))
	require.True(t, attributedExists)
	require.False(t, unattributedExists)
	require.True(t, attributionExists)

	var rollupStateCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM usage_group_rollup_state WHERE id = 1").Scan(&rollupStateCount))
	require.Equal(t, 1, rollupStateCount)
}

func insertEnterpriseUpstreamSubscription(t *testing.T, ctx context.Context, enterpriseID int64) (int64, int64) {
	t.Helper()
	var userID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT dedicated_upstream_user_id FROM enterprises WHERE id = $1", enterpriseID).Scan(&userID))
	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"INSERT INTO groups (name) VALUES ($1) RETURNING id",
		"enterprise-switch-group-"+time.Now().UTC().Format("20060102150405.000000000")).Scan(&groupID))
	anchor := time.Now().UTC().Truncate(time.Second)
	var subscriptionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO user_subscriptions (
			user_id, group_id, starts_at, expires_at, status, weekly_window_start
		) VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active', $3::timestamptz) RETURNING id
	`, userID, groupID, anchor).Scan(&subscriptionID))
	return subscriptionID, groupID
}
