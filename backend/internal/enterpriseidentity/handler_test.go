package enterpriseidentity

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	ippkg "github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutesIncludesEnterpriseAuthSessionsAndAdminAPIs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewHandler(nil, nil, nil, redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))
	h.RegisterRoutes(router.Group("/api/v1"))

	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, expected := range []string{
		"POST /api/v1/enterprise/auth/login",
		"POST /api/v1/enterprise/auth/refresh",
		"POST /api/v1/enterprise/auth/logout",
		"POST /api/v1/enterprise/auth/forgot-password",
		"POST /api/v1/enterprise/auth/reset-password",
		"POST /api/v1/enterprise/password/first-change",
		"GET /api/v1/enterprise/sessions",
		"GET /api/v1/enterprise/sessions/:id",
		"DELETE /api/v1/enterprise/sessions/:id",
		"DELETE /api/v1/enterprise/sessions",
		"GET /api/v1/enterprise/keys/current",
		"POST /api/v1/enterprise/keys",
		"POST /api/v1/enterprise/keys/disable",
		"POST /api/v1/enterprise/keys/rotate",
		"GET /api/v1/enterprise/admin/departments",
		"GET /api/v1/enterprise/admin/departments/:id/deletion-impact",
		"POST /api/v1/enterprise/admin/employees",
		"PUT /api/v1/enterprise/admin/brand",
		"POST /api/v1/enterprise/admin/brand/background",
		"GET /api/v1/enterprise/brand/background",
	} {
		require.True(t, routes[expected], expected)
	}
}

type employeeKeyStoreStub struct {
	current       *enterprise.EmployeeKey
	currentParams [2]int64
	createParams  []enterprise.EmployeeKeyMutationParams
	disableParams []enterprise.EmployeeKeyMutationParams
	rotateParams  []enterprise.EmployeeKeyMutationParams
	createResult  *enterprise.EmployeeKeyMutationResult
	disableResult *enterprise.EmployeeKeyMutationResult
	rotateResult  *enterprise.EmployeeKeyMutationResult
}

func (s *employeeKeyStoreStub) GetEmployeeCurrentKey(_ context.Context, enterpriseID, employeeID int64) (*enterprise.EmployeeKey, error) {
	s.currentParams = [2]int64{enterpriseID, employeeID}
	return s.current, nil
}

func (s *employeeKeyStoreStub) CreateEmployeeKey(_ context.Context, params enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error) {
	s.createParams = append(s.createParams, params)
	return s.createResult, nil
}

func (s *employeeKeyStoreStub) DisableEmployeeKey(_ context.Context, params enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error) {
	s.disableParams = append(s.disableParams, params)
	return s.disableResult, nil
}

func (s *employeeKeyStoreStub) RotateEmployeeKey(_ context.Context, params enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error) {
	s.rotateParams = append(s.rotateParams, params)
	return s.rotateResult, nil
}

type employeeKeyGeneratorStub struct {
	value string
}

func (s employeeKeyGeneratorStub) GenerateKey() (string, error) {
	return s.value, nil
}

type countingEmployeeKeyGenerator struct {
	value string
	calls int
}

func (s *countingEmployeeKeyGenerator) GenerateKey() (string, error) {
	s.calls++
	return s.value, nil
}

func TestEmployeeKeyHandlersUseAuthenticatedEmployeeClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &employeeKeyStoreStub{
		current: &enterprise.EmployeeKey{ID: 67, MaskedKey: "sk-tes...0001", Status: "active"},
		createResult: &enterprise.EmployeeKeyMutationResult{
			Key:       &enterprise.EmployeeKey{ID: 68, MaskedKey: "sk-tes...0002", Status: "active"},
			Plaintext: "test-generated-key",
		},
		rotateResult: &enterprise.EmployeeKeyMutationResult{
			Key:       &enterprise.EmployeeKey{ID: 69, MaskedKey: "sk-tes...0003", Status: "active"},
			Plaintext: "must-not-return-on-replay",
			Replayed:  true,
		},
	}
	h := NewHandler(nil, store, employeeKeyGeneratorStub{value: "test-generated-key"}, nil)
	claims := &Claims{EnterpriseID: 11, PrincipalType: "employee", PrincipalID: 22, Role: "enterprise_employee", SessionID: "session-1"}

	currentRecorder := httptest.NewRecorder()
	currentContext, _ := gin.CreateTestContext(currentRecorder)
	currentContext.Request = httptest.NewRequest(http.MethodGet, "/enterprise/keys/current", nil)
	currentContext.Set(claimsContextKey, claims)
	h.getCurrentKey(currentContext)
	require.Equal(t, http.StatusOK, currentRecorder.Code)
	require.Equal(t, [2]int64{11, 22}, store.currentParams)
	require.Contains(t, currentRecorder.Body.String(), "sk-tes...0001")
	require.NotContains(t, currentRecorder.Body.String(), "test-generated-key")

	createRecorder := httptest.NewRecorder()
	createContext, _ := gin.CreateTestContext(createRecorder)
	createContext.Request = httptest.NewRequest(http.MethodPost, "/enterprise/keys", nil)
	createContext.Request.Header.Set("Idempotency-Key", "create-once")
	createContext.Set(claimsContextKey, claims)
	h.createKey(createContext)
	require.Equal(t, http.StatusCreated, createRecorder.Code)
	require.Equal(t, "no-store", createRecorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", createRecorder.Header().Get("Pragma"))
	require.Len(t, store.createParams, 1)
	require.Equal(t, int64(11), store.createParams[0].EnterpriseID)
	require.Equal(t, int64(22), store.createParams[0].EmployeeID)
	require.Equal(t, "enterprise_employee:22", store.createParams[0].ActorRef)
	require.Contains(t, createRecorder.Body.String(), "test-generated-key")

	rotateRecorder := httptest.NewRecorder()
	rotateContext, _ := gin.CreateTestContext(rotateRecorder)
	rotateContext.Request = httptest.NewRequest(http.MethodPost, "/enterprise/keys/rotate", strings.NewReader(`{"expected_api_key_id":67}`))
	rotateContext.Request.Header.Set("Content-Type", "application/json")
	rotateContext.Request.Header.Set("Idempotency-Key", "rotate-once")
	rotateContext.Set(claimsContextKey, claims)
	h.rotateKey(rotateContext)
	require.Equal(t, http.StatusOK, rotateRecorder.Code)
	require.Len(t, store.rotateParams, 1)
	require.Equal(t, int64(11), store.rotateParams[0].EnterpriseID)
	require.Equal(t, int64(22), store.rotateParams[0].EmployeeID)
	require.Equal(t, int64(67), store.rotateParams[0].ExpectedAPIKeyID)
	require.NotContains(t, rotateRecorder.Body.String(), "must-not-return-on-replay")
}

func TestEmployeeKeyHandlersRejectUntrustedOrIncompleteRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &employeeKeyStoreStub{}
	h := NewHandler(nil, store, employeeKeyGeneratorStub{value: "test-generated-key"}, nil)

	missingIdempotencyRecorder := httptest.NewRecorder()
	missingIdempotencyContext, _ := gin.CreateTestContext(missingIdempotencyRecorder)
	missingIdempotencyContext.Request = httptest.NewRequest(http.MethodPost, "/enterprise/keys", nil)
	missingIdempotencyContext.Set(claimsContextKey, &Claims{EnterpriseID: 11, PrincipalType: "employee", PrincipalID: 22, Role: "enterprise_employee", SessionID: "session-1"})
	h.createKey(missingIdempotencyContext)
	require.Equal(t, http.StatusBadRequest, missingIdempotencyRecorder.Code)
	require.Empty(t, store.createParams)

	adminRecorder := httptest.NewRecorder()
	adminContext, _ := gin.CreateTestContext(adminRecorder)
	adminContext.Request = httptest.NewRequest(http.MethodGet, "/enterprise/keys/current", nil)
	adminContext.Set(claimsContextKey, &Claims{EnterpriseID: 11, PrincipalType: "admin", PrincipalID: 22, Role: "enterprise_admin", SessionID: "session-2"})
	h.getCurrentKey(adminContext)
	require.Equal(t, http.StatusForbidden, adminRecorder.Code)
	require.Equal(t, [2]int64{}, store.currentParams)
}

func TestEmployeeKeyMutationRateLimitFailsClosedBeforeGeneratorAndRepository(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		path string
		body string
	}{
		{name: "create", path: "/keys"},
		{name: "disable", path: "/keys/disable", body: `{"expected_api_key_id":67}`},
		{name: "rotate", path: "/keys/rotate", body: `{"expected_api_key_id":67}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			rdb := redis.NewClient(&redis.Options{
				Addr:         "127.0.0.1:1",
				DialTimeout:  25 * time.Millisecond,
				ReadTimeout:  25 * time.Millisecond,
				WriteTimeout: 25 * time.Millisecond,
			})
			t.Cleanup(func() { _ = rdb.Close() })
			store := &employeeKeyStoreStub{}
			generator := &countingEmployeeKeyGenerator{value: "must-not-be-generated"}
			h := NewHandler(nil, store, generator, rdb)
			router := gin.New()
			root := router.Group("")
			root.Use(func(c *gin.Context) {
				c.Set(claimsContextKey, &Claims{
					EnterpriseID: 11, PrincipalType: "employee", PrincipalID: 22,
					Role: "enterprise_employee", SessionID: "session-1",
				})
				c.Next()
			})
			root.POST("/keys", h.employeeKeyMutationRateLimit(), h.createKey)
			root.POST("/keys/disable", h.employeeKeyMutationRateLimit(), h.disableKey)
			root.POST("/keys/rotate", h.employeeKeyMutationRateLimit(), h.rotateKey)

			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "must-not-reach-repository")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusTooManyRequests, recorder.Code)
			require.Empty(t, store.createParams)
			require.Empty(t, store.disableParams)
			require.Empty(t, store.rotateParams)
			require.Zero(t, generator.calls)
		})
	}
}

func TestEmployeeKeyRotationPlaintextResponseIsNeverStoredByHTTPCaches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &employeeKeyStoreStub{
		rotateResult: &enterprise.EmployeeKeyMutationResult{
			Key:       &enterprise.EmployeeKey{ID: 69, MaskedKey: "sk-tes...0003", Status: "active"},
			Plaintext: "test-rotation-key",
		},
	}
	h := NewHandler(nil, store, employeeKeyGeneratorStub{value: "test-rotation-key"}, nil)
	claims := &Claims{EnterpriseID: 11, PrincipalType: "employee", PrincipalID: 22, Role: "enterprise_employee", SessionID: "session-1"}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/enterprise/keys/rotate", strings.NewReader(`{"expected_api_key_id":67}`))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Request.Header.Set("Idempotency-Key", "rotate-once")
	context.Set(claimsContextKey, claims)

	h.rotateKey(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", recorder.Header().Get("Pragma"))
}

func TestEnterprisePublicAuthRateLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name  string
		path  string
		limit int
	}{
		{name: "login", path: "/api/v1/enterprise/auth/login", limit: 20},
		{name: "refresh", path: "/api/v1/enterprise/auth/refresh", limit: 30},
		{name: "forgot password", path: "/api/v1/enterprise/auth/forgot-password", limit: 5},
		{name: "reset password", path: "/api/v1/enterprise/auth/reset-password", limit: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			router := gin.New()
			newEnterpriseRateLimitTestHandler(rdb).RegisterRoutes(router.Group("/api/v1"))

			for i := 0; i < tt.limit; i++ {
				recorder := performEnterpriseAuthRequest(router, tt.path, "acme.example.com", "198.51.100.10")
				require.Equal(t, http.StatusBadRequest, recorder.Code, "allowed request must reach the handler")
			}

			recorder := performEnterpriseAuthRequest(router, tt.path, "acme.example.com", "198.51.100.10")
			require.Equal(t, http.StatusTooManyRequests, recorder.Code)
			require.Equal(t, "60", recorder.Header().Get("Retry-After"))
			require.Contains(t, recorder.Body.String(), "rate limit exceeded")
		})
	}
}

func TestEnterprisePublicAuthRateLimitFailsClosedWhenRedisUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  25 * time.Millisecond,
		ReadTimeout:  25 * time.Millisecond,
		WriteTimeout: 25 * time.Millisecond,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	router := gin.New()
	newEnterpriseRateLimitTestHandler(rdb).RegisterRoutes(router.Group("/api/v1"))

	for _, path := range []string{
		"/api/v1/enterprise/auth/login",
		"/api/v1/enterprise/auth/refresh",
		"/api/v1/enterprise/auth/forgot-password",
		"/api/v1/enterprise/auth/reset-password",
	} {
		recorder := performEnterpriseAuthRequest(router, path, "acme.example.com", "198.51.100.10")
		require.Equal(t, http.StatusTooManyRequests, recorder.Code, path)
		require.Contains(t, recorder.Body.String(), "rate limit exceeded", path)
	}
}

func TestEnterprisePublicAuthRateLimitIsolatesEnterpriseAndClientWithoutPIIKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := gin.New()
	newEnterpriseRateLimitTestHandler(rdb).RegisterRoutes(router.Group("/api/v1"))

	for i := 0; i < 20; i++ {
		require.Equal(t, http.StatusBadRequest, performEnterpriseAuthRequest(
			router, "/api/v1/enterprise/auth/login", "acme.example.com", "198.51.100.10",
		).Code)
	}
	require.Equal(t, http.StatusTooManyRequests, performEnterpriseAuthRequest(
		router, "/api/v1/enterprise/auth/login", "acme.example.com", "198.51.100.10",
	).Code)
	require.Equal(t, http.StatusBadRequest, performEnterpriseAuthRequest(
		router, "/api/v1/enterprise/auth/login", "other.example.com", "198.51.100.10",
	).Code)
	require.Equal(t, http.StatusBadRequest, performEnterpriseAuthRequest(
		router, "/api/v1/enterprise/auth/login", "acme.example.com", "198.51.100.11",
	).Code)

	keys := server.Keys()
	require.Len(t, keys, 3)
	for _, key := range keys {
		require.True(t, strings.HasPrefix(key, "rate_limit:enterprise-auth-login"), key)
		require.NotContains(t, key, "acme.example.com")
		require.NotContains(t, key, "other.example.com")
		require.NotContains(t, key, "198.51.100.")
	}
}

func TestEnterprisePublicAuthRateLimitRejectsUnknownHostChurnBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h := newEnterpriseRateLimitTestHandler(rdb)
	handlerCalls := 0
	router := gin.New()
	handlers := append(h.publicAuthRateLimits("enterprise-auth-test", 1), func(c *gin.Context) {
		handlerCalls++
		c.Status(http.StatusNoContent)
	})
	router.POST("/test", handlers...)

	for i := 0; i < 10; i++ {
		recorder := performEnterpriseAuthRequest(router, "/test", fmt.Sprintf("unknown-%d.invalid", i), "198.51.100.10")
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
	}
	require.Zero(t, handlerCalls)
	require.Empty(t, server.Keys())
}

func TestEnterprisePublicAuthRateLimitIgnoresUntrustedForwardedIPAndNormalizedHostChurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h := newEnterpriseRateLimitTestHandler(rdb)

	handlerCalls := 0
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies([]string{"10.0.0.0/8"}))
	router.Use(func(c *gin.Context) {
		ippkg.SetForwardedIPSettings(c, true, nil)
		c.Next()
	})
	handlers := append(h.publicAuthRateLimits("enterprise-auth-test", 1), func(c *gin.Context) {
		handlerCalls++
		c.Status(http.StatusNoContent)
	})
	router.POST("/test", handlers...)

	hosts := []string{"acme.example.com", "ACME.EXAMPLE.COM", "acme.example.com.", "acme.example.com:443"}
	for i, host := range hosts {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("{"))
		req.Host = host
		req.RemoteAddr = "198.51.100.10:1234"
		req.Header.Set("CF-Connecting-IP", fmt.Sprintf("203.0.113.%d", i+1))
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if i == 0 {
			require.Equal(t, http.StatusNoContent, recorder.Code)
		} else {
			require.Equal(t, http.StatusTooManyRequests, recorder.Code)
		}
	}
	require.Equal(t, 1, handlerCalls)
}

func TestEnterprisePublicAuthRateLimitDoesNotShareTransportPeerAcrossTrustedClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h := newEnterpriseRateLimitTestHandler(rdb)

	peerKey := "rate_limit:enterprise-auth-test-peer:" + hashRateLimitScope("10.0.0.2")
	server.Set(peerKey, "1000")
	server.SetTTL(peerKey, time.Minute)

	handlerCalls := 0
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies([]string{"10.0.0.0/8"}))
	handlers := append(h.publicAuthRateLimits("enterprise-auth-test", 1), func(c *gin.Context) {
		handlerCalls++
		c.Status(http.StatusNoContent)
	})
	router.POST("/test", handlers...)

	request := func(host, clientIP string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("{"))
		req.Host = host
		req.RemoteAddr = "10.0.0.2:1234"
		req.Header.Set("X-Forwarded-For", clientIP)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, request("acme.example.com", "198.51.100.10").Code)
	require.Equal(t, http.StatusNoContent, request("other.example.com", "198.51.100.10").Code)
	require.Equal(t, http.StatusNoContent, request("acme.example.com", "198.51.100.11").Code)
	require.Equal(t, http.StatusTooManyRequests, request("acme.example.com", "198.51.100.10").Code)
	require.Equal(t, 3, handlerCalls)
}

func TestEnterprisePublicAuthRateLimitSkipsHandlerWhenRedisUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  25 * time.Millisecond,
		ReadTimeout:  25 * time.Millisecond,
		WriteTimeout: 25 * time.Millisecond,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	h := newEnterpriseRateLimitTestHandler(rdb)

	handlerCalls := 0
	router := gin.New()
	handlers := append(h.publicAuthRateLimits("enterprise-auth-test", 1), func(c *gin.Context) {
		handlerCalls++
		c.Status(http.StatusNoContent)
	})
	router.POST("/test", handlers...)

	recorder := performEnterpriseAuthRequest(router, "/test", "acme.example.com", "198.51.100.10")
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Contains(t, recorder.Body.String(), "rate limit exceeded")
	require.Zero(t, handlerCalls)
}

func performEnterpriseAuthRequest(router http.Handler, path, host, clientIP string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{"))
	req.Host = host
	req.RemoteAddr = clientIP + ":1234"
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func newEnterpriseRateLimitTestHandler(rdb *redis.Client) *Handler {
	h := NewHandler(nil, nil, nil, rdb)
	h.resolveRateLimitEnterprise = func(_ context.Context, host string) (int64, error) {
		switch host {
		case "acme.example.com":
			return 1, nil
		case "other.example.com":
			return 2, nil
		default:
			return 0, errWrongHost
		}
	}
	return h
}
