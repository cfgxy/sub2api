//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
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
			FROM pg_constraint AS c
			WHERE c.conname = 'enterprise_usage_attributions_usage_log_id_fkey'
			  AND c.contype = 'f'
			  AND c.confrelid = 'usage_logs'::regclass
		)
	`).Scan(&usageLogFK))
	require.True(t, usageLogFK)
	var usageLogsKind string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT relkind::text FROM pg_class WHERE oid = 'usage_logs'::regclass
	`).Scan(&usageLogsKind))
	require.Equal(t, "r", usageLogsKind)

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

func TestEnterpriseFirstAssignmentAndControlledExternalAttributionSerializeByAPIKey(t *testing.T) {
	t.Run("assignment commits first", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		fixture, usageID := seedFirstAssignmentRaceFixture(t, ctx)
		assignmentSubscriptionID, _ := insertEnterpriseUpstreamSubscription(t, ctx, fixture.enterpriseID)
		repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

		blocker, err := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = blocker.Rollback() })
		_, err = blocker.ExecContext(ctx, "SELECT id FROM api_keys WHERE id = $1 FOR UPDATE", fixture.apiKeyID)
		require.NoError(t, err)

		type assignmentResult struct {
			assignment *enterprise.KeyAssignment
			err        error
		}
		assignmentDone := make(chan assignmentResult, 1)
		go func() {
			assignment, assignmentErr := repo.RebindKeyAssignment(ctx, enterprise.RebindKeyAssignmentParams{
				EnterpriseID:           fixture.enterpriseID,
				EmployeeID:             fixture.employeeID,
				APIKeyID:               fixture.apiKeyID,
				UpstreamSubscriptionID: assignmentSubscriptionID,
				ActorRef:               "test:assignment-first",
			})
			assignmentDone <- assignmentResult{assignment: assignment, err: assignmentErr}
		}()
		select {
		case result := <-assignmentDone:
			require.FailNow(t, "assignment bypassed api key lock", "result=%+v", result)
		case <-time.After(150 * time.Millisecond):
		}

		type attributionResult struct {
			attribution *enterprise.UsageAttribution
			err         error
		}
		attributionDone := make(chan attributionResult, 1)
		go func() {
			attribution, attributionErr := repo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
				EnterpriseID:       fixture.enterpriseID,
				SubscriptionID:     fixture.subscriptionID,
				UsageLogID:         usageID,
				WeeklyWindowAnchor: fixture.anchor,
				Classification:     "controlled_external",
			})
			attributionDone <- attributionResult{attribution: attribution, err: attributionErr}
		}()
		select {
		case result := <-attributionDone:
			require.FailNow(t, "attribution bypassed api key lock", "result=%+v", result)
		case <-time.After(150 * time.Millisecond):
		}

		require.NoError(t, blocker.Commit())
		assignmentResultValue := <-assignmentDone
		require.NoError(t, assignmentResultValue.err)
		require.NotNil(t, assignmentResultValue.assignment)
		attributionResultValue := <-attributionDone
		require.ErrorIs(t, attributionResultValue.err, enterprise.ErrUsageAttributionMismatch)
		require.Nil(t, attributionResultValue.attribution)
	})

	t.Run("attribution commits first", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		fixture, usageID := seedFirstAssignmentRaceFixture(t, ctx)
		assignmentSubscriptionID, _ := insertEnterpriseUpstreamSubscription(t, ctx, fixture.enterpriseID)
		repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

		blocker, err := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = blocker.Rollback() })
		_, err = blocker.ExecContext(ctx, "SELECT id FROM api_keys WHERE id = $1 FOR UPDATE", fixture.apiKeyID)
		require.NoError(t, err)

		type attributionResult struct {
			attribution *enterprise.UsageAttribution
			err         error
		}
		attributionDone := make(chan attributionResult, 1)
		go func() {
			attribution, attributionErr := repo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
				EnterpriseID:       fixture.enterpriseID,
				SubscriptionID:     fixture.subscriptionID,
				UsageLogID:         usageID,
				WeeklyWindowAnchor: fixture.anchor,
				Classification:     "controlled_external",
			})
			attributionDone <- attributionResult{attribution: attribution, err: attributionErr}
		}()
		select {
		case result := <-attributionDone:
			require.FailNow(t, "attribution bypassed api key lock", "result=%+v", result)
		case <-time.After(150 * time.Millisecond):
		}

		type assignmentResult struct {
			assignment *enterprise.KeyAssignment
			err        error
		}
		assignmentDone := make(chan assignmentResult, 1)
		go func() {
			assignment, assignmentErr := repo.RebindKeyAssignment(ctx, enterprise.RebindKeyAssignmentParams{
				EnterpriseID:           fixture.enterpriseID,
				EmployeeID:             fixture.employeeID,
				APIKeyID:               fixture.apiKeyID,
				UpstreamSubscriptionID: assignmentSubscriptionID,
				ActorRef:               "test:attribution-first",
			})
			assignmentDone <- assignmentResult{assignment: assignment, err: assignmentErr}
		}()
		select {
		case result := <-assignmentDone:
			require.FailNow(t, "assignment bypassed api key lock", "result=%+v", result)
		case <-time.After(150 * time.Millisecond):
		}

		require.NoError(t, blocker.Commit())
		attributionResultValue := <-attributionDone
		require.NoError(t, attributionResultValue.err)
		require.NotNil(t, attributionResultValue.attribution)
		require.Equal(t, "controlled_external", attributionResultValue.attribution.Classification)
		assignmentResultValue := <-assignmentDone
		require.ErrorIs(t, assignmentResultValue.err, enterprise.ErrUsageAttributionMismatch)
		require.Nil(t, assignmentResultValue.assignment)

		var activeAssignments int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM enterprise_key_assignments
			WHERE api_key_id = $1 AND status = 'active'
		`, fixture.apiKeyID).Scan(&activeAssignments))
		require.Zero(t, activeAssignments)
	})
}

func seedFirstAssignmentRaceFixture(t *testing.T, ctx context.Context) (enterpriseFixture, int64) {
	t.Helper()
	fixture := seedEnterpriseFixture(t, ctx)
	_, err := integrationDB.ExecContext(ctx,
		"DELETE FROM enterprise_key_assignments WHERE api_key_id = $1", fixture.apiKeyID)
	require.NoError(t, err)
	usageID := insertUsageLog(t, ctx, fixture, "0.1000000000")
	usageAt := time.Now().UTC().Add(2 * time.Minute)
	_, err = integrationDB.ExecContext(ctx,
		"UPDATE usage_logs SET created_at = $2 WHERE id = $1", usageID, usageAt)
	require.NoError(t, err)
	return fixture, usageID
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

func TestDashboardAggregationPartitionCleanupSerializesAfterUsageRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const schema = "shan151_partition_cleanup"
	setupPartitionCleanupSchema(t, ctx, schema)

	attributionDB := openPartitionCleanupDB(t, ctx, schema, "shan151-attribution")
	cleanupDB := openPartitionCleanupDB(t, ctx, schema, "shan151-cleanup")
	blockerDB := openPartitionCleanupDB(t, ctx, schema, "shan151-blocker")

	blocker, err := blockerDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	_, err = blocker.ExecContext(ctx, "LOCK TABLE enterprise_subscriptions IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)

	attributionDone := make(chan error, 1)
	go func() {
		repo := enterprise.NewRepository(attributionDB, enterpriseNoopAuthCacheInvalidator{})
		_, createErr := repo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
			EnterpriseID:       300,
			SubscriptionID:     400,
			UsageLogID:         1,
			WeeklyWindowAnchor: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			Classification:     "controlled_external",
		})
		attributionDone <- createErr
	}()
	requireDatabaseLock(t, ctx, schema, "enterprise_subscriptions", "shan151-attribution", "AccessShareLock", false)
	var attributionExists bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM shan151_partition_cleanup.enterprise_usage_attributions
			WHERE usage_log_id = 1
		)
	`).Scan(&attributionExists))
	require.False(t, attributionExists)

	cleanupDone := make(chan error, 1)
	go func() {
		repo := newDashboardAggregationRepositoryWithSQL(cleanupDB)
		cleanupDone <- repo.dropUsageLogsPartitions(ctx, time.Date(2000, 2, 1, 0, 0, 0, 0, time.UTC))
	}()
	requireDatabaseLock(t, ctx, schema, "usage_logs_200001", "shan151-cleanup", "ExclusiveLock", false)
	require.NoError(t, blocker.Commit())

	select {
	case attributionErr := <-attributionDone:
		require.NoError(t, attributionErr)
	case <-ctx.Done():
		t.Fatal("归因事务未完成")
	}
	select {
	case cleanupErr := <-cleanupDone:
		require.NoError(t, cleanupErr)
	case <-ctx.Done():
		t.Fatal("分区清理事务未完成")
	}

	assertPartitionCleanupState(t, ctx, cleanupDB, true, false, "2000-01-11")
}

func TestDashboardAggregationPartitionCleanupFailureRollsBackDataAndWatermark(t *testing.T) {
	ctx := context.Background()
	const schema = "shan151_partition_cleanup_rollback"
	setupPartitionCleanupSchema(t, ctx, schema)
	db := openPartitionCleanupDB(t, ctx, schema, "shan151-cleanup-rollback")

	_, err := db.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, usage_log_id, weekly_window_anchor, classification
		) VALUES (300, 400, 1, '2000-01-01', 'controlled_external');
		CREATE FUNCTION reject_rollup_invalidation() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'injected rollup invalidation failure';
		END;
		$$;
		CREATE TRIGGER reject_rollup_invalidation
			AFTER UPDATE ON usage_group_rollup_state
			FOR EACH ROW EXECUTE FUNCTION reject_rollup_invalidation();
	`)
	require.NoError(t, err)

	repo := newDashboardAggregationRepositoryWithSQL(db)
	err = repo.dropUsageLogsPartitions(ctx, time.Date(2000, 2, 1, 0, 0, 0, 0, time.UTC))
	require.ErrorContains(t, err, "injected rollup invalidation failure")
	assertPartitionCleanupState(t, ctx, db, true, true, "2000-02-01")
}

func setupPartitionCleanupSchema(t *testing.T, ctx context.Context, schema string) {
	t.Helper()
	quotedSchema := pq.QuoteIdentifier(schema)
	_, err := integrationDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
	})
	_, err = integrationDB.ExecContext(ctx, `
		CREATE SCHEMA `+quotedSchema+`;
		CREATE TABLE `+quotedSchema+`.api_keys (
			id BIGINT PRIMARY KEY,
			deleted_at TIMESTAMPTZ
		);
		CREATE TABLE `+quotedSchema+`.usage_logs (
			id BIGINT NOT NULL,
			user_id BIGINT NOT NULL,
			api_key_id BIGINT NOT NULL,
			subscription_id BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL
		) PARTITION BY RANGE (created_at);
		CREATE TABLE `+quotedSchema+`.usage_logs_200001 PARTITION OF `+quotedSchema+`.usage_logs
			FOR VALUES FROM ('2000-01-01') TO ('2000-02-01');
		ALTER TABLE `+quotedSchema+`.usage_logs_200001
			ADD CONSTRAINT usage_logs_200001_id_key UNIQUE (id);
		CREATE TABLE `+quotedSchema+`.enterprises (
			id BIGINT PRIMARY KEY,
			dedicated_upstream_user_id BIGINT NOT NULL
		);
		CREATE TABLE `+quotedSchema+`.enterprise_subscriptions (
			id BIGINT PRIMARY KEY,
			enterprise_id BIGINT NOT NULL,
			upstream_user_subscription_id BIGINT NOT NULL
		);
		CREATE TABLE `+quotedSchema+`.enterprise_subscription_windows (
			enterprise_id BIGINT NOT NULL,
			subscription_id BIGINT NOT NULL,
			upstream_user_subscription_id BIGINT NOT NULL,
			observed_weekly_window_start TIMESTAMPTZ NOT NULL,
			window_start TIMESTAMPTZ NOT NULL,
			window_end TIMESTAMPTZ NOT NULL
		);
		CREATE TABLE `+quotedSchema+`.enterprise_key_assignments (
			enterprise_id BIGINT NOT NULL,
			employee_id BIGINT,
			api_key_id BIGINT NOT NULL,
			upstream_user_subscription_id BIGINT NOT NULL,
			assigned_at TIMESTAMPTZ NOT NULL,
			ended_at TIMESTAMPTZ
		);
		CREATE TABLE `+quotedSchema+`.enterprise_usage_attributions (
			id BIGSERIAL PRIMARY KEY,
			enterprise_id BIGINT NOT NULL,
			subscription_id BIGINT NOT NULL,
			employee_id BIGINT,
			usage_log_id BIGINT NOT NULL UNIQUE,
			weekly_window_anchor TIMESTAMPTZ NOT NULL,
			classification TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE `+quotedSchema+`.usage_group_rollup_state (
			id SMALLINT PRIMARY KEY,
			closed_before DATE NOT NULL,
			retained_from TIMESTAMPTZ NOT NULL,
			timezone_name TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		INSERT INTO `+quotedSchema+`.api_keys (id) VALUES (10);
		INSERT INTO `+quotedSchema+`.enterprises (id, dedicated_upstream_user_id) VALUES (300, 100);
		INSERT INTO `+quotedSchema+`.enterprise_subscriptions (
			id, enterprise_id, upstream_user_subscription_id
		) VALUES (400, 300, 200);
		INSERT INTO `+quotedSchema+`.enterprise_subscription_windows (
			enterprise_id, subscription_id, upstream_user_subscription_id,
			observed_weekly_window_start, window_start, window_end
		) VALUES (300, 400, 200, '2000-01-01', '2000-01-01', '2000-02-01');
		INSERT INTO `+quotedSchema+`.usage_group_rollup_state (
			id, closed_before, retained_from, timezone_name
		) VALUES (1, '2000-02-01', '2000-01-01', 'UTC');
		INSERT INTO `+quotedSchema+`.usage_logs (
			id, user_id, api_key_id, subscription_id, created_at
		) VALUES
			(1, 100, 10, 200, '2000-01-10'),
			(2, 100, 10, 200, '2000-01-11');
	`)
	require.NoError(t, err)
}

func openPartitionCleanupDB(t *testing.T, ctx context.Context, schema, applicationName string) *sql.DB {
	t.Helper()
	dsn, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("search_path", schema)
	query.Set("application_name", applicationName)
	query.Set("options", "-c deadlock_timeout=50ms")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(ctx))
	return db
}

func requireDatabaseLock(
	t *testing.T,
	ctx context.Context,
	schema, table, applicationName, mode string,
	granted bool,
) {
	t.Helper()
	require.Eventually(t, func() bool {
		return hasDatabaseLock(ctx, schema, table, applicationName, mode, granted)
	}, 5*time.Second, 10*time.Millisecond)
}

func hasDatabaseLock(
	ctx context.Context,
	schema, table, applicationName, mode string,
	granted bool,
) bool {
	var found bool
	err := integrationDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_locks AS lock
			JOIN pg_stat_activity AS activity ON activity.pid = lock.pid
			WHERE lock.relation = to_regclass($1)
			  AND activity.application_name = $2
			  AND lock.mode = $3
			  AND lock.granted = $4
		)
	`, fmt.Sprintf("%s.%s", schema, table), applicationName, mode, granted).Scan(&found)
	return err == nil && found
}

func assertPartitionCleanupState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	attributedExists, unattributedExists bool,
	closedBefore string,
) {
	t.Helper()
	var partitionExists, actualAttributedExists, actualUnattributedExists, attributionExists bool
	var actualClosedBefore string
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('usage_logs_200001') IS NOT NULL").Scan(&partitionExists))
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM usage_logs WHERE id = 1)").Scan(&actualAttributedExists))
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM usage_logs WHERE id = 2)").Scan(&actualUnattributedExists))
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM enterprise_usage_attributions WHERE usage_log_id = 1)").Scan(&attributionExists))
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT closed_before::text FROM usage_group_rollup_state WHERE id = 1").Scan(&actualClosedBefore))
	require.True(t, partitionExists)
	require.Equal(t, attributedExists, actualAttributedExists)
	require.Equal(t, unattributedExists, actualUnattributedExists)
	require.True(t, attributionExists)
	require.Equal(t, closedBefore, actualClosedBefore)
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
