//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pricingUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *pricingUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	if r.user == nil {
		return nil, service.ErrUserNotFound
	}
	return r.user, nil
}

func (r *pricingUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (r *pricingUserRepo) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	return nil
}

func TestModelPricingAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.JWT.Secret = "pricing-unit-test-secret-32-bytes"
	cfg.JWT.AccessTokenExpireMinutes = 60
	user := &service.User{ID: 1, Role: "user", Status: service.StatusActive, TokenVersion: 1}
	repo := &pricingUserRepo{user: user}
	auth := service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userService := service.NewUserService(repo, nil, nil, nil)
	token, err := auth.GenerateToken(context.Background(), user)
	require.NoError(t, err)
	router := gin.New()
	RegisterModelPricingRoutes(router.Group("/api/v1"), middleware.NewJWTAuthMiddleware(auth, userService, nil, nil), nil, nil)

	request := func(bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/model-pricing", nil)
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, bearer := range []string{"", "invalid"} {
		w := request(bearer)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.NotContains(t, w.Body.String(), "¥0.70")
		require.NotContains(t, w.Body.String(), "个人分组 0.35x")
	}
	w := request(token)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
	var body struct {
		Data struct {
			HTML string `json:"html"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Contains(t, body.Data.HTML, "¥0.70")
	require.Contains(t, body.Data.HTML, "未与后台计费配置逐项核对")
	require.Contains(t, body.Data.HTML, "参考价")

	user.TokenVersion++
	require.Equal(t, http.StatusUnauthorized, request(token).Code)
	user.TokenVersion--
	user.Status = service.StatusDisabled
	require.Equal(t, http.StatusUnauthorized, request(token).Code)
	user.Status = service.StatusActive
	repo.user = nil
	require.Equal(t, http.StatusUnauthorized, request(token).Code)
}
