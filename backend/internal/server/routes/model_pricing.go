package routes

import (
	_ "embed"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

//go:embed model_pricing.md
var modelPricingMarkdown []byte

// RegisterModelPricingRoutes 仅向已登录的个人站用户提供静态参考价正文。
func RegisterModelPricingRoutes(v1 *gin.RouterGroup, jwtAuth middleware.JWTAuthMiddleware) {
	pricing := v1.Group("/model-pricing")
	pricing.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	pricing.Use(gin.HandlerFunc(jwtAuth))
	pricing.GET("/content", func(c *gin.Context) {
		role, ok := middleware.GetUserRoleFromContext(c)
		if !ok || (role != service.RoleUser && role != service.RoleAdmin) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Data(http.StatusOK, "text/markdown; charset=utf-8", modelPricingMarkdown)
	})
}
