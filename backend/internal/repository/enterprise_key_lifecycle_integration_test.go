//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/enterpriseidentity"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type employeeKeyAuthCacheInvalidatorStub struct {
	keys []string
}

type employeeKeyHTTPGenerator struct {
	value string
	calls int
}

func installEmployeeKeyLifecycleGate(t *testing.T, ctx context.Context, enterpriseID, employeeID, gate int64, applicationName string) func() {
	t.Helper()
	suffix := fmt.Sprintf("shan153_key_lifecycle_gate_%d", time.Now().UnixNano())
	functionName := pq.QuoteIdentifier(suffix + "_fn")
	triggerName := pq.QuoteIdentifier(suffix + "_trigger")
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger AS $$
		BEGIN
			IF NEW.enterprise_id = %d AND NEW.employee_id = %d
			  AND current_setting('application_name', true) = %s THEN
				PERFORM pg_advisory_xact_lock(%d);
			END IF;
			RETURN NEW;
		END
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER %s
		BEFORE INSERT ON enterprise_key_lifecycle_idempotency
		FOR EACH ROW EXECUTE FUNCTION %s();`,
		functionName, enterpriseID, employeeID, pq.QuoteLiteral(applicationName), gate, triggerName, functionName))
	require.NoError(t, err)
	return func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON enterprise_key_lifecycle_idempotency", triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	}
}

func installEmployeeKeyBillingGate(t *testing.T, ctx context.Context, apiKeyID, gate int64, applicationName string) func() {
	t.Helper()
	suffix := fmt.Sprintf("shan153_key_billing_gate_%d", time.Now().UnixNano())
	functionName := pq.QuoteIdentifier(suffix + "_fn")
	triggerName := pq.QuoteIdentifier(suffix + "_trigger")
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger AS $$
		BEGIN
			IF NEW.id = %d AND current_setting('application_name', true) = %s THEN
				PERFORM pg_advisory_xact_lock(%d);
			END IF;
			RETURN NEW;
		END
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER %s
		BEFORE UPDATE ON api_keys
		FOR EACH ROW EXECUTE FUNCTION %s();`,
		functionName, apiKeyID, pq.QuoteLiteral(applicationName), gate, triggerName, functionName))
	require.NoError(t, err)
	return func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON api_keys", triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	}
}

func holdEmployeeKeyGate(t *testing.T, ctx context.Context, gate int64) func() {
	t.Helper()
	conn, err := integrationDB.Conn(ctx)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, gate)
	require.NoError(t, err)
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, gate)
		_ = conn.Close()
	}
	t.Cleanup(release)
	return release
}

func requireEmployeeKeyApplicationWaitingOnLock(t *testing.T, ctx context.Context, applicationName string, result <-chan error) {
	t.Helper()
	completed := false
	var completionErr error
	require.Eventually(t, func() bool {
		select {
		case completionErr = <-result:
			completed = true
			return true
		default:
		}
		var waitEventType sql.NullString
		err := integrationDB.QueryRowContext(ctx, `SELECT wait_event_type FROM pg_stat_activity WHERE application_name = $1`, applicationName).Scan(&waitEventType)
		return err == nil && waitEventType.Valid && waitEventType.String == "Lock"
	}, 5*time.Second, 10*time.Millisecond)
	require.False(t, completed, "operation completed before reaching its lock barrier: %v", completionErr)
}

func employeeKeyUsageSnapshot(t *testing.T, ctx context.Context, apiKeyID int64) (float64, float64, float64, float64) {
	t.Helper()
	var quotaUsed, usage5h, usage1d, usage7d float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT quota_used, usage_5h, usage_1d, usage_7d FROM api_keys WHERE id = $1`, apiKeyID).
		Scan(&quotaUsed, &usage5h, &usage1d, &usage7d))
	return quotaUsed, usage5h, usage1d, usage7d
}

func (g *employeeKeyHTTPGenerator) GenerateKey() (string, error) {
	g.calls++
	return g.value, nil
}

func (s *employeeKeyAuthCacheInvalidatorStub) InvalidateAuthCacheByKey(_ context.Context, key string) {
	s.keys = append(s.keys, key)
}

func TestEnterpriseEmployeeKeyRotationPreservesSnapshotAndIdempotency(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	window5h := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	window1d := window5h.Add(-2 * time.Hour)
	window7d := window5h.Add(-24 * time.Hour)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE api_keys SET quota = 25, quota_used = 7.5,
			rate_limit_5h = 5, rate_limit_1d = 10, rate_limit_7d = 20,
			usage_5h = 1.25, usage_1d = 2.5, usage_7d = 6.75,
			window_5h_start = $2, window_1d_start = $3, window_7d_start = $4,
			ip_whitelist = '["198.51.100.0/24"]'::jsonb,
			ip_blacklist = '["203.0.113.9"]'::jsonb
		WHERE id = $1`, fixture.apiKeyID, window5h, window1d, window7d)
	require.NoError(t, err)

	plain := fmt.Sprintf("sk-enterprise-rotate-%d", time.Now().UnixNano())
	params := enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rotate-once",
		Plaintext: plain, ActorRef: "test:employee",
	}
	result, err := repo.RotateEmployeeKey(ctx, params)
	require.NoError(t, err)
	require.Equal(t, plain, result.Plaintext)
	require.NotEqual(t, fixture.apiKeyID, result.Key.ID)
	require.Equal(t, 25.0, result.Key.Quota)
	require.Equal(t, 7.5, result.Key.QuotaUsed)
	require.Equal(t, 1.25, result.Key.Usage5h)
	require.Equal(t, 2.5, result.Key.Usage1d)
	require.Equal(t, 6.75, result.Key.Usage7d)
	require.WithinDuration(t, window5h, *result.Key.Window5h, time.Second)
	require.WithinDuration(t, window1d, *result.Key.Window1d, time.Second)
	require.WithinDuration(t, window7d, *result.Key.Window7d, time.Second)

	replayParams := params
	replayParams.Plaintext = fmt.Sprintf("sk-enterprise-unused-%d", time.Now().UnixNano())
	replayed, err := repo.RotateEmployeeKey(ctx, replayParams)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Empty(t, replayed.Plaintext)
	require.Equal(t, result.Key.ID, replayed.Key.ID)

	var oldStatus string
	var oldQuotaUsed, oldUsage5h, oldUsage1d, oldUsage7d float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status, quota_used, usage_5h, usage_1d, usage_7d FROM api_keys WHERE id = $1`, fixture.apiKeyID).
		Scan(&oldStatus, &oldQuotaUsed, &oldUsage5h, &oldUsage1d, &oldUsage7d))
	require.Equal(t, "disabled", oldStatus)
	require.Equal(t, 7.5, oldQuotaUsed)
	require.Equal(t, 1.25, oldUsage5h)
	require.Equal(t, 2.5, oldUsage1d)
	require.Equal(t, 6.75, oldUsage7d)

	_, err = integrationDB.ExecContext(ctx, `
		UPDATE api_keys SET quota_used = quota_used + 0.5,
			usage_5h = usage_5h + 0.5, usage_1d = usage_1d + 0.5, usage_7d = usage_7d + 0.5
		WHERE id = $1 AND status = 'active'`, result.Key.ID)
	require.NoError(t, err)
	var successorQuotaUsed, successorUsage5h float64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT quota_used, usage_5h FROM api_keys WHERE id = $1`, result.Key.ID).Scan(&successorQuotaUsed, &successorUsage5h))
	require.Equal(t, 8.0, successorQuotaUsed)
	require.Equal(t, 1.75, successorUsage5h)
	var snapshotQuotaUsed, snapshotUsage5h, snapshotUsage1d, snapshotUsage7d float64
	var snapshotWindow5h, snapshotWindow1d, snapshotWindow7d time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT quota_used, usage_5h, usage_1d, usage_7d,
			window_5h_start, window_1d_start, window_7d_start
		FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(
		&snapshotQuotaUsed, &snapshotUsage5h, &snapshotUsage1d, &snapshotUsage7d,
		&snapshotWindow5h, &snapshotWindow1d, &snapshotWindow7d,
	))
	require.Equal(t, 7.5, snapshotQuotaUsed)
	require.Equal(t, 1.25, snapshotUsage5h)
	require.Equal(t, 2.5, snapshotUsage1d)
	require.Equal(t, 6.75, snapshotUsage7d)
	require.WithinDuration(t, window5h, snapshotWindow5h, time.Second)
	require.WithinDuration(t, window1d, snapshotWindow1d, time.Second)
	require.WithinDuration(t, window7d, snapshotWindow7d, time.Second)

	var activeCount, auditLeaks, idempotencyCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.employeeID).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND payload::text LIKE '%' || $2 || '%'`, fixture.enterpriseID, plain).Scan(&auditLeaks))
	require.Zero(t, auditLeaks)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_lifecycle_idempotency
		WHERE enterprise_id = $1 AND employee_id = $2 AND operation = 'rotate'`, fixture.enterpriseID, fixture.employeeID).Scan(&idempotencyCount))
	require.Equal(t, 1, idempotencyCount)
}

func TestEnterpriseEmployeeKeyConcurrentRotationUsesExpectedKeyCAS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		i := i
		go func() {
			<-start
			_, rotateErr := repo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
				EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
				ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
				Plaintext: fmt.Sprintf("sk-enterprise-concurrent-%d-%d", i, time.Now().UnixNano()), ActorRef: "test:concurrent",
			})
			results <- rotateErr
		}()
	}
	close(start)
	var succeeded, conflicts int
	for range 2 {
		err := <-results
		if err == nil {
			succeeded++
		} else if errors.Is(err, enterprise.ErrEmployeeKeyVersionConflict) {
			conflicts++
		} else {
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicts)
	var activeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.employeeID).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
}

func TestEnterpriseEmployeeKeyRotationUsesExactReboundAssignmentSegment(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	secondSubscriptionID, _ := insertEnterpriseUpstreamSubscription(t, ctx, fixture.enterpriseID)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err := repo.RebindKeyAssignment(ctx, enterprise.RebindKeyAssignmentParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID, APIKeyID: fixture.apiKeyID,
		UpstreamSubscriptionID: secondSubscriptionID, ActorRef: "test:rebind-before-rotate",
	})
	require.NoError(t, err)
	var sameGenerationSegments int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE api_key_id = $1`, fixture.apiKeyID).Scan(&sameGenerationSegments))
	require.Equal(t, 2, sameGenerationSegments)

	rotated, err := repo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rotate-after-rebind",
		Plaintext: "sk-rotate-after-rebind", ActorRef: "test:employee",
	})
	require.NoError(t, err)
	require.NotEqual(t, fixture.apiKeyID, rotated.Key.ID)
	var activeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.employeeID).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
}

func TestEnterpriseEmployeeKeyConcurrentRotationUsesIndependentLockedTransactions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	const firstApplication = "shan153-rotation-first"
	const secondApplication = "shan153-rotation-second"
	gate := time.Now().UnixNano()
	dbFirst := openPartitionCleanupDB(t, ctx, "public", firstApplication)
	dbSecond := openPartitionCleanupDB(t, ctx, "public", secondApplication)
	cleanupTrigger := installEmployeeKeyLifecycleGate(t, ctx, fixture.enterpriseID, fixture.employeeID, gate, firstApplication)
	t.Cleanup(cleanupTrigger)
	releaseGate := holdEmployeeKeyGate(t, ctx, gate)
	firstRepo := enterprise.NewRepository(dbFirst, enterpriseNoopAuthCacheInvalidator{})
	secondRepo := enterprise.NewRepository(dbSecond, enterpriseNoopAuthCacheInvalidator{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)

	go func() {
		_, err := firstRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "independent-rotation-first",
			Plaintext: fmt.Sprintf("sk-independent-rotation-first-%d", time.Now().UnixNano()), ActorRef: "test:concurrent",
		})
		firstDone <- err
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, firstApplication, firstDone)

	go func() {
		_, err := secondRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "independent-rotation-second",
			Plaintext: fmt.Sprintf("sk-independent-rotation-second-%d", time.Now().UnixNano()), ActorRef: "test:concurrent",
		})
		secondDone <- err
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, secondApplication, secondDone)

	releaseGate()
	require.NoError(t, <-firstDone)
	require.ErrorIs(t, <-secondDone, enterprise.ErrEmployeeKeyVersionConflict)
	var activeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.employeeID).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
}

func TestEnterpriseEmployeeKeyLateBillingAfterRotationUsesSuccessorUnderLockContention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	window := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE api_keys SET quota = 25, quota_used = 7.5,
			rate_limit_5h = 5, rate_limit_1d = 10, rate_limit_7d = 20,
			usage_5h = 1.25, usage_1d = 2.5, usage_7d = 6.75,
			window_5h_start = $2, window_1d_start = $3, window_7d_start = $4
		WHERE id = $1`, fixture.apiKeyID, window, window.Add(-2*time.Hour), window.Add(-24*time.Hour))
	require.NoError(t, err)

	const rotationApplication = "shan153-rotation-before-billing"
	const billingApplication = "shan153-billing-after-rotation"
	gate := time.Now().UnixNano()
	rotationDB := openPartitionCleanupDB(t, ctx, "public", rotationApplication)
	billingDB := openPartitionCleanupDB(t, ctx, "public", billingApplication)
	cleanupTrigger := installEmployeeKeyLifecycleGate(t, ctx, fixture.enterpriseID, fixture.employeeID, gate, rotationApplication)
	t.Cleanup(cleanupTrigger)
	releaseGate := holdEmployeeKeyGate(t, ctx, gate)
	rotationRepo := enterprise.NewRepository(rotationDB, enterpriseNoopAuthCacheInvalidator{})
	billingRepo := NewUsageBillingRepository(nil, billingDB)
	rotationDone := make(chan error, 1)
	billingDone := make(chan error, 1)
	var successor *enterprise.EmployeeKeyMutationResult
	var billingResult *service.UsageBillingApplyResult

	go func() {
		result, rotateErr := rotationRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rotation-before-late-billing",
			Plaintext: fmt.Sprintf("sk-rotation-before-late-billing-%d", time.Now().UnixNano()), ActorRef: "test:concurrent",
		})
		successor = result
		rotationDone <- rotateErr
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, rotationApplication, rotationDone)

	go func() {
		result, applyErr := billingRepo.Apply(ctx, &service.UsageBillingCommand{
			RequestID: "late-billing-after-rotation-" + fmt.Sprint(time.Now().UnixNano()), APIKeyID: fixture.apiKeyID,
			SubscriptionID: &fixture.upstreamSubscriptionID, SubscriptionCost: 0.5,
			APIKeyQuotaCost: 0.5, APIKeyRateLimitCost: 0.5,
		})
		billingResult = result
		billingDone <- applyErr
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, billingApplication, billingDone)

	releaseGate()
	require.NoError(t, <-rotationDone)
	require.NoError(t, <-billingDone)
	require.NotNil(t, successor)
	require.NotNil(t, successor.Key)
	require.NotNil(t, billingResult)
	require.Equal(t, successor.Key.ID, billingResult.BilledAPIKeyID)
	quotaUsed, usage5h, usage1d, usage7d := employeeKeyUsageSnapshot(t, ctx, successor.Key.ID)
	require.Equal(t, 8.0, quotaUsed)
	require.Equal(t, 1.75, usage5h)
	require.Equal(t, 3.0, usage1d)
	require.Equal(t, 7.25, usage7d)
	var enterpriseCandidate bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT enterprise_attribution_candidate FROM api_keys WHERE id = $1`, successor.Key.ID).Scan(&enterpriseCandidate))
	require.True(t, enterpriseCandidate)
}

func TestEnterpriseEmployeeKeyRotationAfterBillingInheritsFinalSnapshotUnderLockContention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	window := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE api_keys SET quota = 25, quota_used = 7.5,
			rate_limit_5h = 5, rate_limit_1d = 10, rate_limit_7d = 20,
			usage_5h = 1.25, usage_1d = 2.5, usage_7d = 6.75,
			window_5h_start = $2, window_1d_start = $3, window_7d_start = $4
		WHERE id = $1`, fixture.apiKeyID, window, window.Add(-2*time.Hour), window.Add(-24*time.Hour))
	require.NoError(t, err)

	const rotationApplication = "shan153-rotation-after-billing"
	const billingApplication = "shan153-billing-before-rotation"
	gate := time.Now().UnixNano()
	rotationDB := openPartitionCleanupDB(t, ctx, "public", rotationApplication)
	billingDB := openPartitionCleanupDB(t, ctx, "public", billingApplication)
	cleanupTrigger := installEmployeeKeyBillingGate(t, ctx, fixture.apiKeyID, gate, billingApplication)
	t.Cleanup(cleanupTrigger)
	releaseGate := holdEmployeeKeyGate(t, ctx, gate)
	rotationRepo := enterprise.NewRepository(rotationDB, enterpriseNoopAuthCacheInvalidator{})
	billingRepo := NewUsageBillingRepository(nil, billingDB)
	rotationDone := make(chan error, 1)
	billingDone := make(chan error, 1)
	var successor *enterprise.EmployeeKeyMutationResult
	var billingResult *service.UsageBillingApplyResult

	go func() {
		result, applyErr := billingRepo.Apply(ctx, &service.UsageBillingCommand{
			RequestID: "billing-before-rotation-" + fmt.Sprint(time.Now().UnixNano()), APIKeyID: fixture.apiKeyID,
			APIKeyQuotaCost: 0.5, APIKeyRateLimitCost: 0.5,
		})
		billingResult = result
		billingDone <- applyErr
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, billingApplication, billingDone)

	go func() {
		result, rotateErr := rotationRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rotation-after-billing",
			Plaintext: fmt.Sprintf("sk-rotation-after-billing-%d", time.Now().UnixNano()), ActorRef: "test:concurrent",
		})
		successor = result
		rotationDone <- rotateErr
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, rotationApplication, rotationDone)

	releaseGate()
	require.NoError(t, <-billingDone)
	require.NoError(t, <-rotationDone)
	require.NotNil(t, successor)
	require.NotNil(t, successor.Key)
	require.NotNil(t, billingResult)
	require.Equal(t, fixture.apiKeyID, billingResult.BilledAPIKeyID)
	quotaUsed, usage5h, usage1d, usage7d := employeeKeyUsageSnapshot(t, ctx, successor.Key.ID)
	require.Equal(t, 8.0, quotaUsed)
	require.Equal(t, 1.75, usage5h)
	require.Equal(t, 3.0, usage1d)
	require.Equal(t, 7.25, usage7d)
}

func TestEnterpriseKeyRebindWaitsForEmployeeKeyRotationBeforeLockingAPIKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	var originalCredential string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&originalCredential))

	const rotationApplication = "shan153-rotation-before-rebind"
	const rebindApplication = "shan153-rebind-during-rotation"
	gate := time.Now().UnixNano()
	rotationDB := openPartitionCleanupDB(t, ctx, "public", rotationApplication)
	rebindDB := openPartitionCleanupDB(t, ctx, "public", rebindApplication)
	cleanupTrigger := installEmployeeKeyLifecycleGate(t, ctx, fixture.enterpriseID, fixture.employeeID, gate, rotationApplication)
	t.Cleanup(cleanupTrigger)
	releaseGate := holdEmployeeKeyGate(t, ctx, gate)
	rotationRepo := enterprise.NewRepository(rotationDB, enterpriseNoopAuthCacheInvalidator{})
	rebindRepo := enterprise.NewRepository(rebindDB, enterpriseNoopAuthCacheInvalidator{})
	rotationDone := make(chan error, 1)
	rebindDone := make(chan error, 1)
	var successor *enterprise.EmployeeKeyMutationResult

	go func() {
		result, rotateErr := rotationRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rotation-before-rebind",
			Plaintext: fmt.Sprintf("sk-rotation-before-rebind-%d", time.Now().UnixNano()), ActorRef: "test:concurrent",
		})
		successor = result
		rotationDone <- rotateErr
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, rotationApplication, rotationDone)

	go func() {
		_, err := rebindRepo.RebindKeyAssignment(ctx, enterprise.RebindKeyAssignmentParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID, APIKeyID: fixture.apiKeyID,
			UpstreamSubscriptionID: fixture.upstreamSubscriptionID, ActorRef: "test:concurrent-rebind",
		})
		rebindDone <- err
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, rebindApplication, rebindDone)

	releaseGate()
	rotationErr := <-rotationDone
	rebindErr := <-rebindDone
	for _, result := range []struct {
		name string
		err  error
	}{
		{name: "rotation", err: rotationErr},
		{name: "rebind", err: rebindErr},
	} {
		var pqErr *pq.Error
		if errors.As(result.err, &pqErr) {
			require.NotEqual(t, pq.ErrorCode("40P01"), pqErr.Code, "%s must not be aborted by a PostgreSQL deadlock", result.name)
		}
	}
	require.NoError(t, rotationErr)
	require.ErrorIs(t, rebindErr, enterprise.ErrKeyGenerationRevoked)
	require.NotNil(t, successor)
	require.NotNil(t, successor.Key)

	var oldStatus, oldCredential, oldAssignmentStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT api_key.status, api_key.key, assignment.status
		FROM api_keys AS api_key
		JOIN enterprise_key_assignments AS assignment ON assignment.api_key_id = api_key.id
		WHERE api_key.id = $1
		ORDER BY assignment.generation DESC, assignment.assigned_at DESC, assignment.id DESC
		LIMIT 1`, fixture.apiKeyID).Scan(&oldStatus, &oldCredential, &oldAssignmentStatus))
	require.Equal(t, "disabled", oldStatus)
	require.Equal(t, "revoked", oldAssignmentStatus)
	require.NotEqual(t, originalCredential, oldCredential)

	var activeAPIKeyID int64
	var activeKeyStatus, activeAssignmentStatus string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT assignment.api_key_id, api_key.status, assignment.status
		FROM enterprise_key_assignments AS assignment
		JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2 AND assignment.status = 'active'`,
		fixture.enterpriseID, fixture.employeeID).Scan(&activeAPIKeyID, &activeKeyStatus, &activeAssignmentStatus))
	require.Equal(t, successor.Key.ID, activeAPIKeyID)
	require.Equal(t, "active", activeKeyStatus)
	require.Equal(t, "active", activeAssignmentStatus)
}

func TestEnterpriseKeyRebindAndSubscriptionBillingUseConsistentLockOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	secondSubscriptionID, _ := insertEnterpriseUpstreamSubscription(t, ctx, fixture.enterpriseID)
	const rebindApplication = "shan153-rebind-before-billing"
	const billingApplication = "shan153-billing-during-rebind"
	gate := time.Now().UnixNano()
	rebindDB := openPartitionCleanupDB(t, ctx, "public", rebindApplication)
	billingDB := openPartitionCleanupDB(t, ctx, "public", billingApplication)
	cleanupTrigger := installEmployeeKeyBillingGate(t, ctx, fixture.apiKeyID, gate, rebindApplication)
	t.Cleanup(cleanupTrigger)
	releaseGate := holdEmployeeKeyGate(t, ctx, gate)
	rebindRepo := enterprise.NewRepository(rebindDB, enterpriseNoopAuthCacheInvalidator{})
	billingRepo := NewUsageBillingRepository(nil, billingDB)
	rebindDone := make(chan error, 1)
	billingDone := make(chan error, 1)

	go func() {
		_, err := rebindRepo.RebindKeyAssignment(ctx, enterprise.RebindKeyAssignmentParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID, APIKeyID: fixture.apiKeyID,
			UpstreamSubscriptionID: secondSubscriptionID, ActorRef: "test:concurrent-rebind",
		})
		rebindDone <- err
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, rebindApplication, rebindDone)

	go func() {
		_, err := billingRepo.Apply(ctx, &service.UsageBillingCommand{
			RequestID: "billing-during-rebind-" + fmt.Sprint(time.Now().UnixNano()), APIKeyID: fixture.apiKeyID,
			SubscriptionID: &fixture.upstreamSubscriptionID, SubscriptionCost: 0.5,
			APIKeyQuotaCost: 0.5, APIKeyRateLimitCost: 0.5,
		})
		billingDone <- err
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, billingApplication, billingDone)

	releaseGate()
	require.NoError(t, <-rebindDone)
	require.NoError(t, <-billingDone)
}

func TestEnterpriseEmployeeKeyRotationRollsBackWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION fail_employee_key_audit_for_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.event_type = 'key.employee_rotate' AND NEW.enterprise_id = %d THEN
				RAISE EXCEPTION 'forced employee key audit failure';
			END IF;
			RETURN NEW;
		END $$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_employee_key_audit_for_test
		BEFORE INSERT ON enterprise_audit_events
		FOR EACH ROW EXECUTE FUNCTION fail_employee_key_audit_for_test()`, fixture.enterpriseID))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS fail_employee_key_audit_for_test ON enterprise_audit_events`)
		_, _ = integrationDB.ExecContext(context.Background(), `DROP FUNCTION IF EXISTS fail_employee_key_audit_for_test()`)
	})

	plain := fmt.Sprintf("sk-enterprise-rollback-%d", time.Now().UnixNano())
	_, err = repo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rollback-audit",
		Plaintext: plain, ActorRef: "test:rollback",
	})
	require.ErrorContains(t, err, "forced employee key audit failure")
	var oldStatus string
	var newCount, activeCount, idemCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&oldStatus))
	require.Equal(t, "active", oldStatus)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE key = $1`, plain).Scan(&newCount))
	require.Zero(t, newCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_key_assignments WHERE employee_id = $1 AND status = 'active'`, fixture.employeeID).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_key_lifecycle_idempotency WHERE employee_id = $1 AND idempotency_key = 'rollback-audit'`, fixture.employeeID).Scan(&idemCount))
	require.Zero(t, idemCount)
}

func TestEnterpriseEmployeeKeyCreateReplaysWithoutDuplicatingCurrentKey(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	plain := fmt.Sprintf("sk-enterprise-create-%d", time.Now().UnixNano())
	params := enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.secondEmployee,
		IdempotencyKey: "create-once", Plaintext: plain, ActorRef: "test:employee",
	}

	created, err := repo.CreateEmployeeKey(ctx, params)
	require.NoError(t, err)
	require.False(t, created.Replayed)
	require.Equal(t, plain, created.Plaintext)

	replayed, err := repo.CreateEmployeeKey(ctx, params)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Empty(t, replayed.Plaintext)
	require.Equal(t, created.Key.ID, replayed.Key.ID)

	var activeCount, auditCount, idempotencyCount, plaintextAuditLeaks int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.secondEmployee).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND event_type = 'key.employee_create'`, fixture.enterpriseID).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_lifecycle_idempotency
		WHERE enterprise_id = $1 AND employee_id = $2 AND operation = 'create' AND result_api_key_id = $3`,
		fixture.enterpriseID, fixture.secondEmployee, created.Key.ID).Scan(&idempotencyCount))
	require.Equal(t, 1, idempotencyCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND payload::text LIKE '%' || $2 || '%'`, fixture.enterpriseID, plain).Scan(&plaintextAuditLeaks))
	require.Zero(t, plaintextAuditLeaks)
}

func TestEnterpriseEmployeeKeyDisablePreservesHistoryAndReplaysWithoutPlaintext(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	var originalKey string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&originalKey))
	params := enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "disable-once", ActorRef: "test:employee",
	}

	disabled, err := repo.DisableEmployeeKey(ctx, params)
	require.NoError(t, err)
	require.False(t, disabled.Replayed)
	require.Empty(t, disabled.Plaintext)
	require.Equal(t, "disabled", disabled.Key.Status)

	replayed, err := repo.DisableEmployeeKey(ctx, params)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Empty(t, replayed.Plaintext)
	require.Equal(t, fixture.apiKeyID, replayed.Key.ID)

	var keyStatus, assignmentStatus, storedKey string
	var endedAt, revokedAt *time.Time
	var activeCount, auditCount, idempotencyCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status, key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&keyStatus, &storedKey))
	require.Equal(t, "disabled", keyStatus)
	require.Equal(t, fmt.Sprintf(":revoked:%d", fixture.apiKeyID), storedKey)
	require.NotEqual(t, originalKey, storedKey)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status, ended_at, revoked_at FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND api_key_id = $3`,
		fixture.enterpriseID, fixture.employeeID, fixture.apiKeyID).Scan(&assignmentStatus, &endedAt, &revokedAt))
	require.Equal(t, "revoked", assignmentStatus)
	require.NotNil(t, endedAt)
	require.NotNil(t, revokedAt)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.employeeID).Scan(&activeCount))
	require.Zero(t, activeCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND event_type = 'key.employee_disable'`, fixture.enterpriseID).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_lifecycle_idempotency
		WHERE enterprise_id = $1 AND employee_id = $2 AND operation = 'disable' AND result_api_key_id = $3`,
		fixture.enterpriseID, fixture.employeeID, fixture.apiKeyID).Scan(&idempotencyCount))
	require.Equal(t, 1, idempotencyCount)
}

func TestEnterpriseEmployeeStatusChangeRevokesCurrentKey(t *testing.T) {
	for _, operation := range []struct {
		name           string
		expectedStatus string
		apply          func(*enterpriseidentity.Service, context.Context, enterpriseFixture) error
	}{
		{
			name:           "disable",
			expectedStatus: "disabled",
			apply: func(svc *enterpriseidentity.Service, ctx context.Context, fixture enterpriseFixture) error {
				return svc.UpdateEmployee(ctx, fixture.enterpriseID, fixture.employeeID, "disabled", nil, 1)
			},
		},
		{
			name:           "terminate",
			expectedStatus: "terminated",
			apply: func(svc *enterpriseidentity.Service, ctx context.Context, fixture enterpriseFixture) error {
				return svc.TerminateEmployee(ctx, fixture.enterpriseID, fixture.employeeID)
			},
		},
	} {
		t.Run(operation.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := seedEnterpriseFixture(t, ctx)
			var originalKey string
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&originalKey))

			invalidator := &employeeKeyAuthCacheInvalidatorStub{}
			svc := enterpriseidentity.NewService(integrationDB, &config.Config{}, nil, nil, invalidator)
			require.NoError(t, operation.apply(svc, ctx, fixture))

			var employeeStatus, assignmentStatus, keyStatus, storedKey string
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM enterprise_employees WHERE id = $1`, fixture.employeeID).Scan(&employeeStatus))
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM enterprise_key_assignments WHERE api_key_id = $1`, fixture.apiKeyID).Scan(&assignmentStatus))
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status, key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&keyStatus, &storedKey))
			require.Equal(t, operation.expectedStatus, employeeStatus)
			require.Equal(t, "revoked", assignmentStatus)
			require.Equal(t, "disabled", keyStatus)
			require.Equal(t, fmt.Sprintf(":revoked:%d", fixture.apiKeyID), storedKey)
			require.NotEqual(t, originalKey, storedKey)
			require.Equal(t, []string{originalKey}, invalidator.keys)

			var originalKeyCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE key = $1`, originalKey).Scan(&originalKeyCount))
			require.Zero(t, originalKeyCount)
		})
	}
}

func TestEnterpriseEmployeeKeyMutationRateLimitRejectsBeforeIdempotencyWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		path string
		body func(enterpriseFixture) string
	}{
		{name: "create", path: "/api/v1/enterprise/keys", body: func(enterpriseFixture) string { return "" }},
		{name: "disable", path: "/api/v1/enterprise/keys/disable", body: func(fixture enterpriseFixture) string {
			return fmt.Sprintf(`{"expected_api_key_id":%d}`, fixture.apiKeyID)
		}},
		{name: "rotate", path: "/api/v1/enterprise/keys/rotate", body: func(fixture enterpriseFixture) string {
			return fmt.Sprintf(`{"expected_api_key_id":%d}`, fixture.apiKeyID)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := seedEnterpriseFixture(t, ctx)
			employeeID := fixture.employeeID
			if test.name == "create" {
				employeeID = fixture.secondEmployee
			}
			const password = "employee-rate-limit-password"
			var host, email string
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT portal_host FROM enterprises WHERE id = $1`, fixture.enterpriseID).Scan(&host))
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT current_email FROM enterprise_employees WHERE id = $1`, employeeID).Scan(&email))
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `
				UPDATE enterprise_employees
				SET password_hash = $1, must_change_password = FALSE
				WHERE id = $2`, string(hash), employeeID)
			require.NoError(t, err)

			identityService := enterpriseidentity.NewService(integrationDB, &config.Config{JWT: config.JWTConfig{Secret: "enterprise-key-rate-limit-secret"}}, nil, nil, nil)
			session, err := identityService.Login(ctx, host, email, password, "integration", "127.0.0.1")
			require.NoError(t, err)
			generator := &employeeKeyHTTPGenerator{value: fmt.Sprintf("sk-rate-limit-%d", time.Now().UnixNano())}
			handler := enterpriseidentity.NewHandler(
				identityService,
				enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{}),
				generator,
				testRedis(t),
			)
			router := gin.New()
			handler.RegisterRoutes(router.Group("/api/v1"))

			perform := func(idempotencyKey string) *httptest.ResponseRecorder {
				body := test.body(fixture)
				req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(body))
				req.Host = host
				req.Header.Set("Authorization", "Bearer "+session.AccessToken)
				if body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				if idempotencyKey != "" {
					req.Header.Set("Idempotency-Key", idempotencyKey)
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, req)
				return recorder
			}

			for attempt := 0; attempt < 10; attempt++ {
				recorder := perform("")
				require.Equal(t, http.StatusBadRequest, recorder.Code)
			}

			var beforeCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM enterprise_key_lifecycle_idempotency
				WHERE enterprise_id = $1 AND employee_id = $2`, fixture.enterpriseID, employeeID).Scan(&beforeCount))
			for attempt := 0; attempt < 2; attempt++ {
				idempotencyKey := fmt.Sprintf("rate-limit-overflow-%s-%d", test.name, attempt)
				recorder := perform(idempotencyKey)
				require.Equal(t, http.StatusTooManyRequests, recorder.Code)
				var persisted int
				require.NoError(t, integrationDB.QueryRowContext(ctx, `
					SELECT COUNT(*)
					FROM enterprise_key_lifecycle_idempotency
					WHERE enterprise_id = $1 AND employee_id = $2 AND idempotency_key = $3`,
					fixture.enterpriseID, employeeID, idempotencyKey).Scan(&persisted))
				require.Zero(t, persisted)
			}
			var afterCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM enterprise_key_lifecycle_idempotency
				WHERE enterprise_id = $1 AND employee_id = $2`, fixture.enterpriseID, employeeID).Scan(&afterCount))
			require.Equal(t, beforeCount, afterCount)
			require.Zero(t, generator.calls)
		})
	}
}

func TestEnterpriseEmployeeKeyHTTPProtocolUsesAuthenticatedEmployeeAndHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	const employeePassword = "employee-password-strong"
	const secondEmployeePassword = "second-employee-password-strong"
	const adminPassword = "admin-password-strong"

	var host, employeeEmail, secondEmployeeEmail, adminEmail, originalKey string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT portal_host FROM enterprises WHERE id = $1`, fixture.enterpriseID).Scan(&host))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT current_email FROM enterprise_employees WHERE id = $1`, fixture.employeeID).Scan(&employeeEmail))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT current_email FROM enterprise_employees WHERE id = $1`, fixture.secondEmployee).Scan(&secondEmployeeEmail))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1`, fixture.enterpriseUserID).Scan(&adminEmail))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&originalKey))

	setEmployeePassword := func(id int64, password string) {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `UPDATE enterprise_employees SET password_hash = $1, must_change_password = FALSE WHERE id = $2`, string(hash), id)
		require.NoError(t, err)
	}
	setEmployeePassword(fixture.employeeID, employeePassword)
	setEmployeePassword(fixture.secondEmployee, secondEmployeePassword)
	adminHash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.MinCost)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, string(adminHash), fixture.enterpriseUserID)
	require.NoError(t, err)

	identityService := enterpriseidentity.NewService(integrationDB, &config.Config{JWT: config.JWTConfig{Secret: "enterprise-http-protocol-secret"}}, nil, nil, nil)
	employeeSession, err := identityService.Login(ctx, host, employeeEmail, employeePassword, "integration", "127.0.0.1")
	require.NoError(t, err)
	secondEmployeeSession, err := identityService.Login(ctx, host, secondEmployeeEmail, secondEmployeePassword, "integration", "127.0.0.1")
	require.NoError(t, err)
	adminSession, err := identityService.Login(ctx, host, adminEmail, adminPassword, "integration", "127.0.0.1")
	require.NoError(t, err)

	generator := &employeeKeyHTTPGenerator{value: fmt.Sprintf("sk-http-rotate-%d", time.Now().UnixNano())}
	handler := enterpriseidentity.NewHandler(identityService, enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{}), generator, testRedis(t))
	router := gin.New()
	handler.RegisterRoutes(router.Group("/api/v1"))

	perform := func(method, path, body, accessToken, requestHost, idempotencyKey string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = requestHost
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if accessToken != "" {
			req.Header.Set("Authorization", "Bearer "+accessToken)
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	current := perform(http.MethodGet, "/api/v1/enterprise/keys/current", "", employeeSession.AccessToken, strings.ToUpper(host)+":443", "")
	require.Equal(t, http.StatusOK, current.Code)
	require.Contains(t, current.Body.String(), enterprise.MaskEmployeeKey(originalKey))
	require.NotContains(t, current.Body.String(), originalKey)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE api_keys SET expires_at = NOW() - INTERVAL '1 second',
			usage_5h = 1.25, usage_1d = 2.5, usage_7d = 6.75,
			window_5h_start = NOW() - INTERVAL '5 hours 1 second',
			window_1d_start = NOW() - INTERVAL '24 hours 1 second',
			window_7d_start = NOW() - INTERVAL '7 days 1 second'
		WHERE id = $1`, fixture.apiKeyID)
	require.NoError(t, err)
	projected := perform(http.MethodGet, "/api/v1/enterprise/keys/current", "", employeeSession.AccessToken, host, "")
	require.Equal(t, http.StatusOK, projected.Code)
	require.Contains(t, projected.Body.String(), `"status":"expired"`)
	require.Contains(t, projected.Body.String(), `"usage_5h":0`)
	require.Contains(t, projected.Body.String(), `"usage_1d":0`)
	require.Contains(t, projected.Body.String(), `"usage_7d":0`)
	require.NotContains(t, projected.Body.String(), "reset_5h_at")
	_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET expires_at = NULL WHERE id = $1`, fixture.apiKeyID)
	require.NoError(t, err)

	wrongHost := perform(http.MethodGet, "/api/v1/enterprise/keys/current", "", employeeSession.AccessToken, "wrong.example.test", "")
	require.Equal(t, http.StatusUnauthorized, wrongHost.Code)
	require.NotContains(t, wrongHost.Body.String(), originalKey)
	tamperedToken := perform(http.MethodGet, "/api/v1/enterprise/keys/current", "", employeeSession.AccessToken+"invalid", host, "")
	require.Equal(t, http.StatusUnauthorized, tamperedToken.Code)
	require.NotContains(t, tamperedToken.Body.String(), originalKey)

	admin := perform(http.MethodGet, "/api/v1/enterprise/keys/current", "", adminSession.AccessToken, host, "")
	require.Equal(t, http.StatusForbidden, admin.Code)
	require.NotContains(t, admin.Body.String(), originalKey)

	crossEmployee := perform(http.MethodPost, "/api/v1/enterprise/keys/rotate", fmt.Sprintf(`{"expected_api_key_id":%d}`, fixture.apiKeyID), secondEmployeeSession.AccessToken, host, "cross-employee")
	require.Equal(t, http.StatusNotFound, crossEmployee.Code)
	require.NotContains(t, crossEmployee.Body.String(), generator.value)

	create := perform(http.MethodPost, "/api/v1/enterprise/keys", "", secondEmployeeSession.AccessToken, host, "create-once")
	require.Equal(t, http.StatusCreated, create.Code)
	require.Equal(t, "no-store", create.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", create.Header().Get("Pragma"))
	require.Contains(t, create.Body.String(), generator.value)

	generator.value = fmt.Sprintf("sk-http-rotate-next-%d", time.Now().UnixNano())
	rotate := perform(http.MethodPost, "/api/v1/enterprise/keys/rotate", fmt.Sprintf(`{"expected_api_key_id":%d}`, fixture.apiKeyID), employeeSession.AccessToken, host, "rotate-once")
	require.Equal(t, http.StatusOK, rotate.Code)
	require.Equal(t, "no-store", rotate.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", rotate.Header().Get("Pragma"))

	var successorID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT api_key_id FROM enterprise_key_assignments WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.employeeID).Scan(&successorID))
	apiKeyService := service.NewAPIKeyService(
		NewAPIKeyRepository(integrationEntClient, integrationDB),
		NewUserRepository(integrationEntClient, integrationDB),
		nil, nil, nil, nil, &config.Config{},
	)
	_, _, err = apiKeyService.ValidateKey(ctx, originalKey)
	require.Error(t, err)
	_, _, err = apiKeyService.ValidateKey(ctx, generator.value)
	require.NoError(t, err)
	disable := perform(http.MethodPost, "/api/v1/enterprise/keys/disable", fmt.Sprintf(`{"expected_api_key_id":%d}`, successorID), employeeSession.AccessToken, host, "disable-once")
	require.Equal(t, http.StatusOK, disable.Code)
	require.NotContains(t, disable.Body.String(), generator.value)
}

func TestEnterpriseEmployeeKeyRejectsCrossEmployeeAndCrossEnterpriseScope(t *testing.T) {
	ctx := context.Background()
	first := seedEnterpriseFixture(t, ctx)
	second := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	for _, params := range []enterprise.EmployeeKeyMutationParams{
		{
			EnterpriseID: first.enterpriseID, EmployeeID: first.secondEmployee,
			ExpectedAPIKeyID: first.apiKeyID, IdempotencyKey: "cross-employee", Plaintext: "sk-cross-employee", ActorRef: "test:employee",
		},
		{
			EnterpriseID: second.enterpriseID, EmployeeID: first.employeeID,
			ExpectedAPIKeyID: first.apiKeyID, IdempotencyKey: "cross-enterprise", Plaintext: "sk-cross-enterprise", ActorRef: "test:employee",
		},
	} {
		_, err := repo.RotateEmployeeKey(ctx, params)
		require.ErrorIs(t, err, enterprise.ErrEmployeeKeyNotFound)
	}

	var firstActive, secondActive, auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, first.enterpriseID, first.employeeID).Scan(&firstActive))
	require.Equal(t, 1, firstActive)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, second.enterpriseID, second.employeeID).Scan(&secondActive))
	require.Equal(t, 1, secondActive)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id IN ($1, $2) AND event_type LIKE 'key.employee_%'`, first.enterpriseID, second.enterpriseID).Scan(&auditCount))
	require.Zero(t, auditCount)
}

func TestEnterpriseEmployeeKeyConcurrentCreateReplaysOneIdempotencyResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	plain := fmt.Sprintf("sk-enterprise-concurrent-create-%d", time.Now().UnixNano())
	params := enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.secondEmployee,
		IdempotencyKey: "concurrent-create", Plaintext: plain, ActorRef: "test:concurrent",
	}
	type outcome struct {
		result *enterprise.EmployeeKeyMutationResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			result, err := repo.CreateEmployeeKey(ctx, params)
			results <- outcome{result: result, err: err}
		}()
	}
	close(start)

	var created, replayed int
	for range 2 {
		outcome := <-results
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		if outcome.result.Replayed {
			replayed++
			require.Empty(t, outcome.result.Plaintext)
		} else {
			created++
			require.Equal(t, plain, outcome.result.Plaintext)
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, 1, replayed)

	var keyCount, activeCount, auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE key = $1`, plain).Scan(&keyCount))
	require.Equal(t, 1, keyCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.secondEmployee).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND event_type = 'key.employee_create'`, fixture.enterpriseID).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestEnterpriseEmployeeKeyRejectsIdempotencyKeyWithDifferentRequestHash(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	params := enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rotate-hash", Plaintext: "sk-rotate-hash", ActorRef: "test:employee",
	}
	rotated, err := repo.RotateEmployeeKey(ctx, params)
	require.NoError(t, err)
	conflicting := params
	conflicting.ExpectedAPIKeyID = rotated.Key.ID
	conflicting.Plaintext = "sk-rotate-conflict"
	_, err = repo.RotateEmployeeKey(ctx, conflicting)
	require.ErrorIs(t, err, enterprise.ErrEmployeeKeyIdempotency)

	var activeCount, auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.employeeID).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND event_type = 'key.employee_rotate'`, fixture.enterpriseID).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestEnterpriseEmployeeKeyCreateAndDisableRollBackWhenAuditFails(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		ctx := context.Background()
		fixture := seedEnterpriseFixture(t, ctx)
		installEmployeeKeyAuditFailure(t, ctx, fixture.enterpriseID, "key.employee_create")
		plain := fmt.Sprintf("sk-enterprise-create-rollback-%d", time.Now().UnixNano())
		repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
		_, err := repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.secondEmployee,
			IdempotencyKey: "create-rollback", Plaintext: plain, ActorRef: "test:rollback",
		})
		require.ErrorContains(t, err, "forced employee key audit failure")
		var keyCount, activeCount, idempotencyCount int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE key = $1`, plain).Scan(&keyCount))
		require.Zero(t, keyCount)
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM enterprise_key_assignments
			WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.secondEmployee).Scan(&activeCount))
		require.Zero(t, activeCount)
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM enterprise_key_lifecycle_idempotency
			WHERE enterprise_id = $1 AND employee_id = $2 AND idempotency_key = 'create-rollback'`, fixture.enterpriseID, fixture.secondEmployee).Scan(&idempotencyCount))
		require.Zero(t, idempotencyCount)
	})

	t.Run("disable", func(t *testing.T) {
		ctx := context.Background()
		fixture := seedEnterpriseFixture(t, ctx)
		installEmployeeKeyAuditFailure(t, ctx, fixture.enterpriseID, "key.employee_disable")
		repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
		_, err := repo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "disable-rollback", ActorRef: "test:rollback",
		})
		require.ErrorContains(t, err, "forced employee key audit failure")
		var keyStatus string
		var activeCount, idempotencyCount int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&keyStatus))
		require.Equal(t, "active", keyStatus)
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM enterprise_key_assignments
			WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.employeeID).Scan(&activeCount))
		require.Equal(t, 1, activeCount)
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM enterprise_key_lifecycle_idempotency
			WHERE enterprise_id = $1 AND employee_id = $2 AND idempotency_key = 'disable-rollback'`, fixture.enterpriseID, fixture.employeeID).Scan(&idempotencyCount))
		require.Zero(t, idempotencyCount)
	})
}

func installEmployeeKeyAuditFailure(t *testing.T, ctx context.Context, enterpriseID int64, eventType string) {
	t.Helper()
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION fail_employee_key_audit_for_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.event_type = '%s' AND NEW.enterprise_id = %d THEN
				RAISE EXCEPTION 'forced employee key audit failure';
			END IF;
			RETURN NEW;
		END $$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_employee_key_audit_for_test
		BEFORE INSERT ON enterprise_audit_events
		FOR EACH ROW EXECUTE FUNCTION fail_employee_key_audit_for_test()`, eventType, enterpriseID))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS fail_employee_key_audit_for_test ON enterprise_audit_events`)
		_, _ = integrationDB.ExecContext(context.Background(), `DROP FUNCTION IF EXISTS fail_employee_key_audit_for_test()`)
	})
}

func TestEnterpriseEmployeeKeyCreateAfterDisableInheritsTerminalSnapshot(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	window5h := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	window1d := window5h.Add(-2 * time.Hour)
	window7d := window5h.Add(-24 * time.Hour)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE api_keys SET quota = 25, quota_used = 7.5,
			rate_limit_5h = 5, rate_limit_1d = 10, rate_limit_7d = 20,
			usage_5h = 1.25, usage_1d = 2.5, usage_7d = 6.75,
			window_5h_start = $2, window_1d_start = $3, window_7d_start = $4
		WHERE id = $1`, fixture.apiKeyID, window5h, window1d, window7d)
	require.NoError(t, err)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err = repo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "disable-before-create", ActorRef: "test:employee",
	})
	require.NoError(t, err)

	successor, err := repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		IdempotencyKey: "create-after-disable", Plaintext: "sk-create-after-disable", ActorRef: "test:employee",
	})
	require.NoError(t, err)
	require.Equal(t, 25.0, successor.Key.Quota)
	require.Equal(t, 7.5, successor.Key.QuotaUsed)
	require.Equal(t, 1.25, successor.Key.Usage5h)
	require.Equal(t, 2.5, successor.Key.Usage1d)
	require.Equal(t, 6.75, successor.Key.Usage7d)
	require.WithinDuration(t, window5h, *successor.Key.Window5h, time.Second)
	require.WithinDuration(t, window1d, *successor.Key.Window1d, time.Second)
	require.WithinDuration(t, window7d, *successor.Key.Window7d, time.Second)
}

func TestEnterpriseEmployeeKeySuccessorRequiresCurrentActiveSubscription(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err := repo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "disable-before-expired-subscription", ActorRef: "test:employee",
	})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE enterprise_subscriptions SET status = 'ended', ended_at = NOW() WHERE enterprise_id = $1`, fixture.enterpriseID)
	require.NoError(t, err)

	_, err = repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		IdempotencyKey: "create-after-expired-subscription", Plaintext: "sk-create-after-expired-subscription", ActorRef: "test:employee",
	})

	require.ErrorIs(t, err, enterprise.ErrEmployeeKeyUnavailable)
	var activeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'active'`, fixture.enterpriseID, fixture.employeeID).Scan(&activeCount))
	require.Zero(t, activeCount)
}

func TestEnterpriseEmployeeKeySuccessorBindsCurrentSubscriptionAfterReplacement(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota = 25, quota_used = 7.5 WHERE id = $1`, fixture.apiKeyID)
	require.NoError(t, err)
	_, err = repo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "disable-before-subscription-replacement", ActorRef: "test:employee",
	})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE enterprise_subscriptions SET status = 'ended', ended_at = NOW() WHERE enterprise_id = $1`, fixture.enterpriseID)
	require.NoError(t, err)

	suffix := fmt.Sprint(time.Now().UnixNano())
	var groupID, subscriptionID int64
	anchor := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO groups (name, daily_limit_usd, weekly_limit_usd, monthly_limit_usd)
		VALUES ($1, 10, 20, 30) RETURNING id`, "enterprise-replacement-group-"+suffix).Scan(&groupID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO user_subscriptions (
			user_id, group_id, starts_at, expires_at, status,
			daily_window_start, weekly_window_start, monthly_window_start
		) VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active', $3::timestamptz, $3::timestamptz, $3::timestamptz)
		RETURNING id`, fixture.enterpriseUserID, groupID, anchor).Scan(&subscriptionID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			observed_weekly_window_start, activated_at, actor_ref
		) VALUES ($1, $2, 'active', $3, $3, 'test:replacement')
		RETURNING id`, fixture.enterpriseID, subscriptionID, anchor).Scan(new(int64)))

	successor, err := repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		IdempotencyKey: "create-after-subscription-replacement", Plaintext: "sk-create-after-subscription-replacement", ActorRef: "test:employee",
	})
	require.NoError(t, err)
	require.Equal(t, groupID, *successor.Key.GroupID)
	require.Equal(t, 25.0, successor.Key.Quota)
	require.Equal(t, 7.5, successor.Key.QuotaUsed)
	var assignmentSubscriptionID, assignmentGroupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT upstream_user_subscription_id, upstream_group_id
		FROM enterprise_key_assignments WHERE api_key_id = $1`, successor.Key.ID).Scan(&assignmentSubscriptionID, &assignmentGroupID))
	require.Equal(t, subscriptionID, assignmentSubscriptionID)
	require.Equal(t, groupID, assignmentGroupID)
}

func TestEnterpriseEmployeeKeyTerminalStatusCannotBecomeCallableThroughLifecycle(t *testing.T) {
	t.Run("disable quota exhausted", func(t *testing.T) {
		ctx := context.Background()
		fixture := seedEnterpriseFixture(t, ctx)
		_, err := integrationDB.ExecContext(ctx, `
			UPDATE api_keys SET quota = 5, quota_used = 5, status = 'quota_exhausted' WHERE id = $1`, fixture.apiKeyID)
		require.NoError(t, err)
		repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
		_, err = repo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "disable-exhausted", ActorRef: "test:employee",
		})
		require.NoError(t, err)
		var status string
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&status))
		require.Equal(t, "disabled", status)
	})

	t.Run("rotate expired", func(t *testing.T) {
		ctx := context.Background()
		fixture := seedEnterpriseFixture(t, ctx)
		_, err := integrationDB.ExecContext(ctx, `
			UPDATE api_keys SET status = 'expired', expires_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, fixture.apiKeyID)
		require.NoError(t, err)
		repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
		successor, err := repo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rotate-expired", Plaintext: "sk-rotate-expired", ActorRef: "test:employee",
		})
		require.NoError(t, err)
		require.Equal(t, "expired", successor.Key.Status)
		var oldStatus, successorStatus string
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&oldStatus))
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM api_keys WHERE id = $1`, successor.Key.ID).Scan(&successorStatus))
		require.Equal(t, "disabled", oldStatus)
		require.Equal(t, "expired", successorStatus)
	})
}

func TestEnterpriseEmployeeKeyLateBillingFollowsActiveSuccessor(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	window5h := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	window1d := window5h.Add(-2 * time.Hour)
	window7d := window5h.Add(-24 * time.Hour)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE api_keys SET quota = 25, quota_used = 7.5,
			rate_limit_5h = 5, rate_limit_1d = 10, rate_limit_7d = 20,
			usage_5h = 1.25, usage_1d = 2.5, usage_7d = 6.75,
			window_5h_start = $2, window_1d_start = $3, window_7d_start = $4
		WHERE id = $1`, fixture.apiKeyID, window5h, window1d, window7d)
	require.NoError(t, err)

	keyRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	successor, err := keyRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rotate-before-late-billing", Plaintext: "sk-late-billing", ActorRef: "test:employee",
	})
	require.NoError(t, err)

	billingRepo := NewUsageBillingRepository(nil, integrationDB)
	result, err := billingRepo.Apply(ctx, &service.UsageBillingCommand{
		RequestID: "late-billing-" + fmt.Sprint(time.Now().UnixNano()), APIKeyID: fixture.apiKeyID,
		APIKeyQuotaCost: 0.5, APIKeyRateLimitCost: 0.5,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Equal(t, successor.Key.ID, result.BilledAPIKeyID)

	var oldQuotaUsed, oldUsage5h, oldUsage1d, oldUsage7d float64
	var successorQuotaUsed, successorUsage5h, successorUsage1d, successorUsage7d float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT quota_used, usage_5h, usage_1d, usage_7d FROM api_keys WHERE id = $1`, fixture.apiKeyID).
		Scan(&oldQuotaUsed, &oldUsage5h, &oldUsage1d, &oldUsage7d))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT quota_used, usage_5h, usage_1d, usage_7d FROM api_keys WHERE id = $1`, successor.Key.ID).
		Scan(&successorQuotaUsed, &successorUsage5h, &successorUsage1d, &successorUsage7d))
	require.Equal(t, 7.5, oldQuotaUsed)
	require.Equal(t, 1.25, oldUsage5h)
	require.Equal(t, 2.5, oldUsage1d)
	require.Equal(t, 6.75, oldUsage7d)
	require.Equal(t, 8.0, successorQuotaUsed)
	require.Equal(t, 1.75, successorUsage5h)
	require.Equal(t, 3.0, successorUsage1d)
	require.Equal(t, 7.25, successorUsage7d)
}

func TestEnterpriseEmployeeKeyLateBillingUsesLatestTerminalSnapshotBeforeRecreate(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	window5h := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	window1d := window5h.Add(-2 * time.Hour)
	window7d := window5h.Add(-24 * time.Hour)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE api_keys SET quota = 25, quota_used = 7.5,
			rate_limit_5h = 5, rate_limit_1d = 10, rate_limit_7d = 20,
			usage_5h = 1.25, usage_1d = 2.5, usage_7d = 6.75,
			window_5h_start = $2, window_1d_start = $3, window_7d_start = $4
		WHERE id = $1`, fixture.apiKeyID, window5h, window1d, window7d)
	require.NoError(t, err)

	keyRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	rotated, err := keyRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: "rotate-before-terminal-billing", Plaintext: "sk-terminal-billing-b", ActorRef: "test:employee",
	})
	require.NoError(t, err)
	_, err = keyRepo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		ExpectedAPIKeyID: rotated.Key.ID, IdempotencyKey: "disable-before-terminal-billing", ActorRef: "test:employee",
	})
	require.NoError(t, err)

	billingRepo := NewUsageBillingRepository(nil, integrationDB)
	result, err := billingRepo.Apply(ctx, &service.UsageBillingCommand{
		RequestID: "terminal-late-billing-" + fmt.Sprint(time.Now().UnixNano()), APIKeyID: fixture.apiKeyID,
		APIKeyQuotaCost: 0.5, APIKeyRateLimitCost: 0.5,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Equal(t, rotated.Key.ID, result.BilledAPIKeyID)

	var initialQuotaUsed, terminalQuotaUsed, terminalUsage5h float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT quota_used FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&initialQuotaUsed))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT quota_used, usage_5h FROM api_keys WHERE id = $1`, rotated.Key.ID).Scan(&terminalQuotaUsed, &terminalUsage5h))
	require.Equal(t, 7.5, initialQuotaUsed)
	require.Equal(t, 8.0, terminalQuotaUsed)
	require.Equal(t, 1.75, terminalUsage5h)

	recreated, err := keyRepo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
		IdempotencyKey: "create-after-terminal-billing", Plaintext: "sk-terminal-billing-c", ActorRef: "test:employee",
	})
	require.NoError(t, err)
	require.Equal(t, 8.0, recreated.Key.QuotaUsed)
	require.Equal(t, 1.75, recreated.Key.Usage5h)
	require.Equal(t, 3.0, recreated.Key.Usage1d)
	require.Equal(t, 7.25, recreated.Key.Usage7d)
	require.WithinDuration(t, window5h, *recreated.Key.Window5h, time.Second)
	require.WithinDuration(t, window1d, *recreated.Key.Window1d, time.Second)
	require.WithinDuration(t, window7d, *recreated.Key.Window7d, time.Second)
}
