//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNodeLocOIDCExplicitDiscovery(t *testing.T) {
	for _, path := range []string{"/.well-known/openid-configuration", "/oauth-provider/.well-known/openid-configuration"} {
		t.Run(path, func(t *testing.T) {
			var baseURL string
			var hits int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, path, r.URL.Path)
				require.Equal(t, "application/json", r.Header.Get("Accept"))
				hits++
				w.Header().Set("Content-Type", "application/json")
				require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
					"issuer":                 baseURL,
					"authorization_endpoint": baseURL + "/oauth-provider/authorize",
					"token_endpoint":         baseURL + "/oauth-provider/token",
					"userinfo_endpoint":      baseURL + "/oauth-provider/userinfo",
					"jwks_uri":               baseURL + "/oauth-provider/jwks",
				}))
			}))
			defer server.Close()
			baseURL = server.URL

			cfg := &config.Config{OIDC: config.OIDCConnectConfig{
				Enabled: true, ProviderName: "NodeLoc",
				ClientID: "mock-client", ClientSecret: "mock-secret",
				IssuerURL: baseURL, DiscoveryURL: baseURL + path,
				Scopes: "openid profile", TokenAuthMethod: "client_secret_post",
				RedirectURL:         "https://example.com/api/v1/auth/oauth/oidc/callback",
				FrontendRedirectURL: "/auth/oidc/callback",
				UsePKCE:             true, ValidateIDToken: true, AllowedSigningAlgs: "RS256",
			}}
			svc := NewSettingService(&settingOIDCRepoStub{values: map[string]string{}}, cfg)
			got, err := svc.GetOIDCConnectOAuthConfig(context.Background())
			require.NoError(t, err)
			require.Equal(t, 1, hits)
			require.Equal(t, baseURL+path, got.DiscoveryURL)
			require.Equal(t, baseURL, got.IssuerURL)
			require.Equal(t, baseURL+"/oauth-provider/authorize", got.AuthorizeURL)
			require.Equal(t, baseURL+"/oauth-provider/token", got.TokenURL)
			require.Equal(t, baseURL+"/oauth-provider/userinfo", got.UserInfoURL)
			require.Equal(t, baseURL+"/oauth-provider/jwks", got.JWKSURL)
			require.Equal(t, "NodeLoc", got.ProviderName)
			require.True(t, got.UsePKCE)
			require.True(t, got.ValidateIDToken)
		})
	}
}

func TestNodeLocOIDCDiscoveryFailureIsClosed(t *testing.T) {
	for _, body := range []string{"unavailable", `{}`, `{"authorization_endpoint":"https://example.com/authorize"}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if body == "unavailable" {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			cfg := &config.Config{OIDC: config.OIDCConnectConfig{
				Enabled: true, ClientID: "mock-client", ClientSecret: "mock-secret",
				IssuerURL: server.URL, DiscoveryURL: server.URL + "/oauth-provider/.well-known/openid-configuration",
				Scopes: "openid profile", RedirectURL: "https://example.com/api/v1/auth/oauth/oidc/callback",
				FrontendRedirectURL: "/auth/oidc/callback", ValidateIDToken: true,
				AllowedSigningAlgs: "RS256",
			}}
			got, err := NewSettingService(&settingOIDCRepoStub{values: map[string]string{}}, cfg).
				GetOIDCConnectOAuthConfig(context.Background())
			require.Error(t, err)
			require.Empty(t, got.ClientSecret)
			require.Empty(t, got.AuthorizeURL)
		})
	}
}

func TestOAuthProvidersDisabledWithoutCredentials(t *testing.T) {
	svc := NewSettingService(&settingOIDCRepoStub{values: map[string]string{}}, &config.Config{})
	_, err := svc.GetLinuxDoConnectOAuthConfig(context.Background())
	require.ErrorContains(t, err, "disabled")
	_, err = svc.GetOIDCConnectOAuthConfig(context.Background())
	require.ErrorContains(t, err, "disabled")
	public, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.False(t, public.LinuxDoOAuthEnabled)
	require.False(t, public.OIDCOAuthEnabled)
}
