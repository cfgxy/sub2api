package enterpriseidentity

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	ippkg "github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutesIncludesEnterpriseAuthSessionsAndAdminAPIs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewHandler(nil, redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))
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
		"GET /api/v1/enterprise/admin/departments",
		"POST /api/v1/enterprise/admin/employees",
		"PUT /api/v1/enterprise/admin/brand",
		"POST /api/v1/enterprise/admin/brand/background",
		"GET /api/v1/enterprise/brand/background",
	} {
		require.True(t, routes[expected], expected)
	}
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
			NewHandler(nil, rdb).RegisterRoutes(router.Group("/api/v1"))

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
	NewHandler(nil, rdb).RegisterRoutes(router.Group("/api/v1"))

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
	NewHandler(nil, rdb).RegisterRoutes(router.Group("/api/v1"))

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
	require.Len(t, keys, 7)
	for _, key := range keys {
		require.True(t, strings.HasPrefix(key, "rate_limit:enterprise-auth-login"), key)
		require.NotContains(t, key, "acme.example.com")
		require.NotContains(t, key, "other.example.com")
		require.NotContains(t, key, "198.51.100.")
	}
}

func TestEnterprisePublicAuthRateLimitBoundsUnknownHostChurnAndSkipsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h := NewHandler(nil, rdb)
	handlerCalls := 0
	router := gin.New()
	handlers := append(h.publicAuthRateLimits("enterprise-auth-test", 1), func(c *gin.Context) {
		handlerCalls++
		c.Status(http.StatusNoContent)
	})
	router.POST("/test", handlers...)

	for i := 0; i < enterpriseAuthClientBurstMultiplier; i++ {
		recorder := performEnterpriseAuthRequest(router, "/test", fmt.Sprintf("unknown-%d.invalid", i), "198.51.100.10")
		require.Equal(t, http.StatusNoContent, recorder.Code)
	}
	require.Equal(t, enterpriseAuthClientBurstMultiplier, handlerCalls)
	require.Len(t, server.Keys(), enterpriseAuthClientBurstMultiplier+2)

	recorder := performEnterpriseAuthRequest(router, "/test", "another-unknown.invalid", "198.51.100.10")
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Contains(t, recorder.Body.String(), "rate limit exceeded")
	require.Equal(t, enterpriseAuthClientBurstMultiplier, handlerCalls)
	require.Len(t, server.Keys(), enterpriseAuthClientBurstMultiplier+2)
}

func TestEnterprisePublicAuthRateLimitBoundsForwardedIPAndHostChurnByTransportPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h := NewHandler(nil, rdb)

	peerKey := "rate_limit:enterprise-auth-test-peer:" + hashRateLimitScope("198.51.100.10")
	server.Set(peerKey, strconv.Itoa(enterpriseAuthPeerBurstMultiplier))
	server.SetTTL(peerKey, time.Minute)

	handlerCalls := 0
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ippkg.SetForwardedIPSettings(c, true, nil)
		c.Next()
	})
	handlers := append(h.publicAuthRateLimits("enterprise-auth-test", 1), func(c *gin.Context) {
		handlerCalls++
		c.Status(http.StatusNoContent)
	})
	router.POST("/test", handlers...)

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("{"))
	req.Host = "rotated.invalid"
	req.RemoteAddr = "198.51.100.10:1234"
	req.Header.Set("CF-Connecting-IP", "203.0.113.99")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Contains(t, recorder.Body.String(), "rate limit exceeded")
	require.Zero(t, handlerCalls)
	require.Equal(t, []string{peerKey}, server.Keys())
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
	h := NewHandler(nil, rdb)

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
