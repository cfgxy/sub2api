//go:build unit

package routes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type authRefreshContractCache struct {
	mu     sync.Mutex
	tokens map[string]*service.RefreshTokenData
}

func newAuthRefreshContractCache() *authRefreshContractCache {
	return &authRefreshContractCache{tokens: make(map[string]*service.RefreshTokenData)}
}

func (c *authRefreshContractCache) StoreRefreshToken(_ context.Context, hash string, data *service.RefreshTokenData, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[hash] = data
	return nil
}

func (c *authRefreshContractCache) GetRefreshToken(_ context.Context, hash string) (*service.RefreshTokenData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.tokens[hash]
	if !ok {
		return nil, service.ErrRefreshTokenNotFound
	}
	return data, nil
}

func (c *authRefreshContractCache) DeleteRefreshToken(_ context.Context, hash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, hash)
	return nil
}

func (*authRefreshContractCache) DeleteUserRefreshTokens(context.Context, int64) error {
	return nil
}

func (*authRefreshContractCache) DeleteTokenFamily(context.Context, string) error {
	return nil
}

func (*authRefreshContractCache) AddToUserTokenSet(context.Context, int64, string, time.Duration) error {
	return nil
}

func (*authRefreshContractCache) AddToFamilyTokenSet(context.Context, string, string, time.Duration) error {
	return nil
}

func (*authRefreshContractCache) GetUserTokenHashes(context.Context, int64) ([]string, error) {
	return nil, nil
}

func (*authRefreshContractCache) GetFamilyTokenHashes(context.Context, string) ([]string, error) {
	return nil, nil
}

func (*authRefreshContractCache) IsTokenInFamily(context.Context, string, string) (bool, error) {
	return false, nil
}

type authRefreshContractUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *authRefreshContractUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return r.user, nil
}

type authRefreshContractSettingRepo struct {
	service.SettingRepository
}

func (authRefreshContractSettingRepo) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}

func authRefreshContractTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func TestAuthRefreshRouteContractRemainsAvailableWhenOAuthRefreshDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{Enabled: false},
		JWT: config.JWTConfig{
			Secret:                   strings.Repeat("x", 32),
			AccessTokenExpireMinutes: 60,
			RefreshTokenExpireDays:   30,
		},
	}
	gate := service.NewOAuthRefreshGateWithEnabled(false)
	require.False(t, gate.Enabled())

	user := &service.User{
		ID:                   701,
		Email:                "session-refresh@example.com",
		Role:                 service.RoleUser,
		Status:               service.StatusActive,
		TokenVersion:         1,
		TokenVersionResolved: true,
	}
	cache := newAuthRefreshContractCache()
	authService := service.NewAuthService(
		nil,
		&authRefreshContractUserRepo{user: user},
		nil,
		cache,
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	initialPair, err := authService.GenerateTokenPair(context.Background(), user, "session-family")
	require.NoError(t, err)

	settingService := service.NewSettingService(authRefreshContractSettingRepo{}, cfg)
	authHandler := handler.NewAuthHandler(cfg, authService, nil, settingService, nil, nil, nil, nil)
	router := gin.New()
	RegisterAuthRoutes(
		router.Group("/api/v1"),
		&handler.Handlers{Auth: authHandler},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() }),
		servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }),
		redisClient,
		settingService,
		nil,
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(
		`{"refresh_token":"`+initialPair.RefreshToken+`"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
			TokenType    string `json:"token_type"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEmpty(t, body.Data.AccessToken)
	require.NotEmpty(t, body.Data.RefreshToken)
	require.NotEqual(t, initialPair.RefreshToken, body.Data.RefreshToken)
	require.Positive(t, body.Data.ExpiresIn)
	require.Equal(t, "Bearer", body.Data.TokenType)

	_, err = cache.GetRefreshToken(context.Background(), authRefreshContractTokenHash(initialPair.RefreshToken))
	require.ErrorIs(t, err, service.ErrRefreshTokenNotFound)
	_, err = cache.GetRefreshToken(context.Background(), authRefreshContractTokenHash(body.Data.RefreshToken))
	require.NoError(t, err)
}
