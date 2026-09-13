//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"testing"
	"time"

	enterprise "github.com/Wei-Shaw/sub2api/internal/enterprise"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func installStandardCredentialCreateGate(t *testing.T, ctx context.Context, credential string, gate int64, applicationName string) func() {
	t.Helper()
	suffix := fmt.Sprintf("zzz_shan153_standard_credential_create_gate_%d", time.Now().UnixNano())
	functionName := pq.QuoteIdentifier(suffix + "_fn")
	triggerName := pq.QuoteIdentifier(suffix + "_trigger")
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger AS $$
		BEGIN
			IF NEW.key = %s AND current_setting('application_name', true) = %s THEN
				PERFORM pg_advisory_xact_lock(%d);
			END IF;
			RETURN NEW;
		END
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER %s
		BEFORE INSERT ON api_keys
		FOR EACH ROW EXECUTE FUNCTION %s();`,
		functionName, pq.QuoteLiteral(credential), pq.QuoteLiteral(applicationName), gate, triggerName, functionName))
	require.NoError(t, err)
	return func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON api_keys", triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	}
}

func TestEnterpriseEmployeeKeyRevocationSerializesWithStandardCredentialCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := seedEnterpriseFixture(t, ctx)
	var credential string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&credential))

	const revocationApplication = "shan153-credential-revocation"
	const createApplication = "shan153-standard-credential-create"
	gate := time.Now().UnixNano()
	revocationDB := openPartitionCleanupDB(t, ctx, "public", revocationApplication)
	createDB := openPartitionCleanupDB(t, ctx, "public", createApplication)
	cleanupTrigger := installStandardCredentialCreateGate(t, ctx, credential, gate, createApplication)
	t.Cleanup(cleanupTrigger)
	releaseGate := holdEmployeeKeyGate(t, ctx, gate)

	createDone := make(chan error, 1)
	go func() {
		_, err := createDB.ExecContext(ctx, `
			INSERT INTO api_keys (user_id, group_id, key, name, status)
			VALUES ($1, $2, $3, 'concurrent standard credential create', 'active')`,
			fixture.enterpriseUserID, fixture.groupID, credential)
		createDone <- err
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, createApplication, createDone)

	revocationDone := make(chan error, 1)
	go func() {
		_, err := enterprise.NewRepository(revocationDB, enterpriseNoopAuthCacheInvalidator{}).DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID:     fixture.enterpriseID,
			EmployeeID:       fixture.employeeID,
			ExpectedAPIKeyID: fixture.apiKeyID,
			IdempotencyKey:   "review-revocation-create-race",
			ActorRef:         "test:employee",
		})
		revocationDone <- err
	}()
	requireEmployeeKeyApplicationWaitingOnLock(t, ctx, revocationApplication, revocationDone)

	releaseGate()
	createErr := <-createDone
	require.Error(t, createErr)
	var pqErr *pq.Error
	require.ErrorAs(t, createErr, &pqErr)
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)
	require.NoError(t, <-revocationDone)

	digest := sha256.Sum256([]byte(credential))
	fingerprint := hex.EncodeToString(digest[:])
	var reservationAPIKeyID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT api_key_id
		FROM api_key_revoked_credential_reservations
		WHERE fingerprint = $1`, fingerprint).Scan(&reservationAPIKeyID))
	require.Equal(t, fixture.apiKeyID, reservationAPIKeyID)

	apiKeyService := service.NewAPIKeyService(
		NewAPIKeyRepository(integrationEntClient, integrationDB),
		NewUserRepository(integrationEntClient, integrationDB),
		nil, nil, nil, nil, nil,
	)
	_, _, err := apiKeyService.ValidateKey(ctx, credential)
	require.Error(t, err)
}

func TestEnterpriseEmployeeKeyRevocationRejectsCredentialReuse(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	var credential string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&credential))

	keyRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	_, err := keyRepo.DisableEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID:     fixture.enterpriseID,
		EmployeeID:       fixture.employeeID,
		ExpectedAPIKeyID: fixture.apiKeyID,
		IdempotencyKey:   "review-revoke-reuse",
		ActorRef:         "test:employee",
	})
	require.NoError(t, err)

	var keyCountBefore int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE user_id = $1`, fixture.enterpriseUserID).Scan(&keyCountBefore))
	apiKeyService := service.NewAPIKeyService(
		NewAPIKeyRepository(integrationEntClient, integrationDB),
		NewUserRepository(integrationEntClient, integrationDB),
		nil, nil, nil, nil, nil,
	)
	_, createErr := apiKeyService.Create(ctx, fixture.enterpriseUserID, service.CreateAPIKeyRequest{
		Name:      "attempted credential reuse",
		CustomKey: &credential,
	})
	if createErr == nil {
		t.Fatal("standard API key creation accepted a revoked credential")
	}

	var keyCountAfter, activeCredentialCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE user_id = $1`, fixture.enterpriseUserID).Scan(&keyCountAfter))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE key = $1 AND deleted_at IS NULL`, credential).Scan(&activeCredentialCount))
	require.Equal(t, keyCountBefore, keyCountAfter)
	require.Zero(t, activeCredentialCount)

	_, _, validateErr := apiKeyService.ValidateKey(ctx, credential)
	if validateErr == nil {
		t.Fatal("revoked credential remained valid after rejected standard creation")
	}
}

func TestEnterpriseCandidateCredentialSearchDoesNotRevealMembership(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	var credential string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT key FROM api_keys WHERE id = $1`, fixture.apiKeyID).Scan(&credential))

	apiKeyRepo := newAPIKeyRepositoryWithSQL(integrationEntClient, integrationDB)
	filters := service.APIKeyListFilters{Search: credential}
	keys, page, err := apiKeyRepo.ListByUserID(
		ctx,
		fixture.enterpriseUserID,
		pagination.PaginationParams{Page: 1, PageSize: 20},
		filters,
	)
	require.NoError(t, err)
	if len(keys) != 0 {
		t.Fatalf("paginated API key search revealed %d candidate rows", len(keys))
	}
	require.NotNil(t, page)
	require.Zero(t, page.Total)

	apiKeyService := service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, nil)
	concurrencyKeys, concurrencyPage, err := apiKeyService.List(
		ctx,
		fixture.enterpriseUserID,
		pagination.PaginationParams{Page: 1, PageSize: 20, SortBy: "current_concurrency"},
		filters,
	)
	require.NoError(t, err)
	if len(concurrencyKeys) != 0 {
		t.Fatalf("current-concurrency API key search revealed %d candidate rows", len(concurrencyKeys))
	}
	require.NotNil(t, concurrencyPage)
	require.Zero(t, concurrencyPage.Total)

	users, userPage, err := NewUserRepository(integrationEntClient, integrationDB).ListWithFilters(
		ctx,
		pagination.PaginationParams{Page: 1, PageSize: 20},
		service.UserListFilters{Search: credential},
	)
	require.NoError(t, err)
	if len(users) != 0 {
		t.Fatalf("user search revealed %d candidate rows", len(users))
	}
	require.NotNil(t, userPage)
	require.Zero(t, userPage.Total)
}

func TestEnterpriseEmployeeKeyProvisionLimitRejectsFreshIdempotencyKeys(t *testing.T) {
	ctx := context.Background()
	fixture := seedEnterpriseFixture(t, ctx)
	keyRepo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	currentID := fixture.apiKeyID

	for attempt := 0; attempt < 10; attempt++ {
		result, err := keyRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
			EnterpriseID:     fixture.enterpriseID,
			EmployeeID:       fixture.employeeID,
			ExpectedAPIKeyID: currentID,
			IdempotencyKey:   fmt.Sprintf("review-provision-%d", attempt),
			Plaintext:        fmt.Sprintf("sk-review-provision-%d-%d", fixture.apiKeyID, attempt),
			ActorRef:         "test:employee",
		})
		require.NoError(t, err)
		currentID = result.Key.ID
	}

	_, err := keyRepo.RotateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID:     fixture.enterpriseID,
		EmployeeID:       fixture.employeeID,
		ExpectedAPIKeyID: currentID,
		IdempotencyKey:   "review-provision-over-limit",
		Plaintext:        fmt.Sprintf("sk-review-provision-over-limit-%d", fixture.apiKeyID),
		ActorRef:         "test:employee",
	})
	require.Error(t, err)
	require.Equal(t, http.StatusTooManyRequests, infraerrors.Code(err))
}
