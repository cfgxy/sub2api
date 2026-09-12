package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/enterpriseidentity"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const enterpriseAllocationJWTSecret = "enterprise-allocation-route-test-secret"

type enterpriseAllocationRouteStore struct {
	setParams    enterprise.SetAllocationParams
	summaryQuery enterprise.AllocationUsageSummaryQuery
	setErr       error
}

func (s *enterpriseAllocationRouteStore) SetAllocation(_ context.Context, params enterprise.SetAllocationParams) (*enterprise.Allocation, error) {
	s.setParams = params
	if s.setErr != nil {
		return nil, s.setErr
	}
	return &enterprise.Allocation{ID: 7, EnterpriseID: params.EnterpriseID, SubscriptionID: params.SubscriptionID, EmployeeID: params.EmployeeID, WindowType: params.WindowType, WindowAnchor: params.WindowAnchor, Credit: params.Credit, Version: 2}, nil
}

func (s *enterpriseAllocationRouteStore) GetAllocationUsageSummary(_ context.Context, query enterprise.AllocationUsageSummaryQuery) (*enterprise.AllocationUsageSummary, error) {
	s.summaryQuery = query
	limit := "4.00000000"
	return &enterprise.AllocationUsageSummary{
		ConfiguredCredit: "5.00000000", UsageCredit: "2.00000000", RemainingCredit: "3.00000000",
		OverageCredit: "0.00000000", AllocatedTotal: "6.00000000", AuthoritativeLimit: &limit,
		OverallocatedBy: "2.00000000", Warning: enterprise.AllocationWarningOverallocated,
	}, nil
}

func TestEnterpriseAllocationRoutesUseAuthenticatedSubjectAndProductionPaths(t *testing.T) {
	store := &enterpriseAllocationRouteStore{}
	router := newEnterpriseAllocationRouteTestRouter(store)

	unauthorized := performAllocationRouteRequest(router, http.MethodPut, "/api/v1/enterprise/subscriptions/11/allocations/22", validAllocationBody(), false)
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)

	success := performAllocationRouteRequest(router, http.MethodPut, "/api/v1/enterprise/subscriptions/11/allocations/22", validAllocationBody(), true)
	require.Equal(t, http.StatusOK, success.Code)
	require.Equal(t, int64(42), store.setParams.RequesterUserID)
	require.Equal(t, int64(9), store.setParams.EnterpriseID)
	require.Equal(t, int64(11), store.setParams.SubscriptionID)
	require.Equal(t, int64(22), store.setParams.EmployeeID)

	summary := performAllocationRouteRequest(router, http.MethodGet, "/api/v1/enterprise/subscriptions/11/allocations/22?enterprise_id=9&window_type=day&window_anchor=2026-09-08T00:00:00Z", nil, true)
	require.Equal(t, http.StatusOK, summary.Code)
	require.Equal(t, int64(42), store.summaryQuery.RequesterUserID)
	require.Equal(t, enterprise.WindowTypeDay, store.summaryQuery.WindowType)
	var response struct {
		Data enterprise.AllocationUsageSummary `json:"data"`
	}
	require.NoError(t, json.Unmarshal(summary.Body.Bytes(), &response))
	require.Equal(t, "2.00000000", response.Data.UsageCredit)
	require.Equal(t, "6.00000000", response.Data.AllocatedTotal)
	require.Equal(t, "4.00000000", *response.Data.AuthoritativeLimit)
	require.Equal(t, "2.00000000", response.Data.OverallocatedBy)
	require.Equal(t, enterprise.AllocationWarningOverallocated, response.Data.Warning)

	invalidSummary := performAllocationRouteRequest(router, http.MethodGet, "/api/v1/enterprise/subscriptions/11/allocations/22?enterprise_id=9&window_type=1d&window_anchor=2026-09-08T10:00:00Z", nil, true)
	require.Equal(t, http.StatusBadRequest, invalidSummary.Code)
}

func TestEnterpriseAllocationRoutesMapScopeVersionAndUnsafeReason(t *testing.T) {
	for name, tc := range map[string]struct {
		storeErr error
		body     []byte
		want     int
	}{
		"cross enterprise": {enterprise.ErrEnterpriseAccessDenied, validAllocationBody(), http.StatusNotFound},
		"version conflict": {enterprise.ErrAllocationVersionConflict, validAllocationBody(), http.StatusConflict},
		"secret reason":    {nil, []byte(`{"enterprise_id":9,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"5","reason":"token=secret"}`), http.StatusBadRequest},
		"long reason":      {nil, []byte(`{"enterprise_id":9,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"5","reason":"` + string(bytes.Repeat([]byte("a"), enterprise.MaxAllocationReasonLength+1)) + `"}`), http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			store := &enterpriseAllocationRouteStore{setErr: tc.storeErr}
			router := newEnterpriseAllocationRouteTestRouter(store)
			response := performAllocationRouteRequest(router, http.MethodPut, "/api/v1/enterprise/subscriptions/11/allocations/22", tc.body, true)
			require.Equal(t, tc.want, response.Code)
		})
	}
}

func TestEnterpriseAllocationRoutesAcceptEnterpriseAdminOrStandardJWT(t *testing.T) {
	t.Run("enterprise administrator can put and get on matching host", func(t *testing.T) {
		store := &enterpriseAllocationRouteStore{}
		router, mock, token := newEnterpriseJWTAllocationRouteTestRouter(t, store, enterpriseidentity.Claims{
			EnterpriseID: 9, PrincipalType: "admin", PrincipalID: 42, Role: "enterprise_admin",
			SessionID: "11111111-1111-1111-1111-111111111111",
		})

		expectEnterpriseAdminAuthentication(mock, "acme.example.com", 9, 42, "11111111-1111-1111-1111-111111111111")
		put := performAllocationJWTRequest(router, http.MethodPut, "/api/v1/enterprise/subscriptions/11/allocations/22", validAllocationBody(), "acme.example.com", token)
		require.Equal(t, http.StatusOK, put.Code)
		require.Equal(t, int64(42), store.setParams.RequesterUserID)

		expectEnterpriseAdminAuthentication(mock, "acme.example.com", 9, 42, "11111111-1111-1111-1111-111111111111")
		get := performAllocationJWTRequest(router, http.MethodGet, "/api/v1/enterprise/subscriptions/11/allocations/22?enterprise_id=9&window_type=day&window_anchor=2026-09-08T00:00:00Z", nil, "acme.example.com", token)
		require.Equal(t, http.StatusOK, get.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("enterprise employee is forbidden", func(t *testing.T) {
		store := &enterpriseAllocationRouteStore{}
		router, mock, token := newEnterpriseJWTAllocationRouteTestRouter(t, store, enterpriseidentity.Claims{
			EnterpriseID: 9, PrincipalType: "employee", PrincipalID: 22, AuthVersion: 3, Role: "enterprise_employee",
			SessionID: "22222222-2222-2222-2222-222222222222",
		})
		expectEnterpriseEmployeeAuthentication(mock, "acme.example.com", 9, 22, 3, "22222222-2222-2222-2222-222222222222")

		response := performAllocationJWTRequest(router, http.MethodPut, "/api/v1/enterprise/subscriptions/11/allocations/22", validAllocationBody(), "acme.example.com", token)
		require.Equal(t, http.StatusForbidden, response.Code)
		require.Zero(t, store.setParams.RequesterUserID)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("enterprise token is rejected on another host", func(t *testing.T) {
		store := &enterpriseAllocationRouteStore{}
		router, mock, token := newEnterpriseJWTAllocationRouteTestRouter(t, store, enterpriseidentity.Claims{
			EnterpriseID: 9, PrincipalType: "admin", PrincipalID: 42, Role: "enterprise_admin",
			SessionID: "33333333-3333-3333-3333-333333333333",
		})
		mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("other.example.com").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(10, "Other", "other.example.com", 42, "active"))

		response := performAllocationJWTRequest(router, http.MethodGet, "/api/v1/enterprise/subscriptions/11/allocations/22?enterprise_id=9&window_type=day&window_anchor=2026-09-08T00:00:00Z", nil, "other.example.com", token)
		require.Equal(t, http.StatusUnauthorized, response.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("oversized enterprise-shaped token is rejected before database authentication", func(t *testing.T) {
		store := &enterpriseAllocationRouteStore{}
		router, mock, token := newEnterpriseJWTAllocationRouteTestRouter(t, store, enterpriseidentity.Claims{
			EnterpriseID: 9, PrincipalType: "admin", PrincipalID: 42, Role: "enterprise_admin",
			SessionID:        "55555555-5555-5555-5555-555555555555",
			RegisteredClaims: jwt.RegisteredClaims{Subject: strings.Repeat("a", service.MaxTokenLength)},
		})
		require.Greater(t, len(token), service.MaxTokenLength)

		response := performAllocationJWTRequest(router, http.MethodGet, "/api/v1/enterprise/subscriptions/11/allocations/22?enterprise_id=9&window_type=day&window_anchor=2026-09-08T00:00:00Z", nil, "acme.example.com", token)
		require.Equal(t, http.StatusUnauthorized, response.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	for name, tc := range map[string]struct {
		method string
		target string
		body   []byte
	}{
		"put body enterprise mismatch": {
			method: http.MethodPut,
			target: "/api/v1/enterprise/subscriptions/11/allocations/22",
			body:   []byte(`{"enterprise_id":10,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"5","expected_version":1,"reason":"rebalance"}`),
		},
		"get query enterprise mismatch": {
			method: http.MethodGet,
			target: "/api/v1/enterprise/subscriptions/11/allocations/22?enterprise_id=10&window_type=day&window_anchor=2026-09-08T00:00:00Z",
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := &enterpriseAllocationRouteStore{}
			router, mock, token := newEnterpriseJWTAllocationRouteTestRouter(t, store, enterpriseidentity.Claims{
				EnterpriseID: 9, PrincipalType: "admin", PrincipalID: 42, Role: "enterprise_admin",
				SessionID: "44444444-4444-4444-4444-444444444444",
			})
			expectEnterpriseAdminAuthentication(mock, "acme.example.com", 9, 42, "44444444-4444-4444-4444-444444444444")

			response := performAllocationJWTRequest(router, tc.method, tc.target, tc.body, "acme.example.com", token)
			require.Equal(t, http.StatusNotFound, response.Code)
			require.Zero(t, store.setParams.RequesterUserID)
			require.Zero(t, store.summaryQuery.RequesterUserID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}

	t.Run("standard user jwt remains accepted", func(t *testing.T) {
		store := &enterpriseAllocationRouteStore{}
		router, mock, _ := newEnterpriseJWTAllocationRouteTestRouter(t, store, enterpriseidentity.Claims{})

		put := performAllocationJWTRequest(router, http.MethodPut, "/api/v1/enterprise/subscriptions/11/allocations/22", validAllocationBody(), "panel.example.com", "route-test")
		require.Equal(t, http.StatusOK, put.Code)
		require.Equal(t, int64(42), store.setParams.RequesterUserID)

		get := performAllocationJWTRequest(router, http.MethodGet, "/api/v1/enterprise/subscriptions/11/allocations/22?enterprise_id=9&window_type=day&window_anchor=2026-09-08T00:00:00Z", nil, "panel.example.com", "route-test")
		require.Equal(t, http.StatusOK, get.Code)
		require.Equal(t, int64(42), store.summaryQuery.RequesterUserID)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func newEnterpriseJWTAllocationRouteTestRouter(t *testing.T, store handler.EnterpriseAllocationStore, claims enterpriseidentity.Claims) (*gin.Engine, sqlmock.Sqlmock, string) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	identityService := enterpriseidentity.NewService(db, &config.Config{JWT: config.JWTConfig{Secret: enterpriseAllocationJWTSecret}}, nil, nil)
	identityHandler := enterpriseidentity.NewHandler(identityService, nil)

	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer route-test" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), "user")
		c.Set(middleware.ContextKeyAuthEmail, "user@example.com")
		c.Set(middleware.ContextKeySessionID, "standard-session")
		c.Next()
	})
	auditLog := middleware.AuditLogMiddleware(func(c *gin.Context) {
		if claims.PrincipalType != "" {
			subject, ok := middleware.GetAuthSubjectFromContext(c)
			require.True(t, ok)
			require.Equal(t, claims.PrincipalID, subject.UserID)
			require.Equal(t, claims.Role, c.GetString(string(middleware.ContextKeyUserRole)))
			require.Equal(t, claims.SessionID, c.GetString(middleware.ContextKeySessionID))
			require.NotEmpty(t, c.GetString(middleware.ContextKeyAuthEmail))
		}
		c.Next()
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterUserRoutes(router.Group("/api/v1"), &handler.Handlers{
		Enterprise:           identityHandler,
		EnterpriseAllocation: handler.NewEnterpriseAllocationHandlerWithStore(store),
	}, jwtAuth, auditLog, nil, &middleware.PanelRateLimiter{})

	if claims.PrincipalType == "" {
		return router, mock, ""
	}
	if claims.AuthVersion == 0 {
		claims.AuthVersion = service.ResolveTokenVersion("admin@example.com", "admin-password-hash", 0)
	}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(enterpriseAllocationJWTSecret))
	require.NoError(t, err)
	return router, mock, token
}

func expectEnterpriseAdminAuthentication(mock sqlmock.Sqlmock, host string, enterpriseID, userID int64, sessionID string) {
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs(host).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(enterpriseID, "Acme", host, userID, "active"))
	mock.ExpectQuery("SELECT users.email, users.password_hash, users.status").WithArgs(enterpriseID, userID).
		WillReturnRows(sqlmock.NewRows([]string{"email", "password_hash", "status"}).AddRow("admin@example.com", "admin-password-hash", "active"))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(enterpriseID, sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
}

func expectEnterpriseEmployeeAuthentication(mock sqlmock.Sqlmock, host string, enterpriseID, employeeID, authVersion int64, sessionID string) {
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs(host).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(enterpriseID, "Acme", host, 42, "active"))
	mock.ExpectQuery("SELECT current_email, status, auth_version, must_change_password").WithArgs(enterpriseID, employeeID).
		WillReturnRows(sqlmock.NewRows([]string{"current_email", "status", "auth_version", "must_change_password"}).AddRow("employee@example.com", "active", authVersion, false))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(enterpriseID, sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
}

func performAllocationJWTRequest(router http.Handler, method, target string, body []byte, host, token string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Host = host
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, request)
	return recorder
}

func newEnterpriseAllocationRouteTestRouter(store handler.EnterpriseAllocationStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	authenticated := v1.Group("")
	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer route-test" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	})
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	RegisterEnterpriseAllocationRoutes(authenticated, handler.NewEnterpriseAllocationHandlerWithStore(store))
	return router
}

func performAllocationRouteRequest(router http.Handler, method, target string, body []byte, authenticated bool) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authenticated {
		request.Header.Set("Authorization", "Bearer route-test")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func validAllocationBody() []byte {
	return []byte(`{"enterprise_id":9,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"5","expected_version":1,"reason":"rebalance"}`)
}
