package enterprise

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type employeeKeyAuthCacheInvalidatorNoop struct{}

func (employeeKeyAuthCacheInvalidatorNoop) InvalidateAuthCacheByKey(context.Context, string) {}

func TestMaskEmployeeKeyNeverReturnsPlaintext(t *testing.T) {
	plain := "sk-" + strings.Repeat("a", 64)
	masked := MaskEmployeeKey(plain)
	require.NotEqual(t, plain, masked)
	require.NotContains(t, masked, strings.Repeat("a", 16))
	require.Equal(t, "sk-aaa...aaaa", masked)
	require.Equal(t, "********", MaskEmployeeKey("short"))
}

func TestEmployeeKeyRequestHashBindsOperationAndExpectedKey(t *testing.T) {
	base := EmployeeKeyRequestHash("rotate", 42)
	require.Len(t, base, 64)
	require.NotEqual(t, base, EmployeeKeyRequestHash("disable", 42))
	require.NotEqual(t, base, EmployeeKeyRequestHash("rotate", 43))
}

func TestRevokeEnterpriseKeyReplaysCompletedRevocationWithoutPlaintext(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const (
		enterpriseID = int64(7)
		employeeID   = int64(11)
		apiKeyID     = int64(31)
	)
	requestHash := EmployeeKeyRequestHash(keyOperationDisable, apiKeyID)
	mock.ExpectQuery(`SELECT employee_id, request_hash, result_api_key_id[\s\S]+FROM enterprise_key_lifecycle_idempotency`).
		WithArgs(enterpriseID, keyOperationDisable, "revoke-once", apiKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"employee_id", "request_hash", "result_api_key_id"}).
			AddRow(employeeID, requestHash, apiKeyID))
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT api_key.id, api_key.key, api_key.name, api_key.status,[\s\S]+FROM enterprise_key_assignments AS assignment`).
		WithArgs(enterpriseID, employeeID, apiKeyID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "key", "name", "status", "quota", "quota_used", "group_id", "expires_at",
			"rate_limit_5h", "rate_limit_1d", "rate_limit_7d", "usage_5h", "usage_1d", "usage_7d",
			"window_5h_start", "window_1d_start", "window_7d_start", "created_at", "updated_at",
			"upstream_user_subscription_id", "upstream_group_id", "generation", "assignment_id",
		}).AddRow(
			apiKeyID, ":revoked:31", "employee key", "disabled", 10.0, 2.0, nil, nil,
			0.0, 0.0, 0.0, 0.0, 0.0, 0.0, nil, nil, nil, now, now,
			101, 201, 1, 301,
		))

	repo := NewRepository(db, employeeKeyAuthCacheInvalidatorNoop{})
	result, err := repo.RevokeEnterpriseKey(context.Background(), enterpriseID, apiKeyID, "revoke-once", "enterprise_admin:9")

	require.NoError(t, err)
	require.True(t, result.Replayed)
	require.Empty(t, result.Plaintext)
	require.NotNil(t, result.Key)
	require.Equal(t, apiKeyID, result.Key.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRevokeEnterpriseKeyCompletedReplayRequiresMatchingRequestHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT employee_id, request_hash, result_api_key_id[\s\S]+FROM enterprise_key_lifecycle_idempotency`).
		WithArgs(int64(7), keyOperationDisable, "revoke-once", int64(31)).
		WillReturnRows(sqlmock.NewRows([]string{"employee_id", "request_hash", "result_api_key_id"}).
			AddRow(int64(11), EmployeeKeyRequestHash(keyOperationDisable, 32), int64(31)))

	repo := NewRepository(db, employeeKeyAuthCacheInvalidatorNoop{})
	_, err = repo.RevokeEnterpriseKey(context.Background(), 7, 31, "revoke-once", "enterprise_admin:9")

	require.ErrorIs(t, err, ErrEmployeeKeyIdempotency)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRevokeEnterpriseKeyDoesNotReplayDifferentIdempotencyOrTenant(t *testing.T) {
	for _, tt := range []struct {
		name           string
		enterpriseID   int64
		idempotencyKey string
	}{
		{name: "different idempotency key", enterpriseID: 7, idempotencyKey: "revoke-other"},
		{name: "different enterprise", enterpriseID: 8, idempotencyKey: "revoke-once"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			mock.ExpectQuery(`SELECT employee_id, request_hash, result_api_key_id[\s\S]+FROM enterprise_key_lifecycle_idempotency`).
				WithArgs(tt.enterpriseID, keyOperationDisable, tt.idempotencyKey, int64(31)).
				WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery(`SELECT employee_id FROM enterprise_key_assignments`).
				WithArgs(tt.enterpriseID, int64(31)).
				WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery(`SELECT employee_id, request_hash, result_api_key_id[\s\S]+FROM enterprise_key_lifecycle_idempotency`).
				WithArgs(tt.enterpriseID, keyOperationDisable, tt.idempotencyKey, int64(31)).
				WillReturnError(sql.ErrNoRows)

			repo := NewRepository(db, employeeKeyAuthCacheInvalidatorNoop{})
			_, err = repo.RevokeEnterpriseKey(context.Background(), tt.enterpriseID, 31, tt.idempotencyKey, "enterprise_admin:9")

			require.ErrorIs(t, err, ErrEmployeeKeyNotFound)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestNormalizeEmployeeKeyPresentationUsesGatewayExpiryAndWindowRules(t *testing.T) {
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(-time.Second)
	window5h := now.Add(-5*time.Hour - time.Second)
	window1d := now.Add(-24*time.Hour - time.Second)
	window7d := now.Add(-7*24*time.Hour - time.Second)
	key := EmployeeKey{
		Status: "active", ExpiresAt: &expiresAt,
		Usage5h: 1.25, Usage1d: 2.5, Usage7d: 6.75,
		Window5h: &window5h, Window1d: &window1d, Window7d: &window7d,
	}

	normalizeEmployeeKeyPresentation(&key, now)

	require.Equal(t, "expired", key.Status)
	require.Zero(t, key.Usage5h)
	require.Zero(t, key.Usage1d)
	require.Zero(t, key.Usage7d)
	require.Nil(t, key.Window5h)
	require.Nil(t, key.Window1d)
	require.Nil(t, key.Window7d)
	require.Nil(t, key.Reset5h)
	require.Nil(t, key.Reset1d)
	require.Nil(t, key.Reset7d)
}
