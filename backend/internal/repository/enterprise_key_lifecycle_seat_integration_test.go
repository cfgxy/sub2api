//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseEmployeeKeyTerminalStateReleasesSeatForRecreate(t *testing.T) {
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

func TestEnterpriseEmployeeKeySeatLimitRejectsAtCapacityAndSerializesConcurrentEmployees(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	_, err := repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.secondEmployee,
		IdempotencyKey: "seat-limit-at-capacity", Plaintext: "sk-seat-limit-at-capacity", ActorRef: "test:employee",
	})
	require.ErrorIs(t, err, enterprise.ErrEnterpriseSeatLimit)

	_, err = repo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID, ExpectedAPIKeyID: fixture.apiKeyID,
		IdempotencyKey: "seat-limit-release", ActorRef: "test:employee",
	})
	require.NoError(t, err)

	start := make(chan struct{})
	type outcome struct{ err error }
	outcomes := make(chan outcome, 2)
	var group sync.WaitGroup
	for _, employeeID := range []int64{fixture.employeeID, fixture.secondEmployee} {
		employeeID := employeeID
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, createErr := repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
				EnterpriseID: fixture.enterpriseID, EmployeeID: employeeID,
				IdempotencyKey: fmt.Sprintf("seat-concurrent-%d", employeeID),
				Plaintext:      fmt.Sprintf("sk-seat-concurrent-%d-%d", employeeID, time.Now().UnixNano()),
				ActorRef:       "test:employee",
			})
			outcomes <- outcome{err: createErr}
		}()
	}
	close(start)
	group.Wait()
	close(outcomes)

	succeeded, rejected := 0, 0
	for outcome := range outcomes {
		switch {
		case outcome.err == nil:
			succeeded++
		case errors.Is(outcome.err, enterprise.ErrEnterpriseSeatLimit):
			rejected++
		default:
			require.NoError(t, outcome.err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)

	var activeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_key_assignments AS assignment
		JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id
		WHERE assignment.enterprise_id = $1 AND assignment.status = 'active' AND api_key.status = 'active'`, fixture.enterpriseID).Scan(&activeCount))
	require.Equal(t, 1, activeCount)
}

func TestEnterpriseEmployeeKeySeatUsageIsIsolatedByEnterprise(t *testing.T) {
	ctx := context.Background()
	first := seedEnterpriseFixture(t, ctx)
	second := seedEnterpriseFixture(t, ctx)
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE api_keys SET expires_at = statement_timestamp() - INTERVAL '1 second'
		WHERE id IN ($1, $2)`, first.apiKeyID, second.apiKeyID)
	require.NoError(t, err)
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})

	for _, fixture := range []enterpriseFixture{first, second} {
		created, createErr := repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID: fixture.enterpriseID, EmployeeID: fixture.employeeID,
			IdempotencyKey: fmt.Sprintf("seat-isolation-%d", fixture.enterpriseID),
			Plaintext:      fmt.Sprintf("sk-seat-isolation-%d", fixture.enterpriseID),
			ActorRef:       "test:employee",
		})
		require.NoError(t, createErr)
		require.NotZero(t, created.Key.ID)
	}
}
