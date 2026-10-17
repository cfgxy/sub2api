package routes

import (
	_ "embed"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// 正文仅嵌入后端，不作为匿名可下载的前端资源发布。
//
//go:embed model_pricing.html
var modelPricingHTML string

func RegisterModelPricingRoutes(
	v1 *gin.RouterGroup,
	jwtAuth middleware.JWTAuthMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	pricing := v1.Group("/model-pricing")
	pricing.Use(gin.HandlerFunc(jwtAuth))
	pricing.Use(middleware.BackendModeUserGuard(settingService))
	pricing.Use(panelRateLimiter.Global())
	pricing.GET("", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		response.Success(c, gin.H{"html": modelPricingHTML})
	})
}
