package enterpriseidentity

import (
	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	servicepkg "github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/redis/go-redis/v9"
)

func ProvideAPIKeyAuthCacheInvalidator(apiKeyService *servicepkg.APIKeyService) APIKeyAuthCacheInvalidator {
	return apiKeyService
}

// ProvideHandler 将生产依赖适配为 Handler 所需的窄接口。
func ProvideHandler(identityService *Service, keyRepository *enterprise.Repository, apiKeyService *servicepkg.APIKeyService, gatewayService *servicepkg.GatewayService, redisClient *redis.Client) *Handler {
	handler := NewHandler(identityService, keyRepository, apiKeyService, redisClient)
	handler.guideModels = gatewayService
	return handler
}
