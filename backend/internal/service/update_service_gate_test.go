//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// countingGitHubClientStub counts outbound release probes so tests can assert
// that a disabled auto-update-check gate issues no requests at all.
type countingGitHubClientStub struct {
	updateServiceGitHubClientStub
	fetchLatestCalls int
}

func (c *countingGitHubClientStub) FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error) {
	c.fetchLatestCalls++
	return c.updateServiceGitHubClientStub.FetchLatestRelease(ctx, repo)
}

func newGateTestService(gate AutoUpdateCheckGate, client GitHubReleaseClient) *UpdateService {
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.0", "release")
	svc.SetAutoUpdateCheckGate(gate)
	return svc
}

// 开关关闭 + 非 force：零出站请求，返回无更新结果。
func TestCheckUpdateGateDisabledSkipsNonForcedCheck(t *testing.T) {
	client := &countingGitHubClientStub{updateServiceGitHubClientStub: updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v9.9.9", Name: "v9.9.9"},
	}}
	svc := newGateTestService(gateStub{enabled: false}, client)

	info, err := svc.CheckUpdate(context.Background(), false)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
	require.Equal(t, "0.1.0", info.LatestVersion)
	require.Zero(t, client.fetchLatestCalls, "disabled gate must not trigger outbound release requests")
}

// 开关关闭 + 手动检查（force=true）：照常探测。
func TestCheckUpdateGateDisabledStillAllowsManualCheck(t *testing.T) {
	client := &countingGitHubClientStub{updateServiceGitHubClientStub: updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v9.9.9", Name: "v9.9.9"},
	}}
	svc := newGateTestService(gateStub{enabled: false}, client)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.Equal(t, "9.9.9", info.LatestVersion)
	require.Equal(t, 1, client.fetchLatestCalls)
}

// 开关开启 + 非 force：行为与引入开关前一致。
func TestCheckUpdateGateEnabledKeepsPreToggleBehavior(t *testing.T) {
	client := &countingGitHubClientStub{updateServiceGitHubClientStub: updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v9.9.9", Name: "v9.9.9"},
	}}
	svc := newGateTestService(gateStub{enabled: true}, client)

	info, err := svc.CheckUpdate(context.Background(), false)

	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.Equal(t, 1, client.fetchLatestCalls)
}

// 未装配 gate（存量构造路径）：缺省视为开启。
func TestCheckUpdateNilGateDefaultsEnabled(t *testing.T) {
	client := &countingGitHubClientStub{updateServiceGitHubClientStub: updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v9.9.9", Name: "v9.9.9"},
	}}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.0", "release")

	info, err := svc.CheckUpdate(context.Background(), false)

	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.Equal(t, 1, client.fetchLatestCalls)
}

type gateStub struct{ enabled bool }

func (g gateStub) IsAutoUpdateCheckEnabled(context.Context) bool { return g.enabled }

// Provider 装配：settingService 必须作为 gate 注入，防止 wire 手工同步回归。
func TestProvideUpdateServiceWiresSettingGate(t *testing.T) {
	repo := &autoCheckSettingRepoStub{values: map[string]string{}}
	settingService := &SettingService{settingRepo: repo, cfg: &config.Config{}}
	cache := &updateServiceCacheStub{}
	client := &countingGitHubClientStub{updateServiceGitHubClientStub: updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v9.9.9", Name: "v9.9.9"},
	}}

	svc := ProvideUpdateService(cache, client, BuildInfo{Version: "0.1.0", BuildType: "release"}, settingService)

	require.NotNil(t, svc.autoCheckGate, "provider must wire the setting service as the auto-check gate")
	require.True(t, svc.autoCheckGate.IsAutoUpdateCheckEnabled(context.Background()), "absent key must read as enabled (opt-out default)")
}
