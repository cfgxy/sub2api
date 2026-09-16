package enterpriseidentity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminKeyStoreForRejectedAudit struct{}

func (adminKeyStoreForRejectedAudit) GetEmployeeCurrentKey(context.Context, int64, int64) (*enterprise.EmployeeKey, error) {
	return nil, enterprise.ErrEmployeeKeyNotFound
}

func (adminKeyStoreForRejectedAudit) CreateEmployeeKey(context.Context, enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error) {
	return nil, enterprise.ErrEmployeeKeyNotFound
}

func (adminKeyStoreForRejectedAudit) DisableEmployeeKey(context.Context, enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error) {
	return nil, enterprise.ErrEmployeeKeyNotFound
}

func (adminKeyStoreForRejectedAudit) RotateEmployeeKey(context.Context, enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error) {
	return nil, enterprise.ErrEmployeeKeyNotFound
}

func (adminKeyStoreForRejectedAudit) ListEnterpriseKeys(context.Context, int64) ([]enterprise.EnterpriseKeySummary, error) {
	return nil, nil
}

func (adminKeyStoreForRejectedAudit) RevokeEnterpriseKey(context.Context, int64, int64, string, string) (*enterprise.EmployeeKeyMutationResult, error) {
	return nil, enterprise.ErrEmployeeKeyNotFound
}

func TestUploadBrandBackgroundRecordsRejectedAuditBeforeParsingFile(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), "brand.background.upload", "enterprise_branding", int64(7), sqlmock.AnyArg(), "enterprise_admin:9").
		WillReturnResult(sqlmock.NewResult(1, 1))
	h := NewHandler(svc, adminKeyStoreForRejectedAudit{}, nil, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/enterprise/admin/brand/background", nil)
	c.Set(claimsContextKey, &Claims{EnterpriseID: 7, PrincipalType: "admin", PrincipalID: 9})
	h.uploadBrandBackground(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRevokeAdminKeyRecordsRejectedAuditForMissingIdempotencyKey(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), "key.revoke", "api_key", int64(31), sqlmock.AnyArg(), "enterprise_admin:9").
		WillReturnResult(sqlmock.NewResult(1, 1))
	h := NewHandler(svc, adminKeyStoreForRejectedAudit{}, nil, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/enterprise/admin/keys/31/revoke", nil)
	c.Params = gin.Params{{Key: "id", Value: "31"}}
	c.Set(claimsContextKey, &Claims{EnterpriseID: 7, PrincipalType: "admin", PrincipalID: 9})
	h.revokeAdminKey(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
