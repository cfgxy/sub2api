//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/enterpriseidentity"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseEmployeeKeyTerminalStateAllowsRecreate(t *testing.T) {
	cases := []struct {
		name           string
		makeTerminal   func(t *testing.T, ctx context.Context, repo *enterprise.Repository, fixture enterpriseFixture)
		oldAssignment  string
		assertFreshKey bool
	}{
		{
			name: "expired key",
			makeTerminal: func(t *testing.T, ctx context.Context, _ *enterprise.Repository, fixture enterpriseFixture) {
				t.Helper()
				_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET expires_at = statement_timestamp() - INTERVAL '1 second' WHERE id = $1`, fixture.apiKeyID)
				require.NoError(t, err)
			},
			oldAssignment:  "ended",
			assertFreshKey: true,
		},
		{
			name: "quota exhausted key",
			makeTerminal: func(t *testing.T, ctx context.Context, _ *enterprise.Repository, fixture enterpriseFixture) {
				t.Helper()
				_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota = 1, quota_used = 1 WHERE id = $1`, fixture.apiKeyID)
				require.NoError(t, err)
			},
			oldAssignment:  "ended",
			assertFreshKey: true,
		},
		{
			name: "explicitly revoked key",
			makeTerminal: func(t *testing.T, ctx context.Context, repo *enterprise.Repository, fixture enterpriseFixture) {
				t.Helper()
				_, err := repo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
					EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID, ExpectedAPIKeyID: fixture.apiKeyID,
					IdempotencyKey: "terminal-revoke", ActorRef: "test:employee",
				})
				require.NoError(t, err)
			},
			oldAssignment: "revoked",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := seedEnterpriseFixture(t, ctx)
			repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
			tt.makeTerminal(t, ctx, repo, fixture)

			created, err := repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
				EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
				IdempotencyKey: "terminal-recreate-" + tt.name,
				Plaintext:      fmt.Sprintf("sk-terminal-recreate-%d", time.Now().UnixNano()),
				ActorRef:       "test:employee",
			})
			require.NoError(t, err)
			require.NotEqual(t, fixture.apiKeyID, created.Key.ID)
			require.Equal(t, "active", created.Key.Status)

			var oldStatus, activeAssignmentStatus string
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT status FROM enterprise_key_assignments WHERE enterprise_id = $1 AND api_key_id = $2`, fixture.enterpriseID, fixture.apiKeyID).Scan(&oldStatus))
			require.Equal(t, tt.oldAssignment, oldStatus)
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT status FROM enterprise_key_assignments WHERE enterprise_id = $1 AND api_key_id = $2`, fixture.enterpriseID, created.Key.ID).Scan(&activeAssignmentStatus))
			require.Equal(t, "active", activeAssignmentStatus)
			if tt.assertFreshKey {
				require.Zero(t, created.Key.Quota)
				require.Nil(t, created.Key.ExpiresAt)
			}
		})
	}
}

func TestEnterpriseEmployeeSeatsRejectAtCapacityAndReleaseOnDisableOrTerminate(t *testing.T) {
	for _, release := range []string{"disable", "terminate"} {
		t.Run(release, func(t *testing.T) {
			ctx := context.Background()
			fixture := seedEnterpriseFixture(t, ctx)
			setEnterpriseFixtureSeatCount(t, ctx, fixture, 2)
			svc := enterpriseidentity.NewService(integrationDB, &config.Config{}, nil, nil, nil)

			_, err := svc.CreateEmployee(ctx, fixture.enterpriseID, fmt.Sprintf("blocked-%d@example.com", time.Now().UnixNano()), "strong-password", nil)
			require.Equal(t, "ENTERPRISE_EMPLOYEE_SEAT_LIMIT_REACHED", infraerrors.Reason(err))
			appErr := infraerrors.FromError(err)
			require.Equal(t, "2", appErr.Metadata["active_employee_count"])
			require.Equal(t, "2", appErr.Metadata["seat_count"])

			if release == "disable" {
				var version int64
				require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT version FROM enterprise_employees WHERE id = $1`, fixture.secondEmployee).Scan(&version))
				require.NoError(t, svc.UpdateEmployee(ctx, fixture.enterpriseID, fixture.secondEmployee, "disabled", nil, version))
			} else {
				require.NoError(t, svc.TerminateEmployee(ctx, fixture.enterpriseID, fixture.secondEmployee))
			}

			created, err := svc.CreateEmployee(ctx, fixture.enterpriseID, fmt.Sprintf("replacement-%d@example.com", time.Now().UnixNano()), "strong-password", nil)
			require.NoError(t, err)
			require.Equal(t, "active", created.Status)
			var activeCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_employees WHERE enterprise_id = $1 AND status = 'active'`, fixture.enterpriseID).Scan(&activeCount))
			require.Equal(t, 2, activeCount)
		})
	}
}

func TestEnterpriseEmployeeSeatLimitRejectsReactivationIntoFullEnterprise(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	setEnterpriseFixtureSeatCount(t, ctx, fixture, 2)
	svc := enterpriseidentity.NewService(integrationDB, &config.Config{}, nil, nil, nil)
	var version int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT version FROM enterprise_employees WHERE id = $1`, fixture.secondEmployee).Scan(&version))
	require.NoError(t, svc.UpdateEmployee(ctx, fixture.enterpriseID, fixture.secondEmployee, "disabled", nil, version))

	_, err := svc.CreateEmployee(ctx, fixture.enterpriseID, fmt.Sprintf("replacement-%d@example.com", time.Now().UnixNano()), "strong-password", nil)
	require.NoError(t, err)
	var disabledVersion int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT version FROM enterprise_employees WHERE id = $1`, fixture.secondEmployee).Scan(&disabledVersion))
	err = svc.UpdateEmployee(ctx, fixture.enterpriseID, fixture.secondEmployee, "active", nil, disabledVersion)
	require.Equal(t, "ENTERPRISE_EMPLOYEE_SEAT_LIMIT_REACHED", infraerrors.Reason(err))
}

func TestEnterpriseEmployeeSeatConcurrentCreateDoesNotOverissue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	setEnterpriseFixtureSeatCount(t, ctx, fixture, 3)
	svc := enterpriseidentity.NewService(integrationDB, &config.Config{}, nil, nil, nil)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := svc.CreateEmployee(ctx, fixture.enterpriseID, fmt.Sprintf("concurrent-%d-%d@example.com", index, time.Now().UnixNano()), "strong-password", nil)
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(errs)

	succeeded, rejected := 0, 0
	for err := range errs {
		if err == nil {
			succeeded++
		} else if infraerrors.Reason(err) == "ENTERPRISE_EMPLOYEE_SEAT_LIMIT_REACHED" {
			rejected++
		} else {
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)
	var activeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_employees WHERE enterprise_id = $1 AND status = 'active'`, fixture.enterpriseID).Scan(&activeCount))
	require.Equal(t, 3, activeCount)
}

func TestEnterpriseEmployeeSeatUsageIsIsolatedByEnterprise(t *testing.T) {
	ctx := context.Background()
	first := seedEnterpriseFixture(t, ctx)
	second := seedEnterpriseFixture(t, ctx)
	setEnterpriseFixtureSeatCount(t, ctx, first, 3)
	setEnterpriseFixtureSeatCount(t, ctx, second, 3)
	svc := enterpriseidentity.NewService(integrationDB, &config.Config{}, nil, nil, nil)

	for _, fixture := range []enterpriseFixture{first, second} {
		created, err := svc.CreateEmployee(ctx, fixture.enterpriseID, fmt.Sprintf("isolated-%d@example.com", fixture.enterpriseID), "strong-password", nil)
		require.NoError(t, err)
		require.Equal(t, "active", created.Status)
	}
}

func TestEnterpriseEmployeeKeyLifecycleDoesNotConsumeOrReleaseEmployeeSeats(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	setEnterpriseFixtureSeatCount(t, ctx, fixture, 1)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	created, err := repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.secondEmployee,
		IdempotencyKey: "key-independent-create", Plaintext: fmt.Sprintf("sk-key-independent-%d", time.Now().UnixNano()), ActorRef: "test:employee",
	})
	require.NoError(t, err, "an existing active employee may create a key even when employee seats are over capacity")
	rotated, err := repo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.secondEmployee, ExpectedAPIKeyID: created.Key.ID,
		IdempotencyKey: "key-independent-rotate", Plaintext: fmt.Sprintf("sk-key-independent-rotated-%d", time.Now().UnixNano()), ActorRef: "test:employee",
	})
	require.NoError(t, err)
	_, err = repo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.secondEmployee, ExpectedAPIKeyID: rotated.Key.ID,
		IdempotencyKey: "key-independent-disable", ActorRef: "test:employee",
	})
	require.NoError(t, err)

	var activeEmployeeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_employees WHERE enterprise_id = $1 AND status = 'active'`, fixture.enterpriseID).Scan(&activeEmployeeCount))
	require.Equal(t, 2, activeEmployeeCount)
}
