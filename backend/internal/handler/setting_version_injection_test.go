//go:build unit && embed

package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type versionInjectionRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *versionInjectionRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string)
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *versionInjectionRepo) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}

func (r *versionInjectionRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func TestProvidedSettingsVersionInEmbeddedHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream, err := os.ReadFile("../../cmd/server/VERSION")
	require.NoError(t, err)
	edition, err := os.ReadFile("../../cmd/server/EDITION")
	require.NoError(t, err)
	version := strings.TrimSpace(string(upstream)) + "-" + strings.TrimSpace(string(edition)) + ".20261018-5"

	for _, state := range []string{"首次加载", "保存关闭设置后", "保存开启设置后"} {
		t.Run(state, func(t *testing.T) {
			repo := &versionInjectionRepo{values: map[string]string{service.SettingKeySiteName: "保存前"}}
			settings := service.NewSettingService(repo, &config.Config{})
			// 使用生产 Wire provider 传递构建身份，不直接填充注入对象或调用 SetVersion。
			public := handler.ProvideSettingHandler(settings, handler.BuildInfo{Version: version}, nil)
			frontend, err := web.NewFrontendServer(settings)
			require.NoError(t, err)
			invalidations := 0
			settings.SetOnUpdateCallback(func() {
				invalidations++
				frontend.InvalidateCache()
			})
			router := gin.New()
			router.Use(frontend.Middleware())
			router.GET("/api/v1/settings/public", public.GetPublicSettings)

			readHTML := func() map[string]any {
				t.Helper()
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil))
				require.Equal(t, http.StatusOK, response.Code)
				_, payload, found := strings.Cut(response.Body.String(), "window.__APP_CONFIG__=")
				require.True(t, found, "实际嵌入 HTML 必须包含配置")
				payload, _, found = strings.Cut(payload, ";</script>")
				require.True(t, found)
				var injected map[string]any
				require.NoError(t, json.Unmarshal([]byte(payload), &injected))
				return injected
			}

			first := readHTML()
			if state != "首次加载" {
				enabled := state == "保存开启设置后"
				require.NoError(t, settings.UpdateSettings(context.Background(), &service.SystemSettings{
					SiteName: "保存后", AutoUpdateCheckEnabled: enabled,
				}))
				require.Equal(t, 1, invalidations, "真实设置保存必须失效 HTML 缓存")
				first = readHTML()
				require.Equal(t, "保存后", first["site_name"])
				require.Equal(t, enabled, first["auto_update_check_enabled"])
			}
			require.Equal(t, version, first["version"], "注入必须独立于更新检查提供完整构建版本")
			require.Equal(t, version, readHTML()["version"], "缓存命中时保留版本")

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil))
			require.Equal(t, http.StatusOK, response.Code)
			var api struct {
				Data struct {
					Version string `json:"version"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &api))
			require.Equal(t, version, api.Data.Version, "API 和 HTML 必须来自同一个 BuildInfo")
		})
	}
}
