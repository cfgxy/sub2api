//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func prepareEnterpriseEmployeeReservationFixture(t *testing.T, ctx context.Context, allocation, usage float64) enterpriseFixture {
	t.Helper()
	fixture := seedEnterpriseFixture(t, ctx)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE api_keys
		SET rate_limit_7d = $2, usage_7d = $3, window_7d_start = $4
		WHERE id = $1`, fixture.apiKeyID, allocation, usage, fixture.anchor)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO enterprise_weekly_allocations (
			enterprise_id, subscription_id, employee_id, window_type, window_anchor, amount, version
		) VALUES ($1, $2, $3, 'week', $4, $5, 1)`,
		fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, fixture.anchor, allocation)
	require.NoError(t, err)
	return fixture
}

func enterpriseEmployeeReservationCommand(fixture enterpriseFixture, requestID, fingerprint string, amount float64) *service.EnterpriseEmployeeUsageReservationCommand {
	return &service.EnterpriseEmployeeUsageReservationCommand{
		RequestID:          requestID,
		RequestFingerprint: fingerprint,
		SourceAPIKeyID:     fixture.apiKeyID,
		ReservedUSD:        amount,
	}
}

func TestEnterpriseEmployeeUsageReservationConcurrentBoundaryAdmitsOnlyOne(t *testing.T) {
	ctx := context.Background()
	fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 0)
	repo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)

	start := make(chan struct{})
	errs := make(chan error, 2)
	for i, fingerprintByte := range []byte{'a', 'b'} {
		go func(index int, value byte) {
			<-start
			_, err := repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(
				fixture,
				fmt.Sprintf("reservation-boundary-%d-%d", time.Now().UnixNano(), index),
				strings.Repeat(string(value), 64),
				6,
			))
			errs <- err
		}(i, fingerprintByte)
	}
	close(start)

	var admitted, rejected int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			admitted++
		case errors.Is(err, service.ErrAPIKeyRateLimit7dExceeded):
			rejected++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, admitted)
	require.Equal(t, 1, rejected)

	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_employee_usage_reservations
		WHERE enterprise_id = $1 AND status = 'reserved'`, fixture.enterpriseID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestEnterpriseEmployeeUnknownCostReservationSerializesAndSettlesActualCost(t *testing.T) {
	ctx := context.Background()
	fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 0)
	repo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)

	type attempt struct {
		requestID string
		result    *service.EnterpriseEmployeeUsageReservationResult
		err       error
	}
	start := make(chan struct{})
	attempts := make(chan attempt, 2)
	for i, fingerprintByte := range []byte{'a', 'b'} {
		go func(index int, value byte) {
			requestID := fmt.Sprintf("reservation-unknown-cost-%d-%d", time.Now().UnixNano(), index)
			<-start
			result, err := repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(
				fixture, requestID, strings.Repeat(string(value), 64), 4,
			))
			attempts <- attempt{requestID: requestID, result: result, err: err}
		}(i, fingerprintByte)
	}
	close(start)

	var admitted attempt
	var rejected int
	for range 2 {
		got := <-attempts
		if got.err == nil {
			admitted = got
			continue
		}
		require.ErrorIs(t, got.err, service.ErrAPIKeyRateLimit7dExceeded)
		rejected++
	}
	require.NotNil(t, admitted.result)
	require.Equal(t, 1, rejected)
	require.False(t, admitted.result.MaximumCostKnown)

	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:                     admitted.requestID,
		APIKeyID:                      fixture.apiKeyID,
		APIKeyRateLimitCost:           6,
		EnterpriseEmployeeReservation: true,
		EnterpriseEmployeeActualUSD:   6,
	})
	require.NoError(t, err)
	require.NoError(t, repo.RecoverEnterpriseEmployeeUsage(ctx, fixture.apiKeyID))

	var usage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_7d FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&usage))
	require.InDelta(t, 6, usage, 0.000000001)
}

func TestEnterpriseEmployeeUsageReservationIdempotentLifecycleAndConflicts(t *testing.T) {
	ctx := context.Background()
	fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 1)
	repo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
	requestID := fmt.Sprintf("reservation-idempotency-%d", time.Now().UnixNano())
	fingerprint := strings.Repeat("c", 64)
	command := enterpriseEmployeeReservationCommand(fixture, requestID, fingerprint, 4)

	created, err := repo.ReserveEnterpriseEmployeeUsage(ctx, command)
	require.NoError(t, err)
	require.False(t, created.AlreadyExists)
	require.Equal(t, "reserved", created.Status)

	replayed, err := repo.ReserveEnterpriseEmployeeUsage(ctx, command)
	require.NoError(t, err)
	require.True(t, replayed.AlreadyExists)
	require.Equal(t, created.ReservationID, replayed.ReservationID)

	conflict := *command
	conflict.RequestFingerprint = strings.Repeat("d", 64)
	_, err = repo.ReserveEnterpriseEmployeeUsage(ctx, &conflict)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)

	settled, err := repo.SettleEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageSettlementCommand{
		RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID, ActualUSD: 3,
	})
	require.NoError(t, err)
	require.Equal(t, "settled", settled.Status)
	require.NotNil(t, settled.ActualUSD)
	require.InDelta(t, 3, *settled.ActualUSD, 0.000000001)

	settledReplay, err := repo.SettleEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageSettlementCommand{
		RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID, ActualUSD: 3,
	})
	require.NoError(t, err)
	require.Equal(t, settled.ReservationID, settledReplay.ReservationID)

	_, err = repo.SettleEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageSettlementCommand{
		RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID, ActualUSD: 2,
	})
	require.ErrorIs(t, err, service.ErrEnterpriseEmployeeReservationStateConflict)

	releasedAfterSettle, err := repo.ReleaseEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageReleaseCommand{
		RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID,
	})
	require.NoError(t, err)
	require.Equal(t, "settled", releasedAfterSettle.Status)

	var usage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_7d FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&usage))
	require.InDelta(t, 4, usage, 0.000000001)
}

func TestEnterpriseEmployeeUsageReservationReleaseIsIdempotentAndBlockedAfterBillingCommit(t *testing.T) {
	ctx := context.Background()
	t.Run("release before billing", func(t *testing.T) {
		fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 0)
		repo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
		requestID := fmt.Sprintf("reservation-release-%d", time.Now().UnixNano())
		_, err := repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(fixture, requestID, strings.Repeat("e", 64), 2))
		require.NoError(t, err)

		released, err := repo.ReleaseEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageReleaseCommand{
			RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID,
		})
		require.NoError(t, err)
		require.Equal(t, "released", released.Status)
		replayed, err := repo.ReleaseEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageReleaseCommand{
			RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID,
		})
		require.NoError(t, err)
		require.Equal(t, "released", replayed.Status)
	})

	t.Run("billing commit keeps reservation", func(t *testing.T) {
		fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 0)
		repo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
		requestID := fmt.Sprintf("reservation-committed-%d", time.Now().UnixNano())
		_, err := repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(fixture, requestID, strings.Repeat("f", 64), 2))
		require.NoError(t, err)
		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID: requestID, APIKeyID: fixture.apiKeyID, EnterpriseEmployeeReservation: true,
		})
		require.NoError(t, err)

		kept, err := repo.ReleaseEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageReleaseCommand{
			RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID,
		})
		require.NoError(t, err)
		require.Equal(t, "committed", kept.Status)
		require.True(t, kept.BillingCommitted)
	})
}

func TestEnterpriseEmployeeUsageCommittedSettlementRecoveryIsDurableAndIdempotent(t *testing.T) {
	ctx := context.Background()
	fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 0)
	repo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
	requestID := fmt.Sprintf("reservation-committed-recovery-%d", time.Now().UnixNano())

	_, err := repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(
		fixture, requestID, strings.Repeat("a", 64), 4,
	))
	require.NoError(t, err)

	applied, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID: requestID, APIKeyID: fixture.apiKeyID,
		APIKeyRateLimitCost: 3, EnterpriseEmployeeReservation: true,
		EnterpriseEmployeeActualUSD: 3,
	})
	require.NoError(t, err)
	require.True(t, applied.Applied)

	var status string
	var actual *float64
	var leaseExpiresAt *time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status, actual_usd, lease_expires_at
		FROM enterprise_employee_usage_reservations
		WHERE request_id = $1 AND source_api_key_id = $2`, requestID, fixture.apiKeyID).Scan(&status, &actual, &leaseExpiresAt))
	require.Equal(t, "committed", status)
	require.NotNil(t, actual)
	require.Nil(t, leaseExpiresAt)
	require.InDelta(t, 3, *actual, 0.000000001)

	var usage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_7d FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&usage))
	require.Zero(t, usage, "core transaction must not impersonate employee settlement")

	// A fresh repository instance models recovery after the usage worker or
	// process ended after core billing had committed.
	restarted := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
	require.NoError(t, restarted.RecoverEnterpriseEmployeeUsage(ctx, fixture.apiKeyID))
	require.NoError(t, restarted.RecoverEnterpriseEmployeeUsage(ctx, fixture.apiKeyID))

	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status FROM enterprise_employee_usage_reservations
		WHERE request_id = $1 AND source_api_key_id = $2`, requestID, fixture.apiKeyID).Scan(&status))
	require.Equal(t, "settled", status)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_7d FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&usage))
	require.InDelta(t, 3, usage, 0.000000001, "recovery must add the committed actual once")
}

func TestEnterpriseEmployeeExpiredReservationReleasesOnlyWithoutBillingEvidence(t *testing.T) {
	ctx := context.Background()
	fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 0)
	repo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
	requestID := fmt.Sprintf("reservation-expired-%d", time.Now().UnixNano())

	_, err := repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(
		fixture, requestID, strings.Repeat("e", 64), 4,
	))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE enterprise_employee_usage_reservations
		SET lease_expires_at = NOW() - INTERVAL '1 second'
		WHERE request_id = $1 AND source_api_key_id = $2`, requestID, fixture.apiKeyID)
	require.NoError(t, err)

	// A fresh repository models recovery after the request process stopped
	// before any durable billing transaction had started.
	restarted := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
	require.NoError(t, restarted.RecoverEnterpriseEmployeeUsage(ctx, fixture.apiKeyID))

	var status string
	var leaseExpiresAt *time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status, lease_expires_at
		FROM enterprise_employee_usage_reservations
		WHERE request_id = $1 AND source_api_key_id = $2`, requestID, fixture.apiKeyID).Scan(&status, &leaseExpiresAt))
	require.Equal(t, "released", status)
	require.Nil(t, leaseExpiresAt)

	_, err = repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(
		fixture, requestID+"-retry", strings.Repeat("f", 64), 4,
	))
	require.NoError(t, err)
}

func TestEnterpriseEmployeeReservationRenewalPreventsExpiredRecovery(t *testing.T) {
	ctx := context.Background()
	fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 0)
	repo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
	requestID := fmt.Sprintf("reservation-renew-%d", time.Now().UnixNano())

	_, err := repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(
		fixture, requestID, strings.Repeat("a", 64), 4,
	))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE enterprise_employee_usage_reservations
		SET lease_expires_at = NOW() - INTERVAL '1 second'
		WHERE request_id = $1 AND source_api_key_id = $2`, requestID, fixture.apiKeyID)
	require.NoError(t, err)

	alive, err := repo.RenewEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageRenewCommand{
		RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID,
	})
	require.NoError(t, err)
	require.True(t, alive)
	require.NoError(t, repo.RecoverEnterpriseEmployeeUsage(ctx, fixture.apiKeyID))

	var status string
	var leaseExpiresAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status, lease_expires_at
		FROM enterprise_employee_usage_reservations
		WHERE request_id = $1 AND source_api_key_id = $2`, requestID, fixture.apiKeyID).Scan(&status, &leaseExpiresAt))
	require.Equal(t, "reserved", status)
	require.True(t, leaseExpiresAt.After(time.Now().UTC()))
}

func TestEnterpriseEmployeeDelayedSettlementPreservesActualCostAndBlocksSubsequentAdmission(t *testing.T) {
	ctx := context.Background()
	fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 5, 0)
	repo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
	requestID := fmt.Sprintf("reservation-committed-admission-%d", time.Now().UnixNano())

	_, err := repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(
		fixture, requestID, strings.Repeat("b", 64), 5,
	))
	require.NoError(t, err)
	_, err = repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID: requestID, APIKeyID: fixture.apiKeyID,
		APIKeyRateLimitCost: 6, EnterpriseEmployeeReservation: true,
		EnterpriseEmployeeActualUSD: 6,
	})
	require.NoError(t, err)

	_, err = repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(
		fixture, requestID+"-pending", strings.Repeat("c", 64), 1,
	))
	require.ErrorIs(t, err, service.ErrAPIKeyRateLimit7dExceeded)

	require.NoError(t, repo.RecoverEnterpriseEmployeeUsage(ctx, fixture.apiKeyID))

	var usage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_7d FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&usage))
	require.InDelta(t, 6, usage, 0.000000001)

	_, err = repo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(
		fixture, requestID+"-after-settlement", strings.Repeat("d", 64), 1,
	))
	require.ErrorIs(t, err, service.ErrAPIKeyRateLimit7dExceeded)

	var status string
	var actual *float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT status, actual_usd
		FROM enterprise_employee_usage_reservations
		WHERE request_id = $1 AND source_api_key_id = $2`, requestID, fixture.apiKeyID).Scan(&status, &actual))
	require.Equal(t, "settled", status)
	require.NotNil(t, actual)
	require.InDelta(t, 6, *actual, 0.000000001)
}

func TestEnterpriseEmployeeUsageSettlementFollowsSuccessorAndDoesNotPolluteAdvancedWindow(t *testing.T) {
	ctx := context.Background()
	t.Run("same window successor", func(t *testing.T) {
		fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 2)
		billingRepo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
		requestID := fmt.Sprintf("reservation-successor-%d", time.Now().UnixNano())
		_, err := billingRepo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(fixture, requestID, strings.Repeat("1", 64), 4))
		require.NoError(t, err)

		keyRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
		successor, err := keyRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: requestID + "-rotate",
			Plaintext: "sk-" + requestID, ActorRef: "test:reservation",
		})
		require.NoError(t, err)

		settled, err := billingRepo.SettleEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageSettlementCommand{
			RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID, ActualUSD: 3,
		})
		require.NoError(t, err)
		require.Equal(t, successor.Key.ID, settled.BilledAPIKeyID)
		var usage float64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_7d FROM api_keys WHERE id = $1`, successor.Key.ID).Scan(&usage))
		require.InDelta(t, 5, usage, 0.000000001)
	})

	t.Run("advanced window", func(t *testing.T) {
		fixture := prepareEnterpriseEmployeeReservationFixture(t, ctx, 10, 2)
		billingRepo := NewUsageBillingRepository(nil, integrationDB).(*usageBillingRepository)
		requestID := fmt.Sprintf("reservation-old-window-%d", time.Now().UnixNano())
		_, err := billingRepo.ReserveEnterpriseEmployeeUsage(ctx, enterpriseEmployeeReservationCommand(fixture, requestID, strings.Repeat("2", 64), 4))
		require.NoError(t, err)

		keyRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
		successor, err := keyRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID, IdempotencyKey: requestID + "-rotate",
			Plaintext: "sk-" + requestID, ActorRef: "test:reservation",
		})
		require.NoError(t, err)

		newAnchor := fixture.anchor.Add(7 * 24 * time.Hour)
		_, err = integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET weekly_window_start = $2 WHERE id = $1`, fixture.upstreamSubscriptionID, newAnchor)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `
			INSERT INTO enterprise_weekly_allocations (
				enterprise_id, subscription_id, employee_id, window_type, window_anchor, amount, version
			) VALUES ($1, $2, $3, 'week', $4, 10, 1)`,
			fixture.enterpriseID, fixture.subscriptionID, fixture.employeeID, newAnchor)
		require.NoError(t, err)

		apiKeyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
		synchronizer, ok := apiKeyRepo.(interface {
			SyncEnterpriseEmployeeWeeklyLimit(context.Context, int64, *service.EnterpriseUsageAttributionIdentity) (*service.EnterpriseEmployeeWeeklyLimitState, error)
		})
		require.True(t, ok)
		employeeID := fixture.employeeID
		state, err := synchronizer.SyncEnterpriseEmployeeWeeklyLimit(ctx, successor.Key.ID, &service.EnterpriseUsageAttributionIdentity{
			EnterpriseID: fixture.enterpriseID, EnterpriseSubscriptionID: fixture.subscriptionID,
			UpstreamSubscriptionID: fixture.upstreamSubscriptionID, EmployeeID: &employeeID,
			WeeklyWindowAnchor: &newAnchor,
		})
		require.NoError(t, err)
		require.Zero(t, state.Usage7d)

		settled, err := billingRepo.SettleEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageSettlementCommand{
			RequestID: requestID, SourceAPIKeyID: fixture.apiKeyID, ActualUSD: 3,
		})
		require.NoError(t, err)
		require.Zero(t, settled.BilledAPIKeyID)
		var usage float64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT usage_7d FROM api_keys WHERE id = $1`, successor.Key.ID).Scan(&usage))
		require.Zero(t, usage)
	})
}
