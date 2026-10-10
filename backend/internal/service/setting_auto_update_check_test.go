//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// stub SettingRepository for the auto-update-check switch tests.
type autoCheckSettingRepoStub struct {
	values     map[string]string
	multiError error
	setCalls   []map[string]string
}

func (s *autoCheckSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	if v, ok := s.values[key]; ok {
		return &Setting{Key: key, Value: v, UpdatedAt: time.Now()}, nil
	}
	return nil, ErrSettingNotFound
}

func (s *autoCheckSettingRepoStub) GetValue(_ context.Context, _ string) (string, error) {
	return "", ErrSettingNotFound
}

func (s *autoCheckSettingRepoStub) Set(_ context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *autoCheckSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	if s.multiError != nil {
		return s.multiError
	}
	s.setCalls = append(s.setCalls, settings)
	return nil
}

func (s *autoCheckSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	return s.values, nil
}

func (s *autoCheckSettingRepoStub) Delete(_ context.Context, _ string) error { return nil }

func (s *autoCheckSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if s.multiError != nil {
		return nil, s.multiError
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = s.values[k]
	}
	return out, nil
}

// 缺 key（存量部署）必须等价于开启：关闭语义只认显式 false。
func TestParseSettingsAutoUpdateCheckDefaultsEnabled(t *testing.T) {
	svc := &SettingService{cfg: &config.Config{}}

	result := svc.parseSettings(map[string]string{})

	require.True(t, result.AutoUpdateCheckEnabled, "legacy rows without the key must read as enabled")
}

func TestParseSettingsAutoUpdateCheckExplicitValues(t *testing.T) {
	svc := &SettingService{cfg: &config.Config{}}

	enabled := svc.parseSettings(map[string]string{SettingKeyAutoUpdateCheckEnabled: "true"})
	disabled := svc.parseSettings(map[string]string{SettingKeyAutoUpdateCheckEnabled: "false"})

	require.True(t, enabled.AutoUpdateCheckEnabled)
	require.False(t, disabled.AutoUpdateCheckEnabled)
}

func TestInitializeDefaultSettingsWritesAutoUpdateCheckEnabled(t *testing.T) {
	repo := &autoCheckSettingRepoStub{values: map[string]string{}}
	svc := &SettingService{settingRepo: repo, cfg: &config.Config{}}

	err := svc.InitializeDefaultSettings(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, repo.setCalls)
	last := repo.setCalls[len(repo.setCalls)-1]
	require.Equal(t, "true", last[SettingKeyAutoUpdateCheckEnabled])
}

func TestIsAutoUpdateCheckEnabledReadsStore(t *testing.T) {
	disabled := &autoCheckSettingRepoStub{values: map[string]string{SettingKeyAutoUpdateCheckEnabled: "false"}}
	enabled := &autoCheckSettingRepoStub{values: map[string]string{SettingKeyAutoUpdateCheckEnabled: "true"}}
	unset := &autoCheckSettingRepoStub{values: map[string]string{}}

	require.False(t, (&SettingService{settingRepo: disabled}).IsAutoUpdateCheckEnabled(context.Background()))
	require.True(t, (&SettingService{settingRepo: enabled}).IsAutoUpdateCheckEnabled(context.Background()))
	require.True(t, (&SettingService{settingRepo: unset}).IsAutoUpdateCheckEnabled(context.Background()))
}

// Fail-open：读取失败时等价开启，保持升级前行为。
func TestIsAutoUpdateCheckEnabledFailsOpen(t *testing.T) {
	repo := &autoCheckSettingRepoStub{multiError: errors.New("db down")}
	svc := &SettingService{settingRepo: repo}

	require.True(t, svc.IsAutoUpdateCheckEnabled(context.Background()))
}
