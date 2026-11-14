//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type oauthRefreshGateOpenAIClientSpy struct {
	refreshCalls int
}

func (s *oauthRefreshGateOpenAIClientSpy) ExchangeCode(context.Context, string, string, string, string, string) (*openai.TokenResponse, error) {
	return nil, nil
}

func (s *oauthRefreshGateOpenAIClientSpy) RefreshToken(context.Context, string, string) (*openai.TokenResponse, error) {
	s.refreshCalls++
	return &openai.TokenResponse{AccessToken: "unexpected-access-token"}, nil
}

func (s *oauthRefreshGateOpenAIClientSpy) RefreshTokenWithClientID(context.Context, string, string, string) (*openai.TokenResponse, error) {
	s.refreshCalls++
	return &openai.TokenResponse{AccessToken: "unexpected-access-token"}, nil
}

type oauthRefreshGateGrokClientSpy struct {
	refreshCalls int
}

func (s *oauthRefreshGateGrokClientSpy) ExchangeCode(context.Context, string, string, string, string, string) (*xai.TokenResponse, error) {
	return nil, nil
}

func (s *oauthRefreshGateGrokClientSpy) RefreshToken(context.Context, string, string, string) (*xai.TokenResponse, error) {
	s.refreshCalls++
	return &xai.TokenResponse{AccessToken: "unexpected-access-token"}, nil
}

func (s *oauthRefreshGateGrokClientSpy) LoginWithPassword(context.Context, string, string, string) (*service.GrokPasswordLoginResult, error) {
	return nil, nil
}

func (s *oauthRefreshGateGrokClientSpy) ConvertSSOToBuild(context.Context, string, string) (*xai.TokenResponse, error) {
	return nil, nil
}

func TestOpenAIOAuthRefreshHandlerDisabledDoesNotCallTokenEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &oauthRefreshGateOpenAIClientSpy{}
	svc := service.NewOpenAIOAuthService(nil, client, service.NewOAuthRefreshGateWithEnabled(false))
	t.Cleanup(svc.Stop)
	h := NewOpenAIOAuthHandler(svc, nil, nil, nil)

	router := gin.New()
	router.POST("/api/v1/admin/openai/refresh-token", h.RefreshToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/openai/refresh-token", strings.NewReader(`{"refresh_token":"old-refresh-token"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "OAUTH_TOKEN_REFRESH_DISABLED")
	require.Zero(t, client.refreshCalls)
}

func TestOpenAIOAuthRefreshHandlerRestrictedKeyDisabledDoesNotCallTokenEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &oauthRefreshGateOpenAIClientSpy{}
	svc := service.NewOpenAIOAuthService(nil, client, service.NewOAuthRefreshGateWithEnabled(false))
	t.Cleanup(svc.Stop)
	h := NewOpenAIOAuthHandler(svc, nil, nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{
			Policy: adminaccess.Policy{GlobalPermissions: []string{"openai.tokens.refresh"}},
		}))
	})
	router.POST("/api/v1/admin/openai/refresh-token", h.RefreshToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/openai/refresh-token", strings.NewReader(`{"refresh_token":"old-refresh-token"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	// 受限密钥对上游错误使用统一的 502 脱敏响应，但 gate 仍在实际服务边界生效。
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "调用方令牌刷新失败")
	require.Zero(t, client.refreshCalls)
}

func TestGrokOAuthRefreshHandlerDisabledDoesNotCallTokenEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &oauthRefreshGateGrokClientSpy{}
	svc := service.NewGrokOAuthService(nil, client)
	svc.SetRefreshGate(service.NewOAuthRefreshGateWithEnabled(false))
	t.Cleanup(svc.Stop)
	h := NewGrokOAuthHandler(svc, nil, nil, nil)

	router := gin.New()
	router.POST("/api/v1/admin/grok/oauth/refresh-token", h.RefreshToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grok/oauth/refresh-token", strings.NewReader(`{"refresh_token":"old-refresh-token"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "OAUTH_TOKEN_REFRESH_DISABLED")
	require.Zero(t, client.refreshCalls)
}

type oauthRefreshGateAdminServiceStub struct {
	service.AdminService
	account      *service.Account
	updateCalls  int
	lastUpdate   *service.UpdateAccountInput
	accountsCall int
}

func (s *oauthRefreshGateAdminServiceStub) GetAccount(context.Context, int64) (*service.Account, error) {
	return s.account, nil
}

func (s *oauthRefreshGateAdminServiceStub) GetAccountsByIDs(context.Context, []int64) ([]*service.Account, error) {
	s.accountsCall++
	return []*service.Account{s.account}, nil
}

func (s *oauthRefreshGateAdminServiceStub) UpdateAccount(_ context.Context, _ int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.updateCalls++
	s.lastUpdate = input
	return s.account, nil
}

func (s *oauthRefreshGateAdminServiceStub) EnsureOpenAIPrivacy(context.Context, *service.Account) string {
	return ""
}

func (s *oauthRefreshGateAdminServiceStub) EnsureAntigravityPrivacy(context.Context, *service.Account) string {
	return ""
}

func newDisabledOpenAIAccount() *service.Account {
	return &service.Account{
		ID:       81,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Status:   service.StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-access-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    "2026-12-01T00:00:00Z",
		},
	}
}

func TestAdminAccountRefreshDisabledDoesNotPersistCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newDisabledOpenAIAccount()
	original := service.MergeCredentials(account.Credentials, nil)
	adminSvc := &oauthRefreshGateAdminServiceStub{account: account}
	client := &oauthRefreshGateOpenAIClientSpy{}
	openaiSvc := service.NewOpenAIOAuthService(nil, client, service.NewOAuthRefreshGateWithEnabled(false))
	t.Cleanup(openaiSvc.Stop)
	h := NewAccountHandler(adminSvc, nil, openaiSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	router := gin.New()
	router.POST("/api/v1/admin/accounts/:id/refresh", h.Refresh)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/81/refresh", nil))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "OAUTH_TOKEN_REFRESH_DISABLED")
	require.Zero(t, client.refreshCalls)
	require.Zero(t, adminSvc.updateCalls)
	require.Equal(t, original, account.Credentials)
}

func TestAdminBatchRefreshDisabledDoesNotPersistCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newDisabledOpenAIAccount()
	original := service.MergeCredentials(account.Credentials, nil)
	adminSvc := &oauthRefreshGateAdminServiceStub{account: account}
	client := &oauthRefreshGateOpenAIClientSpy{}
	openaiSvc := service.NewOpenAIOAuthService(nil, client, service.NewOAuthRefreshGateWithEnabled(false))
	t.Cleanup(openaiSvc.Stop)
	h := NewAccountHandler(adminSvc, nil, openaiSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	router := gin.New()
	router.POST("/api/v1/admin/accounts/batch-refresh", h.BatchRefresh)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/batch-refresh", strings.NewReader(`{"account_ids":[81]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "OAUTH_TOKEN_REFRESH_DISABLED")
	require.Contains(t, rec.Body.String(), `"success":0`)
	require.Contains(t, rec.Body.String(), `"failed":1`)
	require.Zero(t, client.refreshCalls)
	require.Zero(t, adminSvc.updateCalls)
	require.Equal(t, original, account.Credentials)
}
