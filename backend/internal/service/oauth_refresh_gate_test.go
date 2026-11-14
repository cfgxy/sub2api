//go:build unit

package service

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestOAuthRefreshGateDisabledBlocksAllProviderRefreshBoundaries(t *testing.T) {
	gate := NewOAuthRefreshGate(&config.Config{})
	require.False(t, gate.Enabled())

	cases := []struct {
		name string
		call func() error
	}{
		{
			name: "claude",
			call: func() error {
				_, err := NewOAuthService(nil, nil, gate).RefreshToken(context.Background(), "refresh-token", "")
				return err
			},
		},
		{
			name: "openai",
			call: func() error {
				_, err := NewOpenAIOAuthService(nil, nil, gate).RefreshToken(context.Background(), "refresh-token", "")
				return err
			},
		},
		{
			name: "gemini",
			call: func() error {
				_, err := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{}, gate).RefreshToken(context.Background(), "code_assist", "refresh-token", "")
				return err
			},
		},
		{
			name: "antigravity",
			call: func() error {
				_, err := NewAntigravityOAuthService(nil, gate).RefreshToken(context.Background(), "refresh-token", "")
				return err
			},
		},
		{
			name: "grok",
			call: func() error {
				svc := NewGrokOAuthService(nil, nil)
				svc.SetRefreshGate(gate)
				_, err := svc.RefreshToken(context.Background(), "refresh-token", "", "client-id")
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			require.Equal(t, "OAUTH_TOKEN_REFRESH_DISABLED", infraerrors.Reason(err))
			require.Equal(t, 503, infraerrors.Code(err))
		})
	}
}

func TestOAuthRefreshAPIDisabledDoesNotReadOrMutateAccount(t *testing.T) {
	account := &Account{
		ID:       11,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-access-token",
			"refresh_token": "old-refresh-token",
			"expires_at":    "1234567890",
		},
	}
	repo := &refreshAPIAccountRepo{account: account}
	cache := &refreshAPICacheStub{lockResult: true}
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new-access-token"}}
	api := NewOAuthRefreshAPI(repo, cache)
	api.SetRefreshGate(NewOAuthRefreshGateWithEnabled(false))

	result, err := api.RefreshIfNeeded(context.Background(), account, executor, time.Minute)

	require.Nil(t, result)
	require.Equal(t, "OAUTH_TOKEN_REFRESH_DISABLED", infraerrors.Reason(err))
	require.Zero(t, repo.getByIDCalls)
	require.Zero(t, repo.updateCalls)
	require.Zero(t, cache.releaseCalls)
	require.Zero(t, cache.deleteCalls)
	require.Zero(t, executor.refreshCalls)
	require.Equal(t, "old-access-token", account.Credentials["access_token"])
	require.Equal(t, "old-refresh-token", account.Credentials["refresh_token"])
	require.Equal(t, "1234567890", account.Credentials["expires_at"])
}

func TestOAuthRefreshAPIEnabledPreservesExistingRefreshBehavior(t *testing.T) {
	account := &Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive}
	repo := &refreshAPIAccountRepo{account: account}
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new-access-token"}}
	api := NewOAuthRefreshAPI(repo, nil)
	api.SetRefreshGate(NewOAuthRefreshGateWithEnabled(true))

	result, err := api.RefreshIfNeeded(context.Background(), account, executor, time.Minute)

	require.NoError(t, err)
	require.True(t, result.Refreshed)
	require.Equal(t, 1, executor.refreshCalls)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, "new-access-token", result.NewCredentials["access_token"])
}

func TestAuthServiceJWTRefreshRemainsAvailableWhenOAuthRefreshDisabled(t *testing.T) {
	user := &User{
		ID:           13,
		Email:        "session-refresh@example.com",
		Role:         RoleUser,
		Status:       StatusActive,
		TokenVersion: 1,
	}
	service := newAuthService(&userRepoStub{user: user}, nil, nil, nil)
	service.cfg.TokenRefresh.Enabled = false

	oldToken, err := service.GenerateToken(context.Background(), user)
	require.NoError(t, err)
	newToken, err := service.RefreshToken(context.Background(), oldToken)

	require.NoError(t, err)
	require.NotEmpty(t, newToken)
	require.NotEqual(t, oldToken, newToken)
	claims, err := service.ValidateToken(newToken)
	require.NoError(t, err)
	require.Equal(t, user.ID, claims.UserID)
}

type oauthRefreshGateAccountRepo struct {
	AccountRepository
	setErrorCalls          int32
	tempUnschedulableCalls int32
}

func (r *oauthRefreshGateAccountRepo) SetError(context.Context, int64, string) error {
	atomic.AddInt32(&r.setErrorCalls, 1)
	return nil
}

func (r *oauthRefreshGateAccountRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	atomic.AddInt32(&r.tempUnschedulableCalls, 1)
	return nil
}

type oauthRefreshGateRuntimeBlocker struct {
	calls int32
}

func (b *oauthRefreshGateRuntimeBlocker) BlockAccountScheduling(*Account, time.Time, string) {
	atomic.AddInt32(&b.calls, 1)
}

func (*oauthRefreshGateRuntimeBlocker) ClearAccountSchedulingBlock(int64) {}

type oauthRefreshGateTempUnschedCache struct {
	calls int32
}

func (c *oauthRefreshGateTempUnschedCache) SetTempUnsched(context.Context, int64, *TempUnschedState) error {
	atomic.AddInt32(&c.calls, 1)
	return nil
}

func (*oauthRefreshGateTempUnschedCache) GetTempUnsched(context.Context, int64) (*TempUnschedState, error) {
	return nil, nil
}

func (*oauthRefreshGateTempUnschedCache) DeleteTempUnsched(context.Context, int64) error {
	return nil
}

func TestOpenAITokenProviderDisabledRefreshDoesNotMutateMissingRefreshAccount(t *testing.T) {
	repo := &oauthRefreshGateAccountRepo{}
	cache := newOpenAITokenCacheStub()
	api := NewOAuthRefreshAPI(repo, nil)
	api.SetRefreshGate(NewOAuthRefreshGateWithEnabled(false))
	provider := NewOpenAITokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(api, &refreshAPIExecutorStub{needsRefresh: true})
	blocker := &oauthRefreshGateRuntimeBlocker{}
	provider.SetAccountRuntimeBlocker(blocker)
	account := &Account{
		ID:       21,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token": "expired-access-token",
			"expires_at":   time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
		},
	}

	token, err := provider.GetAccessToken(context.Background(), account)

	require.Empty(t, token)
	require.Equal(t, oauthRefreshDisabledReason, infraerrors.Reason(err))
	require.Equal(t, 503, infraerrors.Code(err))
	require.Zero(t, atomic.LoadInt32(&repo.setErrorCalls))
	require.Zero(t, atomic.LoadInt32(&repo.tempUnschedulableCalls))
	require.Zero(t, atomic.LoadInt32(&blocker.calls))
	require.Zero(t, atomic.LoadInt32(&cache.deleteCalled))
}

func TestAntigravityTokenProviderDisabledRefreshDoesNotMarkUnschedulable(t *testing.T) {
	repo := &oauthRefreshGateAccountRepo{}
	api := NewOAuthRefreshAPI(repo, nil)
	api.SetRefreshGate(NewOAuthRefreshGateWithEnabled(false))
	provider := NewAntigravityTokenProvider(repo, nil, nil)
	executor := &refreshAPIExecutorStub{needsRefresh: true}
	provider.SetRefreshAPI(api, executor)
	cache := &oauthRefreshGateTempUnschedCache{}
	provider.SetTempUnschedCache(cache)
	account := &Account{
		ID:       22,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "expired-access-token",
			"expires_at":   time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
		},
	}

	token, err := provider.GetAccessToken(context.Background(), account)

	require.Empty(t, token)
	require.Equal(t, oauthRefreshDisabledReason, infraerrors.Reason(err))
	require.Equal(t, 503, infraerrors.Code(err))
	require.Zero(t, atomic.LoadInt32(&repo.tempUnschedulableCalls))
	require.Zero(t, atomic.LoadInt32(&cache.calls))
	require.Zero(t, executor.refreshCalls)
}

func TestGrokCredentialFailureDisabledRefreshDoesNotMutateAccount(t *testing.T) {
	class := classifyGrokCredentialFailure(
		&Account{ID: 23, Platform: PlatformGrok, Type: AccountTypeOAuth},
		NewOAuthRefreshGateWithEnabled(false).RequireEnabled(),
	)

	require.False(t, class.permanent)
	require.False(t, class.transient)
	require.Equal(t, GrokCredentialReasonProviderDown, class.reason)
}

func TestOAuthRefreshGateProductionProvidersUseLoadedConfig(t *testing.T) {
	load := func(t *testing.T, enabled string) *config.Config {
		t.Helper()
		viper.Reset()
		t.Cleanup(viper.Reset)
		t.Setenv("CONFIG_FILE", "")
		t.Setenv("DATA_DIR", "")
		t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
		t.Setenv("TOKEN_REFRESH_ENABLED", enabled)

		cfg, err := config.LoadForBootstrap()
		require.NoError(t, err)
		return cfg
	}

	t.Run("disabled config reaches every production provider", func(t *testing.T) {
		cfg := load(t, "false")
		gate := ProvideOAuthRefreshGate(cfg)
		claude := ProvideOAuthService(nil, nil, gate)
		openai := ProvideOpenAIOAuthService(nil, nil, nil, gate)
		gemini := ProvideGeminiOAuthService(nil, nil, nil, nil, cfg, gate)
		antigravity := ProvideAntigravityOAuthService(nil, gate)
		grok := ProvideGrokOAuthService(nil, nil, cfg, nil, gate)
		api := ProvideOAuthRefreshAPI(nil, nil, gate)
		background := ProvideTokenRefreshService(
			nil,
			claude,
			openai,
			gemini,
			antigravity,
			grok,
			nil,
			nil,
			cfg,
			gate,
			nil,
			nil,
			nil,
			api,
			nil,
		)
		t.Cleanup(background.Stop)

		require.False(t, cfg.TokenRefresh.Enabled)
		require.False(t, gate.Enabled())
		require.Same(t, gate, claude.refreshGate)
		require.Same(t, gate, openai.refreshGate)
		require.Same(t, gate, gemini.refreshGate)
		require.Same(t, gate, antigravity.refreshGate)
		require.Same(t, gate, grok.refreshGate)
		require.Same(t, gate, api.refreshGate)
		require.Same(t, gate, background.refreshGate)
		require.False(t, background.cfg.Enabled)
	})

	t.Run("enabled config preserves the refresh policy", func(t *testing.T) {
		cfg := load(t, "true")
		gate := ProvideOAuthRefreshGate(cfg)
		require.True(t, cfg.TokenRefresh.Enabled)
		require.True(t, gate.Enabled())
		require.NoError(t, gate.RequireEnabled())
	})
}
