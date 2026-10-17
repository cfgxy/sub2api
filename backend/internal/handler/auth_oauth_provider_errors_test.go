//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOAuthProviderErrorsDoNotCreateAccounts(t *testing.T) {
	for _, provider := range []string{"linuxdo", "oidc"} {
		for _, tc := range []struct {
			name, query, stateCookie, wantError string
			tokenFailure, userInfoFailure       bool
		}{
			{name: "取消授权", query: "error=access_denied", wantError: "provider_error"},
			{name: "缺少授权码", query: "state=state-ok", wantError: "missing_params"},
			{name: "state不一致", query: "code=mock-code&state=state-other", stateCookie: "state-ok", wantError: "invalid_state"},
			{name: "缺少state凭据", query: "code=mock-code&state=state-ok", wantError: "invalid_state"},
			{name: "授权码被拒绝", query: "code=mock-code&state=state-ok", stateCookie: "state-ok", tokenFailure: true, wantError: "token_exchange_failed"},
			{name: "用户信息不可用", query: "code=mock-code&state=state-ok", stateCookie: "state-ok", userInfoFailure: true, wantError: "userinfo_failed"},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				var upstreamHits int
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					upstreamHits++
					w.Header().Set("Content-Type", "application/json")
					if r.URL.Path == "/token" {
						require.NoError(t, r.ParseForm())
						require.Equal(t, "mock-code", r.PostForm.Get("code"))
						require.Equal(t, "https://example.com/api/v1/auth/oauth/"+provider+"/callback", r.PostForm.Get("redirect_uri"))
						require.Equal(t, "verifier-ok", r.PostForm.Get("code_verifier"))
						if tc.tokenFailure {
							w.WriteHeader(http.StatusBadRequest)
							_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
							return
						}
						_, _ = w.Write([]byte(`{"access_token":"mock-access","token_type":"Bearer"}`))
						return
					}
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":"unavailable"}`))
				}))
				defer server.Close()

				var handler *AuthHandler
				var callback func(*gin.Context)
				stateCookie, verifierCookie := linuxDoOAuthStateCookieName, linuxDoOAuthVerifierCookie
				if provider == "linuxdo" {
					handler = newLinuxDoOAuthTestHandler(t, false, config.LinuxDoConnectConfig{
						Enabled: true, ClientID: "mock-client", ClientSecret: "mock-secret",
						AuthorizeURL: server.URL + "/authorize", TokenURL: server.URL + "/token",
						UserInfoURL: server.URL + "/userinfo", Scopes: "user", UsePKCE: true,
						RedirectURL:         "https://example.com/api/v1/auth/oauth/linuxdo/callback",
						FrontendRedirectURL: "/auth/linuxdo/callback", TokenAuthMethod: "client_secret_post",
					})
					callback = handler.LinuxDoOAuthCallback
				} else {
					cfg, cleanup := newOIDCTestProvider(t, oidcProviderFixture{Subject: "mock-subject"})
					defer cleanup()
					cfg.ProviderName = "NodeLoc"
					cfg.RedirectURL = "https://example.com/api/v1/auth/oauth/oidc/callback"
					if tc.tokenFailure {
						cfg.TokenURL = server.URL + "/token"
					}
					if tc.userInfoFailure {
						cfg.UserInfoURL = server.URL + "/userinfo"
					}
					handler = newOIDCOAuthTestHandler(t, false, cfg)
					callback = handler.OIDCOAuthCallback
					stateCookie, verifierCookie = oidcOAuthStateCookieName, oidcOAuthVerifierCookie
				}
				ctx := context.Background()
				client := handler.entClient()
				before, err := client.User.Query().Count(ctx)
				require.NoError(t, err)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/"+provider+"/callback?"+tc.query, nil)
				if tc.stateCookie != "" {
					c.Request.AddCookie(encodedCookie(stateCookie, tc.stateCookie))
				}
				c.Request.AddCookie(encodedCookie(verifierCookie, "verifier-ok"))
				c.Request.AddCookie(encodedCookie(oidcOAuthNonceCookie, "nonce-mock-subject"))
				c.Request.AddCookie(encodedCookie(oauthPendingBrowserCookieName, "mock-browser"))
				callback(c)

				require.Equal(t, http.StatusFound, recorder.Code)
				location, err := url.Parse(recorder.Header().Get("Location"))
				require.NoError(t, err)
				require.Equal(t, "/auth/"+provider+"/callback", location.Path)
				fragment, err := url.ParseQuery(location.Fragment)
				require.NoError(t, err)
				require.Equal(t, tc.wantError, fragment.Get("error"))
				require.Empty(t, fragment.Get("access_token"))
				require.NotContains(t, location.String(), "mock-secret")
				after, err := client.User.Query().Count(ctx)
				require.NoError(t, err)
				require.Equal(t, before, after)
				identities, err := client.AuthIdentity.Query().Count(ctx)
				require.NoError(t, err)
				require.Zero(t, identities)
				sessions, err := client.PendingAuthSession.Query().Count(ctx)
				require.NoError(t, err)
				require.Zero(t, sessions)
				if tc.tokenFailure || tc.userInfoFailure {
					require.Positive(t, upstreamHits)
				} else {
					require.Zero(t, upstreamHits)
				}
			})
		}
	}
}
