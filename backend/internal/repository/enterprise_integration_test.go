//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type enterpriseFixture struct {
	enterpriseUserID       int64
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
		INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, status)
		SELECT enterprise_id, ' ' || UPPER(email) || ' ', ' ' || UPPER(current_email) || ' ', password_hash, 'active'
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

	expectEnterpriseConstraintName(t, ctx, "ck_enterprise_employees_lifecycle", `
		INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, status, disabled_at)
		VALUES ($1, 'invalid-employee@example.com', 'invalid-employee@example.com', '$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu', 'disabled', NULL)
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

func TestEnterpriseUsageAttributionSettlementIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

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
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_usage_attributions
		WHERE usage_log_id = $1 AND window_type = 'week'
	`, usageID).Scan(&count))
	require.Equal(t, 1, count)
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

func TestEnterpriseAllocationAuditFailureRollsBackUpdateAndRevision(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	allocation, err := repo.SetAllocation(ctx, enterprise.SetAllocationParams{
		RequesterUserID: fixture.enterpriseUserID,
		EnterpriseID:    fixture.enterpriseID,
		SubscriptionID:  fixture.subscriptionID,
		EmployeeID:      fixture.employeeID,
		WindowType:      enterprise.WindowTypeWeek,
		WindowAnchor:    fixture.anchor,
		Amount:          "3.00000000",
		Reason:          "initial allocation",
	})
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(ctx, `
		ALTER TABLE enterprise_audit_events
		ADD CONSTRAINT shan154_reject_allocation_audit
		CHECK (payload->>'reason' IS DISTINCT FROM 'audit failure rollback')
	`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `
			ALTER TABLE enterprise_audit_events
			DROP CONSTRAINT IF EXISTS shan154_reject_allocation_audit
		`)
	})

	_, err = repo.SetAllocation(ctx, enterprise.SetAllocationParams{
		RequesterUserID: fixture.enterpriseUserID,
		EnterpriseID:    fixture.enterpriseID,
		SubscriptionID:  fixture.subscriptionID,
		EmployeeID:      fixture.employeeID,
		WindowType:      enterprise.WindowTypeWeek,
		WindowAnchor:    fixture.anchor,
		ExpectedVersion: 1,
		Amount:          "9.00000000",
		Reason:          "audit failure rollback",
	})
	require.Error(t, err)

	var amount string
	var version int64
	var revisionCount int
	var auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT amount::text, version
		FROM enterprise_weekly_allocations
		WHERE id = $1
	`, allocation.ID).Scan(&amount, &version))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM enterprise_allocation_revisions WHERE allocation_id = $1", allocation.ID).Scan(&revisionCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM enterprise_audit_events
		WHERE enterprise_id = $1
		  AND entity_type = 'enterprise_allocation'
		  AND entity_id = $2
	`, fixture.enterpriseID, allocation.ID).Scan(&auditCount))
	require.Equal(t, "3.00000000", amount)
	require.Equal(t, int64(1), version)
	require.Equal(t, 1, revisionCount)
	require.Equal(t, 1, auditCount)
}

func TestEnterpriseAllocationAllowsOvercommitAndReusesCreditAcrossWindows(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	first, err := repo.SetAllocation(ctx, enterprise.SetAllocationParams{
		RequesterUserID: fixture.enterpriseUserID,
		EnterpriseID:    fixture.enterpriseID, SubscriptionID: fixture.subscriptionID,
		EmployeeID: fixture.employeeID, WindowType: enterprise.WindowTypeDay,
		WindowAnchor: fixture.anchor, Amount: "6", Reason: "initial split",
	})
	require.NoError(t, err)
	require.Equal(t, "6.00000000", first.Amount)

	second, err := repo.SetAllocation(ctx, enterprise.SetAllocationParams{
		RequesterUserID: fixture.enterpriseUserID,
		EnterpriseID:    fixture.enterpriseID, SubscriptionID: fixture.subscriptionID,
		EmployeeID: fixture.secondEmployee, WindowType: enterprise.WindowTypeDay,
		WindowAnchor: fixture.anchor, Amount: "5", Reason: "intentional overcommit",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), second.Version)

	summary, err := repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: fixture.anchor,
	})
	require.NoError(t, err)
	require.Equal(t, "6.00000000", summary.ConfiguredCredit)
	require.Equal(t, "11.00000000", summary.AllocatedTotal)
	require.Equal(t, "10.00000000", *summary.AuthoritativeLimit)
	require.Equal(t, "1.00000000", summary.OverallocatedBy)
	require.Equal(t, enterprise.AllocationWarningOverallocated, summary.Warning)

	nextAnchor := fixture.anchor.Add(24 * time.Hour)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE user_subscriptions SET daily_window_start = $2 WHERE id = $1
	`, fixture.upstreamSubscriptionID, nextAnchor)
	require.NoError(t, err)
	next, err := repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: nextAnchor,
	})
	require.NoError(t, err)
	require.Equal(t, first.Credit, next.ConfiguredCredit)
	require.Equal(t, "0.00000000", next.UsageCredit)
}

func TestEnterpriseAllocationRejectsCrossScopeAndInvalidInput(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	other := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	base := enterprise.SetAllocationParams{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeWeek, WindowAnchor: fixture.anchor,
		Amount: "1", Reason: "scope test",
	}
	for name, tc := range map[string]struct {
		mutate func(*enterprise.SetAllocationParams)
		target error
	}{
		"cross enterprise user": {func(p *enterprise.SetAllocationParams) { p.RequesterUserID = other.enterpriseUserID }, enterprise.ErrEnterpriseAccessDenied},
		"cross subscription":    {func(p *enterprise.SetAllocationParams) { p.SubscriptionID = other.subscriptionID }, enterprise.ErrEnterpriseAccessDenied},
		"cross employee":        {func(p *enterprise.SetAllocationParams) { p.EmployeeID = other.employeeID }, enterprise.ErrEnterpriseAccessDenied},
		"invalid window":        {func(p *enterprise.SetAllocationParams) { p.WindowType = "1d" }, enterprise.ErrInvalidWindowType},
		"noncanonical anchor":   {func(p *enterprise.SetAllocationParams) { p.WindowAnchor = p.WindowAnchor.Add(time.Second) }, enterprise.ErrInvalidWindowAnchor},
		"negative":              {func(p *enterprise.SetAllocationParams) { p.Amount = "-1" }, enterprise.ErrInvalidAmount},
		"reason":                {func(p *enterprise.SetAllocationParams) { p.Reason = " " }, enterprise.ErrReasonRequired},
	} {
		t.Run(name, func(t *testing.T) {
			params := base
			tc.mutate(&params)
			_, setErr := repo.SetAllocation(ctx, params)
			require.ErrorIs(t, setErr, tc.target)
		})
	}
}

func TestEnterpriseAllocationCrossEnterpriseObjectsHaveNoWriteSideEffects(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	other := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	counts := func() [3]int64 {
		var result [3]int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_weekly_allocations`).Scan(&result[0]))
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_allocation_revisions`).Scan(&result[1]))
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_audit_events`).Scan(&result[2]))
		return result
	}

	for name, mutate := range map[string]func(*enterprise.SetAllocationParams){
		"subscription": func(params *enterprise.SetAllocationParams) { params.SubscriptionID = other.subscriptionID },
		"employee":     func(params *enterprise.SetAllocationParams) { params.EmployeeID = other.employeeID },
	} {
		t.Run(name, func(t *testing.T) {
			params := enterprise.SetAllocationParams{
				RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
				SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
				WindowType: enterprise.WindowTypeWeek, WindowAnchor: fixture.anchor,
				Credit: "1", Reason: "cross enterprise isolation",
			}
			mutate(&params)
			before := counts()
			_, err := repo.SetAllocation(ctx, params)
			require.ErrorIs(t, err, enterprise.ErrEnterpriseAccessDenied)
			require.Equal(t, before, counts(), "cross-enterprise write must not create allocation, revision, or audit rows")
		})
	}

	for name, mutate := range map[string]func(*enterprise.AllocationUsageSummaryQuery){
		"subscription": func(query *enterprise.AllocationUsageSummaryQuery) { query.SubscriptionID = other.subscriptionID },
		"employee":     func(query *enterprise.AllocationUsageSummaryQuery) { query.EmployeeID = other.employeeID },
	} {
		t.Run("summary "+name, func(t *testing.T) {
			query := enterprise.AllocationUsageSummaryQuery{
				RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
				SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
				WindowType: enterprise.WindowTypeWeek, WindowAnchor: fixture.anchor,
			}
			mutate(&query)
			before := counts()
			_, err := repo.GetAllocationUsageSummary(ctx, query)
			require.ErrorIs(t, err, enterprise.ErrEnterpriseAccessDenied)
			require.Equal(t, before, counts(), "cross-enterprise summary must not create allocation, revision, or audit rows")
		})
	}
}

func TestEnterpriseAllocationConcurrentEmployeesMayOverallocate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, employeeID := range []int64{fixture.employeeID, fixture.secondEmployee} {
		employeeID := employeeID
		go func() {
			<-start
			_, setErr := repo.SetAllocation(ctx, enterprise.SetAllocationParams{
				RequesterUserID: fixture.enterpriseUserID,
				EnterpriseID:    fixture.enterpriseID, SubscriptionID: fixture.subscriptionID,
				EmployeeID: employeeID, WindowType: enterprise.WindowTypeDay,
				WindowAnchor: fixture.anchor, Amount: "6", Reason: "concurrent split",
			})
			results <- setErr
		}()
	}
	close(start)

	var succeeded int
	for range 2 {
		setErr := <-results
		require.NoError(t, setErr)
		succeeded++
	}
	require.Equal(t, 2, succeeded)

	var total string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0)::text
		FROM enterprise_weekly_allocations
		WHERE enterprise_id = $1 AND subscription_id = $2
		  AND window_type = 'day' AND window_anchor = $3
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.anchor).Scan(&total))
	require.Equal(t, "12.00000000", total)
}

func TestEnterpriseAllocationWindowUsageSurvivesRotationAndDelayedBilling(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	allocation, err := repo.SetAllocation(ctx, enterprise.SetAllocationParams{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: fixture.anchor,
		Amount: "5", Reason: "usage window",
	})
	require.NoError(t, err)

	var usageID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT nextval('usage_logs_id_seq')`).Scan(&usageID))
	requestAt := fixture.anchor.Add(time.Hour)
	attributions, err := repo.CreateUsageAttributionSnapshot(ctx, enterprise.CreateUsageAttributionSnapshotParams{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: &fixture.employeeID,
		APIKeyID: fixture.apiKeyID, UsageLogID: usageID,
		UpstreamSubscriptionID: fixture.upstreamSubscriptionID,
		RequestAt:              requestAt, DailyWindowAnchor: fixture.anchor,
		WeeklyWindowAnchor: fixture.anchor, MonthlyWindowAnchor: fixture.anchor,
		Classification: "employee",
	})
	require.NoError(t, err)
	require.Len(t, attributions, 3)
	replayed, err := repo.CreateUsageAttributionSnapshot(ctx, enterprise.CreateUsageAttributionSnapshotParams{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: &fixture.employeeID,
		APIKeyID: fixture.apiKeyID, UsageLogID: usageID,
		UpstreamSubscriptionID: fixture.upstreamSubscriptionID,
		RequestAt:              requestAt, DailyWindowAnchor: fixture.anchor,
		WeeklyWindowAnchor: fixture.anchor, MonthlyWindowAnchor: fixture.anchor,
		Classification: "employee",
	})
	require.NoError(t, err)
	require.Equal(t,
		[]int64{attributions[0].ID, attributions[1].ID, attributions[2].ID},
		[]int64{replayed[0].ID, replayed[1].ID, replayed[2].ID},
	)
	_, err = repo.CreateUsageAttributionSnapshot(ctx, enterprise.CreateUsageAttributionSnapshotParams{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: &fixture.employeeID,
		APIKeyID: fixture.apiKeyID, UsageLogID: usageID,
		UpstreamSubscriptionID: fixture.upstreamSubscriptionID,
		RequestAt:              requestAt.Add(time.Second), DailyWindowAnchor: fixture.anchor,
		WeeklyWindowAnchor: fixture.anchor, MonthlyWindowAnchor: fixture.anchor,
		Classification: "employee",
	})
	require.ErrorIs(t, err, enterprise.ErrUsageAttributionMismatch)
	newKeyID := insertAPIKeyForUser(t, ctx, fixture.enterpriseUserID, fixture.groupID, fmt.Sprintf("rotation-%d", time.Now().UnixNano()))
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE enterprise_key_assignments SET status = 'ended', ended_at = $2 WHERE api_key_id = $1 AND status = 'active'
	`, fixture.apiKeyID, fixture.anchor.Add(2*time.Hour))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO usage_logs (
			id, user_id, api_key_id, account_id, subscription_id, model, actual_cost, created_at
		) VALUES ($1, $2, $3, $4, $5, 'enterprise-delayed-test', 2.5, $6)
	`, usageID, fixture.enterpriseUserID, fixture.apiKeyID, fixture.accountID,
		fixture.upstreamSubscriptionID, fixture.anchor.Add(6*time.Hour))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
			upstream_group_id, generation, status, actor_ref, assigned_at
		) VALUES ($1, $2, $3, $4, $5, 1, 'active', 'test:rotation', $6)
	`, fixture.enterpriseID, fixture.employeeID, newKeyID, fixture.upstreamSubscriptionID,
		fixture.groupID, fixture.anchor.Add(2*time.Hour))
	require.NoError(t, err)

	summary, err := repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: fixture.anchor,
	})
	require.NoError(t, err)
	require.Equal(t, allocation.Credit, summary.ConfiguredCredit)
	require.Equal(t, "2.50000000", summary.UsageCredit)

	var storedGeneration int64
	var storedRequestAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT assignment_generation, request_at
		FROM enterprise_usage_attributions
		WHERE usage_log_id = $1 AND window_type = 'day'
	`, usageID).Scan(&storedGeneration, &storedRequestAt))
	require.Equal(t, int64(1), storedGeneration)
	require.Equal(t, requestAt, storedRequestAt)
}

func TestEnterpriseUsageLogPersistsFrozenPricingAtAcrossWindowBoundaries(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	requestAt := fixture.anchor.Add(time.Hour)
	dayAnchor := requestAt.Add(-23 * time.Hour)
	weekAnchor := requestAt.Add(-6 * 24 * time.Hour)
	monthAnchor := requestAt.Add(-29 * 24 * time.Hour)
	usageRepo := NewUsageLogRepository(nil, integrationDB)
	usageLog := &service.UsageLog{
		UserID: fixture.enterpriseUserID, APIKeyID: fixture.apiKeyID, AccountID: fixture.accountID,
		RequestID: fmt.Sprintf("shan154-delayed-%d", time.Now().UnixNano()), Model: "enterprise-test",
		SubscriptionID: &fixture.upstreamSubscriptionID, ActualCost: 1.25, CreatedAt: requestAt.Add(48 * time.Hour),
		EnterpriseAttributionCandidate: true,
		AttributionRequestAt:           requestAt, AttributionDailyWindowAnchor: &dayAnchor,
		AttributionWeeklyWindowAnchor: &weekAnchor, AttributionMonthlyWindowAnchor: &monthAnchor,
	}
	inserted, err := usageRepo.Create(ctx, usageLog)
	require.NoError(t, err)
	require.True(t, inserted)

	rows, err := integrationDB.QueryContext(ctx, `
		SELECT window_type, window_anchor, request_at
		FROM enterprise_usage_attributions WHERE usage_log_id = $1 ORDER BY window_type
	`, usageLog.ID)
	require.NoError(t, err)
	defer rows.Close()
	want := map[string]time.Time{"day": dayAnchor, "week": weekAnchor, "month": monthAnchor}
	seen := 0
	for rows.Next() {
		var windowType string
		var anchor, storedRequestAt time.Time
		require.NoError(t, rows.Scan(&windowType, &anchor, &storedRequestAt))
		require.Equal(t, want[windowType].UTC(), anchor)
		require.Equal(t, requestAt.UTC(), storedRequestAt)
		seen++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 3, seen)

	replayed, err := usageRepo.Create(ctx, usageLog)
	require.NoError(t, err)
	require.False(t, replayed)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM enterprise_usage_attributions WHERE usage_log_id = $1", usageLog.ID).Scan(&count))
	require.Equal(t, 3, count)
}

func TestEnterpriseDedicatedAPIKeyLoadsAttributionCandidate(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := NewAPIKeyRepository(testEntClient(t), integrationDB)

	apiKey, err := repo.GetByID(ctx, fixture.apiKeyID)
	require.NoError(t, err)
	require.Equal(t, fixture.apiKeyID, apiKey.ID)
	require.True(t, apiKey.EnterpriseAttributionCandidate)
}

func TestEnterpriseUsageLogMatchesSubscriptionLifecycleAtRequestTime(t *testing.T) {
	ctx := context.Background()

	t.Run("active", func(t *testing.T) {
		fixture := seedEnterpriseFixture(t, ctx)
		requestAt := fixture.anchor.Add(30 * time.Minute)
		usageLog := enterpriseUsageLogForTest(fixture, "active", requestAt, requestAt.Add(time.Hour), &fixture.anchor, nil, nil)

		inserted, err := NewUsageLogRepository(nil, integrationDB).Create(ctx, usageLog)
		require.NoError(t, err)
		require.True(t, inserted)
		requireEnterpriseAttributionCount(t, ctx, usageLog.ID, 1)
	})

	t.Run("scheduled subscription is excluded", func(t *testing.T) {
		fixture := seedEnterpriseFixture(t, ctx)
		requestAt := fixture.anchor.Add(2 * time.Hour)
		_, err := integrationDB.ExecContext(ctx, `
			UPDATE enterprise_subscriptions
			SET status = 'ended', ended_at = $2
			WHERE id = $1
		`, fixture.subscriptionID, requestAt.Add(-time.Minute))
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `
			INSERT INTO enterprise_subscriptions (
				enterprise_id, upstream_user_subscription_id, status, actor_ref
			) VALUES ($1, $2, 'scheduled', 'test:scheduled')
		`, fixture.enterpriseID, fixture.upstreamSubscriptionID)
		require.NoError(t, err)
		usageLog := enterpriseUsageLogForTest(fixture, "scheduled", requestAt, requestAt.Add(time.Hour), &fixture.anchor, nil, nil)

		inserted, err := NewUsageLogRepository(nil, integrationDB).Create(ctx, usageLog)
		require.NoError(t, err)
		require.True(t, inserted)
		requireEnterpriseAttributionCount(t, ctx, usageLog.ID, 0)
	})

	t.Run("request before ended_at remains attributable after late settlement", func(t *testing.T) {
		fixture := seedEnterpriseFixture(t, ctx)
		requestAt := fixture.anchor.Add(30 * time.Minute)
		endedAt := requestAt.Add(30 * time.Minute)
		_, err := integrationDB.ExecContext(ctx, `
			UPDATE enterprise_subscriptions
			SET status = 'ended', ended_at = $2
			WHERE id = $1
		`, fixture.subscriptionID, endedAt)
		require.NoError(t, err)
		usageLog := enterpriseUsageLogForTest(fixture, "late-ended", requestAt, endedAt.Add(time.Hour), &fixture.anchor, nil, nil)

		inserted, err := NewUsageLogRepository(nil, integrationDB).Create(ctx, usageLog)
		require.NoError(t, err)
		require.True(t, inserted)
		requireEnterpriseAttributionCount(t, ctx, usageLog.ID, 1)
	})
}

func TestEnterpriseUsageLogIdempotencyRejectsDifferentAttributionSnapshots(t *testing.T) {
	ctx := context.Background()

	t.Run("request_at differs", func(t *testing.T) {
		fixture := seedEnterpriseFixture(t, ctx)
		requestAt := fixture.anchor.Add(30 * time.Minute)
		requestID := fmt.Sprintf("shan154-idempotent-request-at-%d", time.Now().UnixNano())
		first := enterpriseUsageLogForTest(fixture, requestID, requestAt, requestAt.Add(time.Hour), &fixture.anchor, nil, nil)
		repo := NewUsageLogRepository(nil, integrationDB)
		inserted, err := repo.Create(ctx, first)
		require.NoError(t, err)
		require.True(t, inserted)

		replay := enterpriseUsageLogForTest(fixture, requestID, requestAt.Add(time.Second), requestAt.Add(2*time.Hour), &fixture.anchor, nil, nil)
		inserted, err = repo.Create(ctx, replay)
		require.ErrorIs(t, err, enterprise.ErrUsageAttributionMismatch)
		require.False(t, inserted)
		requireEnterpriseAttributionCount(t, ctx, first.ID, 1)
	})

	t.Run("window anchor set differs", func(t *testing.T) {
		fixture := seedEnterpriseFixture(t, ctx)
		requestAt := fixture.anchor.Add(30 * time.Minute)
		requestID := fmt.Sprintf("shan154-idempotent-anchor-set-%d", time.Now().UnixNano())
		first := enterpriseUsageLogForTest(fixture, requestID, requestAt, requestAt.Add(time.Hour), &fixture.anchor, nil, nil)
		repo := NewUsageLogRepository(nil, integrationDB)
		inserted, err := repo.Create(ctx, first)
		require.NoError(t, err)
		require.True(t, inserted)

		replay := enterpriseUsageLogForTest(fixture, requestID, requestAt, requestAt.Add(2*time.Hour), &fixture.anchor, &fixture.anchor, nil)
		inserted, err = repo.Create(ctx, replay)
		require.ErrorIs(t, err, enterprise.ErrUsageAttributionMismatch)
		require.False(t, inserted)
		requireEnterpriseAttributionCount(t, ctx, first.ID, 1)
	})

	t.Run("employee generation and classification differ", func(t *testing.T) {
		fixture := seedEnterpriseFixture(t, ctx)
		requestAt := fixture.anchor.Add(30 * time.Minute)
		requestID := fmt.Sprintf("shan154-idempotent-generation-%d", time.Now().UnixNano())
		first := enterpriseUsageLogForTest(fixture, requestID, requestAt, requestAt.Add(time.Hour), &fixture.anchor, nil, nil)
		repo := NewUsageLogRepository(nil, integrationDB)
		inserted, err := repo.Create(ctx, first)
		require.NoError(t, err)
		require.True(t, inserted)

		rotationAt := requestAt.Add(15 * time.Minute)
		_, err = integrationDB.ExecContext(ctx, `
			UPDATE enterprise_key_assignments
			SET status = 'ended', ended_at = $2
			WHERE api_key_id = $1 AND status = 'active'
		`, fixture.apiKeyID, rotationAt)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `
			INSERT INTO enterprise_key_assignments (
				enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
				upstream_group_id, generation, status, actor_ref, assigned_at
			) VALUES ($1, $2, $3, $4, $5, 2, 'active', 'test:rotate', $6)
		`, fixture.enterpriseID, fixture.secondEmployee, fixture.apiKeyID,
			fixture.upstreamSubscriptionID, fixture.groupID, rotationAt)
		require.NoError(t, err)

		replayAt := rotationAt.Add(time.Minute)
		replay := enterpriseUsageLogForTest(fixture, requestID, replayAt, replayAt.Add(time.Hour), &fixture.anchor, nil, nil)
		inserted, err = repo.Create(ctx, replay)
		require.ErrorIs(t, err, enterprise.ErrUsageAttributionMismatch)
		require.False(t, inserted)
		requireEnterpriseAttributionCount(t, ctx, first.ID, 1)
	})
}

func TestEnterpriseUsageLogConcurrentDifferentSnapshotsDoNotMix(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	requestAt := fixture.anchor.Add(30 * time.Minute)
	requestID := fmt.Sprintf("shan154-concurrent-snapshot-%d", time.Now().UnixNano())
	otherAnchor := fixture.anchor.Add(-24 * time.Hour)
	logs := []*service.UsageLog{
		enterpriseUsageLogForTest(fixture, requestID, requestAt, requestAt.Add(time.Hour), &fixture.anchor, nil, nil),
		enterpriseUsageLogForTest(fixture, requestID, requestAt, requestAt.Add(time.Hour), &otherAnchor, nil, nil),
	}

	start := make(chan struct{})
	errs := make([]error, len(logs))
	var wg sync.WaitGroup
	for i := range logs {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, errs[index] = NewUsageLogRepository(nil, integrationDB).Create(ctx, logs[index])
		}(i)
	}
	close(start)
	wg.Wait()

	var success, mismatch int
	for _, err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, enterprise.ErrUsageAttributionMismatch):
			mismatch++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, mismatch)

	var usageLogID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT id FROM usage_logs WHERE request_id = $1 AND api_key_id = $2",
		requestID, fixture.apiKeyID).Scan(&usageLogID))
	requireEnterpriseAttributionCount(t, ctx, usageLogID, 1)
}

func TestEnterpriseUsageLogCreatesPartialAndControlledExternalAttributions(t *testing.T) {
	ctx := context.Background()

	t.Run("部分 anchor 独立落库", func(t *testing.T) {
		fixture := seedEnterpriseFixture(t, ctx)
		requestAt := fixture.anchor.Add(time.Hour)
		monthAnchor := fixture.anchor.AddDate(0, -1, 0)
		usageRepo := NewUsageLogRepository(nil, integrationDB)
		usageLog := &service.UsageLog{
			UserID: fixture.enterpriseUserID, APIKeyID: fixture.apiKeyID, AccountID: fixture.accountID,
			RequestID: fmt.Sprintf("shan154-partial-%d", time.Now().UnixNano()), Model: "enterprise-test",
			SubscriptionID: &fixture.upstreamSubscriptionID, ActualCost: 1.25, CreatedAt: requestAt.Add(time.Hour),
			EnterpriseAttributionCandidate: true,
			AttributionRequestAt:           requestAt, AttributionDailyWindowAnchor: &fixture.anchor,
			AttributionMonthlyWindowAnchor: &monthAnchor,
		}
		inserted, err := usageRepo.Create(ctx, usageLog)
		require.NoError(t, err)
		require.True(t, inserted)

		rows, err := integrationDB.QueryContext(ctx, `
			SELECT window_type, classification, employee_id, assignment_generation
			FROM enterprise_usage_attributions WHERE usage_log_id = $1 ORDER BY window_type
		`, usageLog.ID)
		require.NoError(t, err)
		defer rows.Close()
		seen := make([]string, 0, 2)
		for rows.Next() {
			var windowType, classification string
			var employeeID sql.NullInt64
			var generation int64
			require.NoError(t, rows.Scan(&windowType, &classification, &employeeID, &generation))
			require.Equal(t, "employee", classification)
			require.True(t, employeeID.Valid)
			require.Equal(t, fixture.employeeID, employeeID.Int64)
			require.Equal(t, int64(1), generation)
			seen = append(seen, windowType)
		}
		require.NoError(t, rows.Err())
		require.Equal(t, []string{"day", "month"}, seen)
	})

	t.Run("无有效 assignment 时写 controlled_external", func(t *testing.T) {
		fixture := seedEnterpriseFixture(t, ctx)
		_, err := integrationDB.ExecContext(ctx,
			"DELETE FROM enterprise_key_assignments WHERE api_key_id = $1", fixture.apiKeyID)
		require.NoError(t, err)
		requestAt := fixture.anchor.Add(time.Hour)
		usageRepo := NewUsageLogRepository(nil, integrationDB)
		usageLog := &service.UsageLog{
			UserID: fixture.enterpriseUserID, APIKeyID: fixture.apiKeyID, AccountID: fixture.accountID,
			RequestID: fmt.Sprintf("shan154-external-%d", time.Now().UnixNano()), Model: "enterprise-test",
			SubscriptionID: &fixture.upstreamSubscriptionID, ActualCost: 1.25, CreatedAt: requestAt.Add(time.Hour),
			EnterpriseAttributionCandidate: true,
			AttributionRequestAt:           requestAt, AttributionDailyWindowAnchor: &fixture.anchor,
			AttributionWeeklyWindowAnchor: &fixture.anchor, AttributionMonthlyWindowAnchor: &fixture.anchor,
		}
		inserted, err := usageRepo.Create(ctx, usageLog)
		require.NoError(t, err)
		require.True(t, inserted)

		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM enterprise_usage_attributions
			WHERE usage_log_id = $1 AND classification = 'controlled_external'
			  AND employee_id IS NULL AND assignment_generation = 0
		`, usageLog.ID).Scan(&count))
		require.Equal(t, 3, count)
	})

	t.Run("普通 usage 不产生企业归属", func(t *testing.T) {
		fixture := seedEnterpriseFixture(t, ctx)
		requestAt := fixture.anchor.Add(time.Hour)
		var ordinaryGroupID, ordinarySubscriptionID, ordinaryAPIKeyID int64
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"INSERT INTO groups (name) VALUES ($1) RETURNING id",
			fmt.Sprintf("ordinary-group-%d", time.Now().UnixNano())).Scan(&ordinaryGroupID))
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			INSERT INTO user_subscriptions (
				user_id, group_id, starts_at, expires_at, status,
				daily_window_start, weekly_window_start, monthly_window_start
			) VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active',
			          $3::timestamptz, $3::timestamptz, $3::timestamptz)
			RETURNING id
		`, fixture.enterpriseUserID, ordinaryGroupID, fixture.anchor).Scan(&ordinarySubscriptionID))
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			INSERT INTO api_keys (user_id, group_id, key, name)
			VALUES ($1, $2, $3, 'ordinary-key') RETURNING id
		`, fixture.enterpriseUserID, ordinaryGroupID,
			fmt.Sprintf("sk-ordinary-%d", time.Now().UnixNano())).Scan(&ordinaryAPIKeyID))
		usageRepo := NewUsageLogRepository(nil, integrationDB)
		usageLog := &service.UsageLog{
			UserID: fixture.enterpriseUserID, APIKeyID: ordinaryAPIKeyID, AccountID: fixture.accountID,
			RequestID: fmt.Sprintf("shan154-normal-%d", time.Now().UnixNano()), Model: "normal-test",
			SubscriptionID: &ordinarySubscriptionID, CreatedAt: requestAt.Add(time.Hour), AttributionRequestAt: requestAt,
			AttributionDailyWindowAnchor: &fixture.anchor, AttributionWeeklyWindowAnchor: &fixture.anchor,
			AttributionMonthlyWindowAnchor: &fixture.anchor,
		}
		inserted, err := usageRepo.Create(ctx, usageLog)
		require.NoError(t, err)
		require.True(t, inserted)

		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM enterprise_usage_attributions WHERE usage_log_id = $1", usageLog.ID).Scan(&count))
		require.Zero(t, count)
	})
}

func TestEnterpriseAllocationSummaryValidatesAnchorsAndZeroLimit(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err := repo.SetAllocation(ctx, enterprise.SetAllocationParams{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: fixture.anchor,
		Amount: "5", Reason: "anchor validation",
	})
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET daily_limit_usd = 0 WHERE id = $1", fixture.groupID)
	require.NoError(t, err)
	current, err := repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: fixture.anchor,
	})
	require.NoError(t, err)
	require.Nil(t, current.AuthoritativeLimit)
	require.Equal(t, "0.00000000", current.OverallocatedBy)
	require.Empty(t, current.Warning)

	historicalAnchor := fixture.anchor.Add(-24 * time.Hour)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_weekly_allocations (
			enterprise_id, subscription_id, employee_id, window_type, window_anchor, amount, version
		) VALUES ($1, $2, $3, 'day', $4, 4, 1)
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, historicalAnchor)
	require.NoError(t, err)
	_, err = repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: historicalAnchor,
	})
	require.NoError(t, err)

	futureAnchor := fixture.anchor.Add(24 * time.Hour)
	_, err = repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: futureAnchor,
	})
	require.ErrorIs(t, err, enterprise.ErrInvalidWindowAnchor)
}

func TestEnterpriseLateUsageStaysInPricingWindow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		windowType string
		next       func(time.Time) time.Time
		updateSQL  string
		setAnchor  func(*service.UsageLog, *time.Time)
	}{
		{name: "day", windowType: enterprise.WindowTypeDay, next: func(v time.Time) time.Time { return v.Add(24 * time.Hour) },
			updateSQL: "UPDATE user_subscriptions SET daily_window_start = $2 WHERE id = $1",
			setAnchor: func(log *service.UsageLog, anchor *time.Time) { log.AttributionDailyWindowAnchor = anchor }},
		{name: "week", windowType: enterprise.WindowTypeWeek, next: func(v time.Time) time.Time { return v.Add(7 * 24 * time.Hour) },
			updateSQL: "UPDATE user_subscriptions SET weekly_window_start = $2 WHERE id = $1",
			setAnchor: func(log *service.UsageLog, anchor *time.Time) { log.AttributionWeeklyWindowAnchor = anchor }},
		{name: "month", windowType: enterprise.WindowTypeMonth, next: func(v time.Time) time.Time { return v.AddDate(0, 1, 0) },
			updateSQL: "UPDATE user_subscriptions SET monthly_window_start = $2 WHERE id = $1",
			setAnchor: func(log *service.UsageLog, anchor *time.Time) { log.AttributionMonthlyWindowAnchor = anchor }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := seedEnterpriseFixture(t, ctx)
			repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
			_, err := repo.SetAllocation(ctx, enterprise.SetAllocationParams{
				RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
				SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
				WindowType: tc.windowType, WindowAnchor: fixture.anchor,
				Amount: "5", Reason: "late usage",
			})
			require.NoError(t, err)

			nextAnchor := tc.next(fixture.anchor)
			pricingAt := nextAnchor.Add(-time.Second)
			_, err = integrationDB.ExecContext(ctx, tc.updateSQL, fixture.upstreamSubscriptionID, nextAnchor)
			require.NoError(t, err)
			usageRepo := NewUsageLogRepository(nil, integrationDB)
			usageLog := &service.UsageLog{
				UserID: fixture.enterpriseUserID, APIKeyID: fixture.apiKeyID, AccountID: fixture.accountID,
				RequestID: fmt.Sprintf("shan154-late-%s-%d", tc.name, time.Now().UnixNano()), Model: "enterprise-test",
				SubscriptionID: &fixture.upstreamSubscriptionID, ActualCost: 2.5,
				EnterpriseAttributionCandidate: true,
				CreatedAt:                      nextAnchor.Add(time.Second), AttributionRequestAt: pricingAt,
			}
			tc.setAnchor(usageLog, &fixture.anchor)
			inserted, err := usageRepo.Create(ctx, usageLog)
			require.NoError(t, err)
			require.True(t, inserted)

			var storedAnchor, storedRequestAt time.Time
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT window_anchor, request_at FROM enterprise_usage_attributions
				WHERE usage_log_id = $1 AND window_type = $2
			`, usageLog.ID, tc.windowType).Scan(&storedAnchor, &storedRequestAt))
			require.Equal(t, fixture.anchor, storedAnchor)
			require.Equal(t, pricingAt, storedRequestAt)

			oldSummary, err := repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
				RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
				SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
				WindowType: tc.windowType, WindowAnchor: fixture.anchor,
			})
			require.NoError(t, err)
			require.Equal(t, "2.50000000", oldSummary.UsageCredit)
			newSummary, err := repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
				RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
				SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
				WindowType: tc.windowType, WindowAnchor: nextAnchor,
			})
			require.NoError(t, err)
			require.Equal(t, "0.00000000", newSummary.UsageCredit)
		})
	}
}

func TestGatewayRecordUsagePersistsFrozenEnterpriseWindowThroughRealRepository(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	requestAt := fixture.anchor.Add(30 * time.Minute)
	newAnchor := fixture.anchor.Add(24 * time.Hour)
	enterpriseRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err := enterpriseRepo.SetAllocation(ctx, enterprise.SetAllocationParams{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: fixture.anchor,
		Amount: "100", Reason: "gateway integration old window",
	})
	require.NoError(t, err)
	loadedSubscription, err := NewUserSubscriptionRepository(testEntClient(t)).GetActiveByUserIDAndGroupID(
		ctx, fixture.enterpriseUserID, fixture.groupID,
	)
	require.NoError(t, err)
	loadedAPIKey, err := NewAPIKeyRepository(testEntClient(t), integrationDB).GetByID(ctx, fixture.apiKeyID)
	require.NoError(t, err)
	require.True(t, loadedAPIKey.EnterpriseAttributionCandidate)

	input := &service.RecordUsageInput{
		Result: &service.ForwardResult{
			RequestID: fmt.Sprintf("shan154-gateway-real-repo-%d", time.Now().UnixNano()),
			Model:     "claude-sonnet-4", Duration: time.Second,
			Usage: service.ClaudeUsage{InputTokens: 1000, OutputTokens: 500},
		},
		APIKey:       loadedAPIKey,
		User:         &service.User{ID: fixture.enterpriseUserID},
		Account:      &service.Account{ID: fixture.accountID},
		PricingAt:    requestAt,
		Subscription: loadedSubscription,
	}
	input.APIKey.GroupID = &fixture.groupID
	input.APIKey.Group = &service.Group{
		ID: fixture.groupID, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 1,
	}

	_, err = integrationDB.ExecContext(ctx,
		"UPDATE user_subscriptions SET daily_window_start = $2 WHERE id = $1",
		fixture.upstreamSubscriptionID, newAnchor)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_weekly_allocations (
			enterprise_id, subscription_id, employee_id, window_type, window_anchor, amount, version
		) VALUES ($1, $2, $3, 'day', $4, 100, 1)
	`, fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, newAnchor)
	require.NoError(t, err)

	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	usageRepo := NewUsageLogRepository(nil, integrationDB)
	gateway := service.NewGatewayService(
		nil, nil, usageRepo, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, &service.BillingCacheService{}, nil, nil,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	require.NoError(t, gateway.RecordUsage(ctx, input))

	oldSummary, err := enterpriseRepo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: fixture.anchor,
	})
	require.NoError(t, err)
	require.NotEqual(t, "0.00000000", oldSummary.UsageCredit)
	newSummary, err := enterpriseRepo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: fixture.enterpriseUserID, EnterpriseID: fixture.enterpriseID,
		SubscriptionID: fixture.subscriptionID, EmployeeID: fixture.employeeID,
		WindowType: enterprise.WindowTypeDay, WindowAnchor: newAnchor,
	})
	require.NoError(t, err)
	require.Equal(t, "0.00000000", newSummary.UsageCredit)
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
		_, err = repo.CreateUsageAttribution(ctx, enterprise.CreateUsageAttributionParams{
			EnterpriseID: fixture.enterpriseID, SubscriptionID: fixture.subscriptionID,
			EmployeeID: &fixture.employeeID, UsageLogID: usageLogID,
			WeeklyWindowAnchor: fixture.anchor, Classification: "employee",
		})
		require.NoError(t, err)
	}

	summary, err := repo.GetAllocationUsageSummary(ctx, enterprise.AllocationUsageSummaryQuery{
		EnterpriseID:       fixture.enterpriseID,
		SubscriptionID:     fixture.subscriptionID,
		WeeklyWindowAnchor: fixture.anchor,
		EmployeeID:         fixture.employeeID,
	})
	require.NoError(t, err)
	require.Equal(t, "0.10000000", summary.ConfiguredCredit)
	require.Equal(t, "0.30000000", summary.UsageCredit)
	require.Equal(t, "0.00000000", summary.RemainingCredit)
	require.Equal(t, "0.20000000", summary.OverageCredit)
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
		INSERT INTO groups (name, daily_limit_usd, weekly_limit_usd, monthly_limit_usd)
		VALUES ($1, 10, 20, 30) RETURNING id
	`, "enterprise-group-"+suffix).Scan(&groupID))

	var userSubscriptionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO user_subscriptions (
			user_id, group_id, starts_at, expires_at, status,
			daily_window_start, weekly_window_start, monthly_window_start
		) VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active',
		          $3::timestamptz, $3::timestamptz, $3::timestamptz) RETURNING id
	`, userID, groupID, anchor).Scan(&userSubscriptionID))

	var enterpriseID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprises (name, dedicated_upstream_user_id, admin_user_id, portal_host)
		VALUES ($1, $2, $2, $3) RETURNING id
	`, "Enterprise "+suffix, userID, "enterprise-"+suffix+".example.com").Scan(&enterpriseID))

	var employeeID, secondEmployeeID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, status)
		VALUES ($1, $2, $2, '$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu', 'active') RETURNING id
	`, enterpriseID, "member-"+suffix+"@example.com").Scan(&employeeID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, status)
		VALUES ($1, $2, $2, '$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu', 'active') RETURNING id
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
		enterpriseUserID:       userID,
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

func enterpriseUsageLogForTest(
	fixture enterpriseFixture,
	requestID string,
	requestAt time.Time,
	createdAt time.Time,
	dayAnchor *time.Time,
	weekAnchor *time.Time,
	monthAnchor *time.Time,
) *service.UsageLog {
	if requestID == "active" || requestID == "scheduled" || requestID == "late-ended" {
		requestID = fmt.Sprintf("shan154-lifecycle-%s-%d", requestID, time.Now().UnixNano())
	}
	return &service.UsageLog{
		UserID: fixture.enterpriseUserID, APIKeyID: fixture.apiKeyID, AccountID: fixture.accountID,
		RequestID: requestID, Model: "enterprise-test", SubscriptionID: &fixture.upstreamSubscriptionID,
		ActualCost: 1.25, CreatedAt: createdAt, EnterpriseAttributionCandidate: true, AttributionRequestAt: requestAt,
		AttributionDailyWindowAnchor: dayAnchor, AttributionWeeklyWindowAnchor: weekAnchor,
		AttributionMonthlyWindowAnchor: monthAnchor,
	}
}

func requireEnterpriseAttributionCount(t *testing.T, ctx context.Context, usageLogID int64, want int) {
	t.Helper()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM enterprise_usage_attributions WHERE usage_log_id = $1",
		usageLogID).Scan(&count))
	require.Equal(t, want, count)
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
