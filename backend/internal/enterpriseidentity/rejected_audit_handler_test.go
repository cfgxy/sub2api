package enterpriseidentity

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type adminKeyStoreForRejectedAudit struct {
	revokeResult *enterprise.EmployeeKeyMutationResult
	revokeErr    error
}

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

func (s adminKeyStoreForRejectedAudit) RevokeEnterpriseKey(context.Context, int64, int64, string, string) (*enterprise.EmployeeKeyMutationResult, error) {
	if s.revokeResult != nil || s.revokeErr != nil {
		return s.revokeResult, s.revokeErr
	}
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

type exactRejectedAuditPayload string

func (want exactRejectedAuditPayload) Match(value driver.Value) bool {
	encoded, ok := value.([]byte)
	if !ok {
		return false
	}
	var payload map[string]any
	if json.Unmarshal(encoded, &payload) != nil || len(payload) != 2 {
		return false
	}
	return payload["result"] == "rejected" && payload["reason"] == string(want)
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
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), "key.revoke", "api_key", int64(31), auditPayloadWithReason("invalid_request"), "enterprise_admin:9").
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

func TestRevokeAdminKeyRecordsSpecificRejectedAuditReasons(t *testing.T) {
	tests := []struct {
		name     string
		store    employeeKeyStore
		entityID any
		want     auditPayloadWithReason
	}{
		{name: "unsupported store", store: employeeKeyStoreForRejectedAudit{}, entityID: nil, want: employeeKeyAuditReasonServiceUnavailable},
		{name: "target unavailable", store: adminKeyStoreForRejectedAudit{}, entityID: int64(31), want: employeeKeyAuditReasonTargetUnavailable},
		{name: "already active", store: adminKeyStoreForRejectedAudit{revokeErr: enterprise.ErrEmployeeKeyAlreadyActive}, entityID: int64(31), want: employeeKeyAuditReasonKeyConflict},
		{name: "version conflict", store: adminKeyStoreForRejectedAudit{revokeErr: enterprise.ErrEmployeeKeyVersionConflict}, entityID: int64(31), want: employeeKeyAuditReasonKeyConflict},
		{name: "idempotency conflict", store: adminKeyStoreForRejectedAudit{revokeErr: enterprise.ErrEmployeeKeyIdempotency}, entityID: int64(31), want: employeeKeyAuditReasonIdempotency},
		{name: "provision limit", store: adminKeyStoreForRejectedAudit{revokeErr: enterprise.ErrEmployeeKeyProvisionLimit}, entityID: int64(31), want: employeeKeyAuditReasonProvisionLimit},
		{name: "service unavailable", store: adminKeyStoreForRejectedAudit{revokeErr: enterprise.ErrEmployeeKeyUnavailable}, entityID: int64(31), want: employeeKeyAuditReasonServiceUnavailable},
		{name: "unknown store error", store: adminKeyStoreForRejectedAudit{revokeErr: errors.New("database unavailable")}, entityID: int64(31), want: employeeKeyAuditReasonServiceUnavailable},
		{name: "no available result", store: adminKeyStoreForRejectedAudit{revokeResult: &enterprise.EmployeeKeyMutationResult{}}, entityID: int64(31), want: employeeKeyAuditReasonNoAvailableResult},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, mock := newMockService(t)
			mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(
				int64(7), "key.revoke", "api_key", tt.entityID, tt.want, "enterprise_admin:9",
			).WillReturnResult(sqlmock.NewResult(1, 1))
			h := NewHandler(svc, tt.store, nil, nil)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/enterprise/admin/keys/31/revoke", nil)
			c.Request.Header.Set("Idempotency-Key", "request-1")
			c.Params = gin.Params{{Key: "id", Value: "31"}}
			c.Set(claimsContextKey, &Claims{EnterpriseID: 7, PrincipalType: "admin", PrincipalID: 9})
			h.revokeAdminKey(c)

			require.GreaterOrEqual(t, recorder.Code, http.StatusBadRequest)
			require.NotContains(t, recorder.Body.String(), `"data":null`)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
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

func TestRegisteredAdminKeyRoutesAuditPreHandlerRejections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	routes := []struct {
		name       string
		path       string
		body       string
		eventType  string
		entityType string
	}{
		{
			name: "create employee key", path: "/api/v1/enterprise/admin/employees/31/keys",
			eventType: "key.employee_create", entityType: "employee",
		},
		{
			name: "rotate employee key", path: "/api/v1/enterprise/admin/employees/31/keys/rotate",
			body: `{"expected_api_key_id":41}`, eventType: "key.employee_rotate", entityType: "employee",
		},
		{
			name: "revoke key", path: "/api/v1/enterprise/admin/keys/31/revoke",
			eventType: "key.revoke", entityType: "api_key",
		},
	}
	rejections := []struct {
		name          string
		principalType string
		role          string
		reason        string
		wantStatus    int
		redisState    string
	}{
		{
			name: "permission denied", principalType: "employee", role: "enterprise_employee",
			reason: "permission_denied", wantStatus: http.StatusForbidden, redisState: "available",
		},
		{
			name: "rate limit exceeded", principalType: "admin", role: "enterprise_admin",
			reason: "rate_limited", wantStatus: http.StatusTooManyRequests, redisState: "exhausted",
		},
		{
			name: "redis fail close", principalType: "admin", role: "enterprise_admin",
			reason: "rate_limited", wantStatus: http.StatusTooManyRequests, redisState: "unavailable",
		},
	}

	for _, route := range routes {
		for _, rejection := range rejections {
			t.Run(route.name+"/"+rejection.name, func(t *testing.T) {
				svc, mock := newMockService(t)
				const (
					enterpriseID = int64(7)
					principalID  = int64(9)
					sessionID    = "session-pre-handler-audit"
				)
				claims := Claims{
					EnterpriseID: enterpriseID, PrincipalType: rejection.principalType, PrincipalID: principalID,
					Role: rejection.role, SessionID: sessionID,
					RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
				}
				mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).
						AddRow(enterpriseID, "Acme", "acme.example.com", principalID, "active"))
				if rejection.principalType == "admin" {
					const email = "admin@example.com"
					const passwordHash = "admin-password-hash"
					claims.AuthVersion = expectedUserTokenVersion(email, passwordHash)
					mock.ExpectQuery("SELECT users.email, users.password_hash, users.status").WithArgs(enterpriseID, principalID).
						WillReturnRows(sqlmock.NewRows([]string{"email", "password_hash", "status"}).
							AddRow(email, passwordHash, "active"))
				} else {
					claims.AuthVersion = 3
					mock.ExpectQuery("SELECT current_email, status, auth_version, must_change_password").WithArgs(enterpriseID, principalID).
						WillReturnRows(sqlmock.NewRows([]string{"current_email", "status", "auth_version", "must_change_password"}).
							AddRow("employee@example.com", "active", claims.AuthVersion, false))
				}
				mock.ExpectQuery("SELECT EXISTS").WithArgs(enterpriseID, sessionID).
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				actorRef := "enterprise_" + rejection.principalType + ":9"
				mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(
					enterpriseID, route.eventType, route.entityType, int64(31), exactRejectedAuditPayload(rejection.reason), actorRef,
				).WillReturnResult(sqlmock.NewResult(1, 1))

				raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
				require.NoError(t, err)
				redisAddress := "127.0.0.1:1"
				if rejection.redisState != "unavailable" {
					redisAddress = miniredis.RunT(t).Addr()
				}
				rdb := redis.NewClient(&redis.Options{
					Addr: redisAddress, DialTimeout: 25 * time.Millisecond,
					ReadTimeout: 25 * time.Millisecond, WriteTimeout: 25 * time.Millisecond,
				})
				t.Cleanup(func() { _ = rdb.Close() })
				if rejection.redisState == "exhausted" {
					rateLimitScope := hashRateLimitScope("7\x00admin\x009")
					err = rdb.Set(
						context.Background(),
						"rate_limit:"+employeeKeyMutationRateLimitKey+":"+rateLimitScope,
						strconv.Itoa(employeeKeyMutationRateLimit),
						employeeKeyMutationRateLimitWindow,
					).Err()
					require.NoError(t, err)
				}
				store := &employeeKeyStoreStub{}
				generator := &countingEmployeeKeyGenerator{value: "must-not-be-generated"}
				h := NewHandler(svc, store, generator, rdb)
				router := gin.New()
				h.RegisterRoutes(router.Group("/api/v1"))

				req := httptest.NewRequest(http.MethodPost, route.path, strings.NewReader(route.body))
				req.Host = "acme.example.com"
				req.Header.Set("Authorization", "Bearer "+raw)
				req.Header.Set("Idempotency-Key", "must-not-reach-handler")
				if route.body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, req)

				require.Equal(t, rejection.wantStatus, recorder.Code)
				require.Empty(t, store.createParams)
				require.Empty(t, store.rotateParams)
				require.Zero(t, store.revokeCalls)
				require.Zero(t, generator.calls)
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestAdminKeyMutationPreHandlerRejectionReasonExcludesHandler429(t *testing.T) {
	require.Equal(t, employeeKeyAuditReasonPermissionDenied,
		adminKeyMutationPreHandlerRejectionReason(http.StatusForbidden, false))
	require.Equal(t, employeeKeyAuditReasonRateLimited,
		adminKeyMutationPreHandlerRejectionReason(http.StatusTooManyRequests, false))
	require.Empty(t,
		adminKeyMutationPreHandlerRejectionReason(http.StatusTooManyRequests, true),
		"handler provision-limit responses must not be recorded as middleware rate limits",
	)
}
