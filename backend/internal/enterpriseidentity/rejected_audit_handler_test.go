package enterpriseidentity

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

type employeeKeyStoreForRejectedAudit struct {
	err    error
	result *enterprise.EmployeeKeyMutationResult
}

func (s employeeKeyStoreForRejectedAudit) GetEmployeeCurrentKey(context.Context, int64, int64) (*enterprise.EmployeeKey, error) {
	return nil, enterprise.ErrEmployeeKeyNotFound
}

func (s employeeKeyStoreForRejectedAudit) CreateEmployeeKey(context.Context, enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error) {
	return s.result, s.err
}

func (s employeeKeyStoreForRejectedAudit) DisableEmployeeKey(context.Context, enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error) {
	return s.result, s.err
}

func (s employeeKeyStoreForRejectedAudit) RotateEmployeeKey(context.Context, enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error) {
	return s.result, s.err
}

type auditPayloadWithReason string

func (want auditPayloadWithReason) Match(value driver.Value) bool {
	encoded, ok := value.([]byte)
	if !ok {
		return false
	}
	var payload map[string]any
	return json.Unmarshal(encoded, &payload) == nil && payload["result"] == "rejected" && payload["reason"] == string(want)
}

type failingEmployeeKeyGeneratorForRejectedAudit struct{}

func (failingEmployeeKeyGeneratorForRejectedAudit) GenerateKey() (string, error) {
	return "", errors.New("key generator unavailable")
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

func TestCreateAdminEmployeeKeyRecordsTargetedRejectedAudit(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(
		int64(7), "key.employee_create", "employee", int64(31), sqlmock.AnyArg(), "enterprise_admin:9",
	).WillReturnResult(sqlmock.NewResult(1, 1))
	h := NewHandler(svc, adminKeyStoreForRejectedAudit{}, employeeKeyGeneratorStub{value: "unused"}, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/enterprise/admin/employees/31/keys", nil)
	c.Params = gin.Params{{Key: "id", Value: "31"}}
	c.Set(claimsContextKey, &Claims{EnterpriseID: 7, PrincipalType: "admin", PrincipalID: 9})
	h.createAdminEmployeeKey(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminEmployeeKeyRejectedAuditReasons(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		err       error
		result    *enterprise.EmployeeKeyMutationResult
		want      auditPayloadWithReason
	}{
		{name: "target unavailable", operation: "create", err: enterprise.ErrEmployeeKeyNotFound, want: employeeKeyAuditReasonTargetUnavailable},
		{name: "already active", operation: "create", err: enterprise.ErrEmployeeKeyAlreadyActive, want: employeeKeyAuditReasonKeyConflict},
		{name: "version conflict", operation: "rotate", err: enterprise.ErrEmployeeKeyVersionConflict, want: employeeKeyAuditReasonKeyConflict},
		{name: "idempotency conflict", operation: "rotate", err: enterprise.ErrEmployeeKeyIdempotency, want: employeeKeyAuditReasonIdempotency},
		{name: "provision limit", operation: "create", err: enterprise.ErrEmployeeKeyProvisionLimit, want: employeeKeyAuditReasonProvisionLimit},
		{name: "service unavailable", operation: "rotate", err: enterprise.ErrEmployeeKeyUnavailable, want: employeeKeyAuditReasonServiceUnavailable},
		{name: "wrapped service error", operation: "create", err: errors.New("database unavailable"), want: employeeKeyAuditReasonServiceUnavailable},
		{name: "no available result", operation: "rotate", result: &enterprise.EmployeeKeyMutationResult{}, want: employeeKeyAuditReasonNoAvailableResult},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, mock := newMockService(t)
			mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(
				int64(7), "key.employee_"+tt.operation, "employee", int64(31), tt.want, "enterprise_admin:9",
			).WillReturnResult(sqlmock.NewResult(1, 1))
			h := NewHandler(svc, employeeKeyStoreForRejectedAudit{err: tt.err, result: tt.result}, employeeKeyGeneratorStub{value: "unused"}, nil)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/enterprise/admin/employees/31/keys", nil)
			c.Request.Header.Set("Idempotency-Key", "request-1")
			c.Params = gin.Params{{Key: "id", Value: "31"}}
			c.Set(claimsContextKey, &Claims{EnterpriseID: 7, PrincipalType: "admin", PrincipalID: 9})
			if tt.operation == "create" {
				h.createAdminEmployeeKey(c)
			} else {
				c.Request.Body = io.NopCloser(strings.NewReader(`{"expected_api_key_id":11}`))
				c.Request.Header.Set("Content-Type", "application/json")
				h.rotateAdminEmployeeKey(c)
			}

			require.GreaterOrEqual(t, recorder.Code, http.StatusBadRequest)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAdminEmployeeKeyGeneratorFailureRecordsServiceUnavailableAudit(t *testing.T) {
	for _, operation := range []string{"create", "rotate"} {
		t.Run(operation, func(t *testing.T) {
			svc, mock := newMockService(t)
			mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(
				int64(7), "key.employee_"+operation, "employee", int64(31), auditPayloadWithReason(employeeKeyAuditReasonServiceUnavailable), "enterprise_admin:9",
			).WillReturnResult(sqlmock.NewResult(1, 1))
			h := NewHandler(svc, employeeKeyStoreForRejectedAudit{}, failingEmployeeKeyGeneratorForRejectedAudit{}, nil)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/enterprise/admin/employees/31/keys", nil)
			c.Request.Header.Set("Idempotency-Key", "request-1")
			c.Params = gin.Params{{Key: "id", Value: "31"}}
			c.Set(claimsContextKey, &Claims{EnterpriseID: 7, PrincipalType: "admin", PrincipalID: 9})
			if operation == "create" {
				h.createAdminEmployeeKey(c)
			} else {
				c.Request.Body = io.NopCloser(strings.NewReader(`{"expected_api_key_id":11}`))
				c.Request.Header.Set("Content-Type", "application/json")
				h.rotateAdminEmployeeKey(c)
			}

			require.Equal(t, http.StatusInternalServerError, recorder.Code)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
