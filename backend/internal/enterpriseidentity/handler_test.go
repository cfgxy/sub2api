package enterpriseidentity

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutesIncludesEnterpriseAuthSessionsAndAdminAPIs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewHandler(nil)
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
