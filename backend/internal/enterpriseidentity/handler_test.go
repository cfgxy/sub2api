package enterpriseidentity

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	h := NewHandler(nil, rdb)
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
