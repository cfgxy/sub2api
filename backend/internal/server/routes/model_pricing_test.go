package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPricingContentRequiresPersonalSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	auth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		switch c.GetHeader("Authorization") {
		case "Bearer personal":
			c.Set(string(middleware.ContextKeyUserRole), "user")
		case "Bearer enterprise":
			c.Set(string(middleware.ContextKeyUserRole), "enterprise_admin")
		default:
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	})
	RegisterModelPricingRoutes(router.Group("/api/v1"), auth)

	for _, tc := range []struct {
		name   string
		token  string
		status int
	}{
		{"匿名", "", http.StatusUnauthorized},
		{"企业管理员", "Bearer enterprise", http.StatusForbidden},
		{"个人登录", "Bearer personal", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/model-pricing/content", nil)
			request.Header.Set("Authorization", tc.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			require.Equal(t, tc.status, response.Code)
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			if tc.status == http.StatusOK {
				require.Contains(t, response.Body.String(), "gpt-6.1-sol")
				require.Contains(t, response.Body.String(), "¥0.20 / 张")
			} else {
				require.NotContains(t, response.Body.String(), "gpt-6.1-sol")
			}
		})
	}

}
