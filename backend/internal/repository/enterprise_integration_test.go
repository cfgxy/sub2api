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
	anchor                 time.Time
	effectiveAnchor        time.Time
}

func TestEnterpriseSchemaConstraints(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	assertEnterpriseSchemaShape(t, ctx)

	expectEnterpriseConstraintError(t, ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, status)
		VALUES ($1, ' MEMBER@example.com ', 'active')
	`, fixture.enterpriseID)

	expectEnterpriseConstraintName(t, ctx, "uq_enterprise_subscriptions_one_active", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			effective_window_anchor, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, NOW(), 'test:duplicate-active')
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID, fixture.effectiveAnchor)

	expectEnterpriseConstraintName(t, ctx, "uq_enterprise_key_assignments_api_key_history", `
		INSERT INTO enterprise_key_assignments (enterprise_id, employee_id, api_key_id, status, actor_ref)
		VALUES ($1, $2, $3, 'active', 'test:duplicate-key')
	`, fixture.enterpriseID, fixture.secondEmployee, fixture.apiKeyID)

	secondAPIKeyID := insertAPIKey(t, ctx, fixture.enterpriseID)
	expectEnterpriseConstraintName(t, ctx, "uq_enterprise_key_assignments_active_employee", `
		INSERT INTO enterprise_key_assignments (enterprise_id, employee_id, api_key_id, status, actor_ref)
		VALUES ($1, $2, $3, 'active', 'test:duplicate-employee')
	`, fixture.enterpriseID, fixture.employeeID, secondAPIKeyID)

	other := seedEnterpriseFixture(t, ctx)
	expectEnterpriseConstraintName(t, ctx, "enterprise_key_assignments_enterprise_id_employee_id_fkey", `
		INSERT INTO enterprise_key_assignments (enterprise_id, employee_id, api_key_id, status, actor_ref)
		VALUES ($1, $2, $3, 'active', 'test:cross-enterprise-employee')
	`, fixture.enterpriseID, other.secondEmployee, secondAPIKeyID)

	expectEnterpriseTriggerError(t, ctx, "enterprise subscription must belong to its dedicated upstream user", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status, effective_window_anchor, actor_ref
		) VALUES ($1, $2, 'scheduled', NOW(), 'test:cross-enterprise-subscription')
	`, fixture.enterpriseID, other.upstreamSubscriptionID)

	expectEnterpriseTriggerError(t, ctx, "enterprise key must belong to its dedicated upstream user", `
		INSERT INTO enterprise_key_assignments (enterprise_id, employee_id, api_key_id, status, revoked_at, actor_ref)
		VALUES ($1, $2, $3, 'revoked', NOW(), 'test:cross-enterprise-key')
	`, fixture.enterpriseID, fixture.secondEmployee, other.apiKeyID)

	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_employees_status_disabled_at", `
		INSERT INTO enterprise_employees (enterprise_id, email, status, disabled_at)
		VALUES ($1, 'invalid-employee@example.com', 'disabled', NULL)
	`, fixture.enterpriseID)
	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_subscriptions_status_timestamps", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status, effective_window_anchor, actor_ref
		) VALUES ($1, $2, 'active', $3, 'test:invalid-subscription')
	`, other.enterpriseID, other.upstreamSubscriptionID, other.effectiveAnchor)
	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_subscriptions_status_timestamps", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			effective_window_anchor, activated_at, ended_at, actor_ref
		) VALUES ($1, $2, 'cancelled', $3, NOW(), NOW(), 'test:invalid-cancelled')
	`, other.enterpriseID, other.upstreamSubscriptionID, other.effectiveAnchor)
	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_key_assignments_status_revoked_at", `
		INSERT INTO enterprise_key_assignments (enterprise_id, employee_id, api_key_id, status, revoked_at, actor_ref)
		VALUES ($1, $2, $3, 'revoked', NULL, 'test:invalid-key')
	`, fixture.enterpriseID, fixture.secondEmployee, secondAPIKeyID)

	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_subscriptions_actor_ref_nonempty", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status, effective_window_anchor, actor_ref
		) VALUES ($1, $2, 'scheduled', $3, '   ')
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID, fixture.effectiveAnchor)
	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_key_assignments_actor_ref_nonempty", `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, status, actor_ref
		) VALUES ($1, $2, $3, 'active', '   ')
	`, fixture.enterpriseID, fixture.secondEmployee, secondAPIKeyID)
	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_audit_events_event_type_nonempty", `
		INSERT INTO enterprise_audit_events (
			enterprise_id, event_type, entity_type, actor_ref
		) VALUES ($1, '   ', 'enterprise', 'test:audit')
	`, fixture.enterpriseID)

	var allocationID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_weekly_allocations (
			enterprise_id, subscription_id, weekly_window_anchor, employee_id, amount
		) VALUES ($1, $2, $3, $4, 1) RETURNING id
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.anchor.Add(time.Hour), fixture.employeeID).Scan(&allocationID))
	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_allocation_revisions_reason_nonempty", `
		INSERT INTO enterprise_allocation_revisions (
			enterprise_id, allocation_id, version, previous_amount, new_amount, reason, actor_ref
		) VALUES ($1, $2, 1, NULL, 1, '   ', 'test:revision')
	`, fixture.enterpriseID, allocationID)
}

func TestEnterpriseUsageAttributionOwnershipValidation(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	other := seedEnterpriseFixture(t, ctx)

	expectEnterpriseTriggerError(t, ctx, "enterprise usage attribution references unknown usage log", `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, 999999999, $4, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, fixture.anchor)

	otherUsageID := insertUsageLog(t, ctx, other, "1.0000000000")
	expectEnterpriseTriggerError(t, ctx, "enterprise usage attribution user mismatch", `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, otherUsageID, fixture.anchor)

	usageID := insertUsageLog(t, ctx, fixture, "1.0000000000")
	_, err := integrationDB.ExecContext(ctx,
		"UPDATE usage_logs SET subscription_id = $1 WHERE id = $2", other.upstreamSubscriptionID, usageID)
	require.NoError(t, err)
	expectEnterpriseTriggerError(t, ctx, "enterprise usage attribution subscription mismatch", `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, usageID, fixture.anchor)

	secondAPIKeyID := insertAPIKey(t, ctx, fixture.enterpriseID)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, status, actor_ref, assigned_at
		) VALUES ($1, $2, $3, 'active', 'test:second-key', $4)
	`, fixture.enterpriseID, fixture.secondEmployee, secondAPIKeyID, fixture.anchor)
	require.NoError(t, err)
	usageID = insertUsageLog(t, ctx, fixture, "1.0000000000")
	expectEnterpriseTriggerError(t, ctx, "enterprise usage attribution employee key mismatch", `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.secondEmployee, usageID, fixture.anchor)

	var futureEmployeeID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, status)
		VALUES ($1, 'future-assignment@example.com', 'active') RETURNING id
	`, fixture.enterpriseID).Scan(&futureEmployeeID))
	futureKeyID := insertAPIKey(t, ctx, fixture.enterpriseID)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, status, actor_ref, assigned_at
		) VALUES ($1, $2, $3, 'active', 'test:future-key', NOW() + INTERVAL '1 hour')
	`, fixture.enterpriseID, futureEmployeeID, futureKeyID)
	require.NoError(t, err)
	futureUsageID := insertUsageLogWithKeyAt(t, ctx, fixture, futureKeyID, time.Now().UTC())
	expectEnterpriseTriggerError(t, ctx, "enterprise usage attribution employee key mismatch", `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, futureEmployeeID, futureUsageID, fixture.anchor)

	var boundaryEmployeeID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, status)
		VALUES ($1, 'boundary-assignment@example.com', 'active') RETURNING id
	`, fixture.enterpriseID).Scan(&boundaryEmployeeID))
	boundaryKeyID := insertAPIKey(t, ctx, fixture.enterpriseID)
	boundary := time.Now().UTC().Truncate(time.Microsecond)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, status, actor_ref, assigned_at, revoked_at
		) VALUES ($1, $2, $3, 'revoked', 'test:boundary-key', $4, $4)
	`, fixture.enterpriseID, boundaryEmployeeID, boundaryKeyID, boundary)
	require.NoError(t, err)
	boundaryUsageID := insertUsageLogWithKeyAt(t, ctx, fixture, boundaryKeyID, boundary)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, boundaryEmployeeID, boundaryUsageID, fixture.anchor)
	require.NoError(t, err)
}

func TestEnterpriseActiveSubscriptionRequiresValidUpstreamWeeklyWindow(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		mutateSQL   string
		anchorDelta time.Duration
		message     string
	}{
		{name: "missing weekly anchor", mutateSQL: "UPDATE user_subscriptions SET weekly_window_start = NULL WHERE id = $1", message: "active enterprise subscription requires upstream weekly window anchor"},
		{name: "wrong effective anchor", anchorDelta: time.Minute, message: "enterprise effective window anchor must match next upstream weekly window"},
		{name: "expired", mutateSQL: "UPDATE user_subscriptions SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1", message: "active enterprise subscription upstream subscription is expired"},
		{name: "inactive", mutateSQL: "UPDATE user_subscriptions SET status = 'expired' WHERE id = $1", message: "active enterprise subscription requires active upstream subscription"},
		{name: "not started", mutateSQL: "UPDATE user_subscriptions SET starts_at = NOW() + INTERVAL '1 hour' WHERE id = $1", message: "active enterprise subscription upstream subscription is not currently valid"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := seedEnterpriseFixture(t, ctx)
			_, err := integrationDB.ExecContext(ctx, `
				UPDATE enterprise_subscriptions
				SET status = 'ended', ended_at = NOW(), actor_ref = 'test:end-existing'
				WHERE id = $1
			`, fixture.subscriptionID)
			require.NoError(t, err)
			if test.mutateSQL != "" {
				_, err = integrationDB.ExecContext(ctx, test.mutateSQL, fixture.upstreamSubscriptionID)
				require.NoError(t, err)
			}
			effectiveAnchor := fixture.effectiveAnchor.Add(test.anchorDelta)
			expectEnterpriseTriggerError(t, ctx, test.message, `
				INSERT INTO enterprise_subscriptions (
					enterprise_id, upstream_user_subscription_id, status,
					effective_window_anchor, activated_at, actor_ref
				) VALUES ($1, $2, 'active', $3, $4, 'test:activate')
			`, fixture.enterpriseID, fixture.upstreamSubscriptionID, effectiveAnchor, time.Now().UTC())
		})
	}
}

func TestEnterpriseScheduledSubscriptionActivationRevalidatesUpstream(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE enterprise_subscriptions
		SET status = 'ended', ended_at = NOW(), actor_ref = 'test:end-existing'
		WHERE id = $1
	`, fixture.subscriptionID)
	require.NoError(t, err)

	var scheduledID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			effective_window_anchor, actor_ref
		) VALUES ($1, $2, 'scheduled', $3, 'test:schedule') RETURNING id
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID, fixture.effectiveAnchor).Scan(&scheduledID))
	_, err = integrationDB.ExecContext(ctx,
		"UPDATE user_subscriptions SET weekly_window_start = NULL WHERE id = $1", fixture.upstreamSubscriptionID)
	require.NoError(t, err)

	expectEnterpriseTriggerError(t, ctx, "active enterprise subscription requires upstream weekly window anchor", `
		UPDATE enterprise_subscriptions
		SET status = 'active', activated_at = NOW(), actor_ref = 'test:activate'
		WHERE id = $1
	`, scheduledID)
}

func TestEnterpriseActiveSubscriptionRejectsForgedActivationTimesAndCorrectsLegacyAnchor(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE enterprise_subscriptions
		SET status = 'ended', ended_at = NOW(), actor_ref = 'test:end-existing'
		WHERE id = $1
	`, fixture.subscriptionID)
	require.NoError(t, err)

	expectEnterpriseTriggerError(t, ctx, "enterprise subscription activation cannot precede upstream start", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			effective_window_anchor, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, $4, 'test:historical')
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID, fixture.effectiveAnchor, fixture.anchor.Add(-time.Hour))
	expectEnterpriseTriggerError(t, ctx, "enterprise subscription activation cannot be in the future", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			effective_window_anchor, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, NOW() + INTERVAL '1 hour', 'test:future')
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID, fixture.effectiveAnchor)

	legacyStartsAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	legacyMidnight := legacyStartsAt.Truncate(24 * time.Hour)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE user_subscriptions
		SET starts_at = $2::timestamptz, weekly_window_start = $3,
		    expires_at = $2::timestamptz + INTERVAL '30 days'
		WHERE id = $1
	`, fixture.upstreamSubscriptionID, legacyStartsAt, legacyMidnight)
	require.NoError(t, err)

	var legacySubscriptionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			effective_window_anchor, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, NOW(), 'test:legacy-anchor') RETURNING id
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID, legacyStartsAt.Add(7*24*time.Hour)).Scan(&legacySubscriptionID))
	require.NotZero(t, legacySubscriptionID)
}

func TestEnterpriseSubscriptionHistoryStateMachine(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)

	expectEnterpriseTriggerError(t, ctx, "enterprise subscription immutable identity fields cannot change", `
		UPDATE enterprise_subscriptions
		SET effective_window_anchor = effective_window_anchor + INTERVAL '1 hour'
		WHERE id = $1
	`, fixture.subscriptionID)

	_, err := integrationDB.ExecContext(ctx, `
		UPDATE enterprise_subscriptions
		SET status = 'ended', ended_at = NOW(), actor_ref = 'test:end'
		WHERE id = $1
	`, fixture.subscriptionID)
	require.NoError(t, err)

	expectEnterpriseTriggerError(t, ctx, "terminal enterprise subscription cannot be modified", `
		UPDATE enterprise_subscriptions SET actor_ref = 'test:rewrite' WHERE id = $1
	`, fixture.subscriptionID)
}

func TestEnterpriseSubscriptionRejectsFutureEndedAt(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)

	expectEnterpriseTriggerError(t, ctx, "enterprise subscription ended_at cannot be in the future", `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			effective_window_anchor, ended_at, actor_ref
		) VALUES ($1, $2, 'cancelled', $3, NOW() + INTERVAL '1 hour', 'test:cancel')
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID, fixture.effectiveAnchor)

	var scheduledID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			effective_window_anchor, actor_ref
		) VALUES ($1, $2, 'scheduled', $3, 'test:schedule') RETURNING id
	`, fixture.enterpriseID, fixture.upstreamSubscriptionID, fixture.effectiveAnchor).Scan(&scheduledID))
	expectEnterpriseTriggerError(t, ctx, "enterprise subscription ended_at cannot be in the future", `
		UPDATE enterprise_subscriptions
		SET status = 'cancelled', ended_at = NOW() + INTERVAL '1 hour', actor_ref = 'test:cancel'
		WHERE id = $1
	`, scheduledID)

	expectEnterpriseTriggerError(t, ctx, "enterprise subscription ended_at cannot be in the future", `
		UPDATE enterprise_subscriptions
		SET status = 'ended', ended_at = NOW() + INTERVAL '1 hour', actor_ref = 'test:end'
		WHERE id = $1
	`, fixture.subscriptionID)
}

func TestEnterpriseRevokedKeyCannotBeReassigned(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	replacementKeyID := insertAPIKey(t, ctx, fixture.enterpriseID)
	expectEnterpriseTriggerError(t, ctx, "enterprise key assignment identity fields cannot change", `
		UPDATE enterprise_key_assignments
		SET status = 'revoked', api_key_id = $2, revoked_at = NOW(),
		    actor_ref = 'test:swap-on-revoke', updated_at = clock_timestamp()
		WHERE enterprise_id = $1 AND api_key_id = $3
	`, fixture.enterpriseID, replacementKeyID, fixture.apiKeyID)
	expectEnterpriseTriggerError(t, ctx, "active enterprise key assignments may only transition to revoked", `
		UPDATE enterprise_key_assignments
		SET actor_ref = 'test:metadata-only', updated_at = clock_timestamp()
		WHERE enterprise_id = $1 AND api_key_id = $2
	`, fixture.enterpriseID, fixture.apiKeyID)
	expectEnterpriseTriggerError(t, ctx, "enterprise key revocation requires a new actor ref", `
		UPDATE enterprise_key_assignments
		SET status = 'revoked', revoked_at = NOW(), actor_ref = actor_ref,
		    updated_at = clock_timestamp()
		WHERE enterprise_id = $1 AND api_key_id = $2
	`, fixture.enterpriseID, fixture.apiKeyID)
	expectEnterpriseTriggerError(t, ctx, "enterprise key revocation must advance updated_at", `
		UPDATE enterprise_key_assignments
		SET status = 'revoked', revoked_at = NOW(), actor_ref = 'test:revoke',
		    updated_at = updated_at
		WHERE enterprise_id = $1 AND api_key_id = $2
	`, fixture.enterpriseID, fixture.apiKeyID)

	_, err := integrationDB.ExecContext(ctx, `
		UPDATE enterprise_key_assignments
		SET status = 'revoked', revoked_at = NOW(), actor_ref = 'test:revoke',
		    updated_at = clock_timestamp()
		WHERE enterprise_id = $1 AND api_key_id = $2
	`, fixture.enterpriseID, fixture.apiKeyID)
	require.NoError(t, err)

	expectEnterpriseConstraintName(t, ctx, "uq_enterprise_key_assignments_api_key_history", `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, status, actor_ref
		) VALUES ($1, $2, $3, 'active', 'test:reassign')
	`, fixture.enterpriseID, fixture.secondEmployee, fixture.apiKeyID)

	expectEnterpriseTriggerError(t, ctx, "revoked enterprise key assignments cannot be modified", `
		UPDATE enterprise_key_assignments SET actor_ref = 'test:rewrite' WHERE api_key_id = $1
	`, fixture.apiKeyID)
}

func TestEnterpriseUsageAttributionRequiresMatchingSubscriptionWindow(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	windowEnd := fixture.anchor.Add(7 * 24 * time.Hour)

	crossWeekUsageID := insertUsageLogWithKeyAt(t, ctx, fixture, fixture.apiKeyID, windowEnd.Add(time.Hour))
	expectEnterpriseTriggerError(t, ctx, "enterprise usage attribution does not match subscription window", `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, crossWeekUsageID, fixture.anchor)

	beforeBoundaryUsageID := insertUsageLogWithKeyAt(t, ctx, fixture, fixture.apiKeyID, windowEnd.Add(-time.Microsecond))
	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, beforeBoundaryUsageID, fixture.anchor)
	require.NoError(t, err)

	boundaryUsageID := insertUsageLogWithKeyAt(t, ctx, fixture, fixture.apiKeyID, windowEnd)
	expectEnterpriseTriggerError(t, ctx, "enterprise usage attribution does not match subscription window", `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, boundaryUsageID, fixture.anchor)

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_subscription_windows (
			enterprise_id, subscription_id, source_anchor, window_start, window_end
		) VALUES ($1, $2, $3::timestamptz, $3::timestamptz, $3::timestamptz + INTERVAL '7 days')
	`, fixture.enterpriseID, fixture.subscriptionID, windowEnd)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, boundaryUsageID, windowEnd)
	require.NoError(t, err)
}

func TestEnterpriseAllocationRepositoryConcurrentRevision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB)
	anchor := fixture.anchor

	allocation, err := repo.CreateAllocation(ctx, enterprise.CreateAllocationParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: anchor,
		EmployeeID:         fixture.employeeID,
		Amount:             "10.1234567891",
		Reason:             "initial allocation",
		ActorRef:           "test:create",
	})
	require.NoError(t, err)
	require.Equal(t, "10.1234567891", allocation.Amount)
	require.Equal(t, int64(1), allocation.Version)

	start := make(chan struct{})
	type reviseResult struct {
		amount string
		err    error
	}
	results := make(chan reviseResult, 2)
	for _, amount := range []string{"11.0000000001", "12.0000000002"} {
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
		err = result.err
		switch {
		case err == nil:
			success++
			winningAmount = result.amount
		case errors.Is(err, enterprise.ErrAllocationVersionConflict):
			conflict++
		default:
			require.NoError(t, err)
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

	rows, err := integrationDB.QueryContext(ctx, `
		SELECT previous_amount::text, new_amount::text, actor_ref
		FROM enterprise_allocation_revisions
		WHERE allocation_id = $1
		ORDER BY version
	`, allocation.ID)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var initialPrevious sql.NullString
	var initialNew, initialActor string
	require.NoError(t, rows.Scan(&initialPrevious, &initialNew, &initialActor))
	require.False(t, initialPrevious.Valid)
	require.Equal(t, "10.1234567891", initialNew)
	require.Equal(t, "test:create", initialActor)
	require.True(t, rows.Next())
	var revisedPrevious sql.NullString
	var revisedNew, revisedActor string
	require.NoError(t, rows.Scan(&revisedPrevious, &revisedNew, &revisedActor))
	require.Equal(t, "10.1234567891", revisedPrevious.String)
	require.Equal(t, winningAmount, revisedNew)
	require.Equal(t, "test:concurrent", revisedActor)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

func TestEnterpriseAllocationRevisionFailureRollsBackUpdate(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB)
	allocation, err := repo.CreateAllocation(ctx, enterprise.CreateAllocationParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: time.Now().UTC().Truncate(time.Second),
		EmployeeID:         fixture.employeeID,
		Amount:             "3.0000000000",
		Reason:             "initial allocation",
		ActorRef:           "test:create",
	})
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION shan151_reject_revision() RETURNS trigger AS $$
		BEGIN
			IF NEW.reason = 'force-failure' THEN
				RAISE EXCEPTION 'forced revision failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER shan151_reject_revision
		BEFORE INSERT ON enterprise_allocation_revisions
		FOR EACH ROW EXECUTE FUNCTION shan151_reject_revision();
	`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DROP TRIGGER IF EXISTS shan151_reject_revision ON enterprise_allocation_revisions")
		_, _ = integrationDB.ExecContext(context.Background(), "DROP FUNCTION IF EXISTS shan151_reject_revision()")
	})

	_, err = repo.ReviseAllocation(ctx, enterprise.ReviseAllocationParams{
		AllocationID:    allocation.ID,
		ExpectedVersion: 1,
		Amount:          "9.0000000000",
		Reason:          "force-failure",
		ActorRef:        "test:failure",
	})
	require.ErrorContains(t, err, "forced revision failure")

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
	require.Equal(t, "3.0000000000", amount)
	require.Equal(t, int64(1), version)
	require.Equal(t, 1, revisionCount)

	var previous sql.NullString
	var current, actor string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT previous_amount::text, new_amount::text, actor_ref
		FROM enterprise_allocation_revisions
		WHERE allocation_id = $1
	`, allocation.ID).Scan(&previous, &current, &actor))
	require.False(t, previous.Valid)
	require.Equal(t, "3.0000000000", current)
	require.Equal(t, "test:create", actor)
}

func TestEnterpriseHistoryIsImmutable(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB)
	anchor := fixture.anchor
	allocation, err := repo.CreateAllocation(ctx, enterprise.CreateAllocationParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: anchor,
		EmployeeID:         fixture.employeeID,
		Amount:             "5.0000000000",
		Reason:             "initial allocation",
		ActorRef:           "test:create",
	})
	require.NoError(t, err)

	usageLogID := insertUsageLog(t, ctx, fixture, "1.0000000000")
	var attributionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
			) VALUES ($1, $2, $3, $4, $5, 'employee') RETURNING id
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, usageLogID, anchor).Scan(&attributionID))

	var auditID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_audit_events (enterprise_id, event_type, entity_type, entity_id, payload, actor_ref)
		VALUES ($1, 'allocation.created', 'allocation', $2, '{}'::jsonb, 'test:audit') RETURNING id
	`, fixture.enterpriseID, allocation.ID).Scan(&auditID))

	for name, query := range map[string]string{
		"revision update":       "UPDATE enterprise_allocation_revisions SET reason = 'changed' WHERE allocation_id = $1",
		"revision delete":       "DELETE FROM enterprise_allocation_revisions WHERE allocation_id = $1",
		"attribution update":    "UPDATE enterprise_usage_attributions SET classification = 'changed' WHERE id = $1",
		"attribution delete":    "DELETE FROM enterprise_usage_attributions WHERE id = $1",
		"audit update":          "UPDATE enterprise_audit_events SET event_type = 'changed' WHERE id = $1",
		"audit delete":          "DELETE FROM enterprise_audit_events WHERE id = $1",
		"subscription delete":   "DELETE FROM enterprise_subscriptions WHERE id = $1",
		"key assignment delete": "DELETE FROM enterprise_key_assignments WHERE enterprise_id = $1",
		"employee delete":       "DELETE FROM enterprise_employees WHERE id = $1",
		"allocation delete":     "DELETE FROM enterprise_weekly_allocations WHERE id = $1",
	} {
		name, query := name, query
		t.Run(name, func(t *testing.T) {
			arg := allocation.ID
			switch name {
			case "attribution update", "attribution delete":
				arg = attributionID
			case "audit update", "audit delete":
				arg = auditID
			case "subscription delete":
				arg = fixture.subscriptionID
			case "key assignment delete":
				arg = fixture.enterpriseID
			case "employee delete":
				arg = fixture.secondEmployee
			case "allocation delete":
				arg = allocation.ID
			}
			expectEnterpriseConstraintError(t, ctx, query, arg)
		})
	}

	_, err = integrationDB.ExecContext(ctx, `
		UPDATE enterprise_key_assignments
		SET status = 'revoked', revoked_at = NOW(), actor_ref = 'test:revoke',
		    updated_at = clock_timestamp()
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'
	`, fixture.enterpriseID, fixture.employeeID)
	require.NoError(t, err)
	expectEnterpriseConstraintError(t, ctx, `
		UPDATE enterprise_key_assignments SET status = 'active', revoked_at = NULL
		WHERE enterprise_id = $1 AND employee_id = $2
	`, fixture.enterpriseID, fixture.employeeID)
}

func assertEnterpriseSchemaShape(t *testing.T, ctx context.Context) {
	t.Helper()
	tables := []string{
		"enterprises",
		"enterprise_employees",
		"enterprise_subscriptions",
		"enterprise_subscription_windows",
		"enterprise_key_assignments",
		"enterprise_weekly_allocations",
		"enterprise_allocation_revisions",
		"enterprise_usage_attributions",
		"enterprise_audit_events",
	}
	for _, table := range tables {
		var exists bool
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT to_regclass('public.' || $1) IS NOT NULL", table).Scan(&exists))
		require.True(t, exists, table)
	}

	var invalidTimeColumns int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = ANY($1)
		  AND (
			column_name LIKE '%\_at' ESCAPE '\'
			OR column_name LIKE '%\_anchor' ESCAPE '\'
			OR column_name IN ('window_start', 'window_end')
		  )
		  AND data_type <> 'timestamp with time zone'
	`, pq.Array(tables)).Scan(&invalidTimeColumns))
	require.Zero(t, invalidTimeColumns)

	var invalidMoneyColumns int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = ANY($1)
		  AND data_type = 'numeric'
		  AND (numeric_precision <> 20 OR numeric_scale <> 10)
	`, pq.Array(tables)).Scan(&invalidMoneyColumns))
	require.Zero(t, invalidMoneyColumns)

	var persistedActualCostColumns int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = ANY($1)
		  AND column_name LIKE 'actual_cost%'
	`, pq.Array(tables)).Scan(&persistedActualCostColumns))
	require.Zero(t, persistedActualCostColumns, "enterprise schema must not persist a second actual_cost")

	indexes := []string{
		"uq_enterprise_subscriptions_one_active",
		"idx_enterprise_subscriptions_effective_anchor",
		"uq_enterprise_key_assignments_api_key_history",
		"uq_enterprise_key_assignments_active_employee",
		"idx_enterprise_key_assignments_enterprise_employee",
		"idx_enterprise_weekly_allocations_window",
		"idx_enterprise_usage_attributions_window",
		"idx_enterprise_audit_events_timeline",
	}
	for _, index := range indexes {
		var exists bool
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT to_regclass('public.' || $1) IS NOT NULL", index).Scan(&exists))
		require.True(t, exists, index)
	}

	columns := map[string][]string{
		"enterprise_subscriptions":        {"actor_ref"},
		"enterprise_key_assignments":      {"actor_ref"},
		"enterprise_allocation_revisions": {"previous_amount", "new_amount", "actor_ref"},
		"enterprise_audit_events":         {"actor_ref"},
	}
	for table, names := range columns {
		for _, name := range names {
			var exists bool
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
				)
			`, table, name).Scan(&exists))
			require.True(t, exists, table+"."+name)
		}
	}

	var activeSubscriptionIndex, keyHistoryIndex, activeEmployeeIndex string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'uq_enterprise_subscriptions_one_active'
	`).Scan(&activeSubscriptionIndex))
	require.Contains(t, activeSubscriptionIndex, "CREATE UNIQUE INDEX")
	require.Contains(t, activeSubscriptionIndex, "USING btree (enterprise_id)")
	require.Contains(t, activeSubscriptionIndex, "WHERE")
	require.Contains(t, activeSubscriptionIndex, "'active'::text")
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'uq_enterprise_key_assignments_api_key_history'
	`).Scan(&keyHistoryIndex))
	require.Contains(t, keyHistoryIndex, "CREATE UNIQUE INDEX")
	require.Contains(t, keyHistoryIndex, "USING btree (api_key_id)")
	require.NotContains(t, keyHistoryIndex, "WHERE")
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'uq_enterprise_key_assignments_active_employee'
	`).Scan(&activeEmployeeIndex))
	require.Contains(t, activeEmployeeIndex, "CREATE UNIQUE INDEX")
	require.Contains(t, activeEmployeeIndex, "USING btree (employee_id)")
	require.Contains(t, activeEmployeeIndex, "WHERE")
	require.Contains(t, activeEmployeeIndex, "'active'::text")

	checks := []string{
		"ck_enterprise_subscriptions_actor_ref_nonempty",
		"ck_enterprise_key_assignments_actor_ref_nonempty",
		"ck_enterprise_allocation_revisions_reason_nonempty",
		"ck_enterprise_allocation_revisions_actor_ref_nonempty",
		"ck_enterprise_audit_events_event_type_nonempty",
		"ck_enterprise_audit_events_entity_type_nonempty",
		"ck_enterprise_audit_events_actor_ref_nonempty",
		"ck_enterprise_usage_attributions_classification_employee",
	}
	for _, constraint := range checks {
		var exists bool
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)
		`, constraint).Scan(&exists))
		require.True(t, exists, constraint)
	}
}

func TestEnterpriseAllocationUsageSummaryUsesUsageLogsActualCost(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB)
	anchor := fixture.anchor
	_, err := repo.CreateAllocation(ctx, enterprise.CreateAllocationParams{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: anchor,
		EmployeeID:         fixture.employeeID,
		Amount:             "1.0000000000",
		Reason:             "weekly allocation",
		ActorRef:           "test:create",
	})
	require.NoError(t, err)

	usageLogID := insertUsageLog(t, ctx, fixture, "2.0000000001")
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
			) VALUES ($1, $2, $3, $4, $5, 'employee')
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, usageLogID, anchor)
	require.NoError(t, err)

	externalUsageLogID := insertUsageLog(t, ctx, fixture, "99.0000000000")
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, usage_log_id,
			weekly_window_anchor, classification
		) VALUES ($1, $2, NULL, $3, $4, 'controlled_external')
	`, fixture.enterpriseID, fixture.subscriptionID, externalUsageLogID, anchor)
	require.NoError(t, err)

	summary, err := repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: anchor,
		EmployeeID:         fixture.employeeID,
	})
	require.NoError(t, err)
	require.Equal(t, "1.0000000000", summary.Allocation)
	require.Equal(t, "2.0000000001", summary.ActualCost)
	require.Equal(t, "0.0000000000", summary.Remaining)
	require.Equal(t, "1.0000000001", summary.Overage)
}

func TestEnterpriseMigrationRunnerRepeatAndTransactionalRollback(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, integrationDB))
	require.NoError(t, ApplyMigrations(ctx, integrationDB))

	failingFS := fstest.MapFS{
		"235_failure_probe.sql": {Data: []byte(`
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

	var migrationCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM schema_migrations WHERE filename = '235_failure_probe.sql'
	`).Scan(&migrationCount))
	require.Zero(t, migrationCount)
}

func seedEnterpriseFixture(t *testing.T, ctx context.Context) enterpriseFixture {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	anchor := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	effectiveAnchor := anchor.Add(7 * 24 * time.Hour)

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
		VALUES ($1, 'member@example.com', 'active') RETURNING id
	`, enterpriseID).Scan(&employeeID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, status)
		VALUES ($1, 'second@example.com', 'active') RETURNING id
	`, enterpriseID).Scan(&secondEmployeeID))

	var subscriptionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			effective_window_anchor, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, $4, 'test:seed') RETURNING id
	`, enterpriseID, userSubscriptionID, effectiveAnchor, anchor).Scan(&subscriptionID))

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_subscription_windows (
			enterprise_id, subscription_id, source_anchor, window_start, window_end
		) VALUES ($1, $2, $3::timestamptz, $3::timestamptz, $3::timestamptz + INTERVAL '7 days')
	`, enterpriseID, subscriptionID, anchor)
	require.NoError(t, err)

	apiKeyID := insertAPIKeyForUser(t, ctx, userID, suffix)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (enterprise_id, employee_id, api_key_id, status, actor_ref)
		VALUES ($1, $2, $3, 'active', 'test:seed')
	`, enterpriseID, employeeID, apiKeyID)
	require.NoError(t, err)

	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO accounts (name, platform, type) VALUES ($1, 'anthropic', 'apikey') RETURNING id
	`, "enterprise-account-"+suffix).Scan(&accountID))

	return enterpriseFixture{
		enterpriseID:           enterpriseID,
		employeeID:             employeeID,
		secondEmployee:         secondEmployeeID,
		upstreamSubscriptionID: userSubscriptionID,
		subscriptionID:         subscriptionID,
		apiKeyID:               apiKeyID,
		accountID:              accountID,
		anchor:                 anchor,
		effectiveAnchor:        effectiveAnchor,
	}
}

func insertAPIKey(t *testing.T, ctx context.Context, enterpriseID int64) int64 {
	t.Helper()
	var userID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT dedicated_upstream_user_id FROM enterprises WHERE id = $1", enterpriseID).Scan(&userID))
	return insertAPIKeyForUser(t, ctx, userID, fmt.Sprintf("%d", time.Now().UnixNano()))
}

func insertAPIKeyForUser(t *testing.T, ctx context.Context, userID int64, suffix string) int64 {
	t.Helper()
	var apiKeyID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, key, name) VALUES ($1, $2, $3) RETURNING id
	`, userID, "sk-enterprise-"+suffix, "enterprise-key-"+suffix).Scan(&apiKeyID))
	return apiKeyID
}

func insertUsageLog(t *testing.T, ctx context.Context, fixture enterpriseFixture, actualCost string) int64 {
	t.Helper()
	var userID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT dedicated_upstream_user_id FROM enterprises WHERE id = $1", fixture.enterpriseID).Scan(&userID))
	var usageLogID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO usage_logs (user_id, api_key_id, account_id, subscription_id, model, actual_cost)
		VALUES ($1, $2, $3, $4, 'enterprise-test', $5::numeric) RETURNING id
	`, userID, fixture.apiKeyID, fixture.accountID, fixture.upstreamSubscriptionID, actualCost).Scan(&usageLogID))
	return usageLogID
}

func insertUsageLogWithKeyAt(
	t *testing.T,
	ctx context.Context,
	fixture enterpriseFixture,
	apiKeyID int64,
	createdAt time.Time,
) int64 {
	t.Helper()
	var userID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT dedicated_upstream_user_id FROM enterprises WHERE id = $1", fixture.enterpriseID).Scan(&userID))
	var usageLogID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO usage_logs (
			user_id, api_key_id, account_id, subscription_id, model, actual_cost, created_at
		) VALUES ($1, $2, $3, $4, 'enterprise-test', 1, $5) RETURNING id
	`, userID, apiKeyID, fixture.accountID, fixture.upstreamSubscriptionID, createdAt).Scan(&usageLogID))
	return usageLogID
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

func expectEnterpriseTriggerError(t *testing.T, ctx context.Context, message, query string, args ...any) {
	t.Helper()
	tx, err := integrationDB.BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, query, args...)
	var pqErr *pq.Error
	require.ErrorAs(t, err, &pqErr)
	require.Equal(t, message, pqErr.Message)
	require.NoError(t, tx.Rollback())
}
