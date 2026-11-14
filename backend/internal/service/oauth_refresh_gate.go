package service

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const oauthRefreshDisabledReason = "OAUTH_TOKEN_REFRESH_DISABLED"

// OAuthRefreshGate 是上游 OAuth refresh 的启动时策略快照。
// 它只控制 refresh_token grant，不影响授权码交换或本系统用户会话刷新。
type OAuthRefreshGate struct {
	enabled bool
}

// NewOAuthRefreshGate 从完整配置构造 OAuth refresh gate。
// 配置缺失时保持历史默认行为：允许刷新。
func NewOAuthRefreshGate(cfg *config.Config) *OAuthRefreshGate {
	if cfg == nil {
		return &OAuthRefreshGate{enabled: true}
	}
	return &OAuthRefreshGate{enabled: cfg.TokenRefresh.Enabled}
}

// NewOAuthRefreshGateWithEnabled 用于测试或显式构造固定策略。
func NewOAuthRefreshGateWithEnabled(enabled bool) *OAuthRefreshGate {
	return &OAuthRefreshGate{enabled: enabled}
}

// Enabled 返回当前启动快照是否允许上游 OAuth refresh。
func (g *OAuthRefreshGate) Enabled() bool {
	return g == nil || g.enabled
}

// RequireEnabled 在发起任何上游 refresh_token grant 前检查 gate。
func (g *OAuthRefreshGate) RequireEnabled() error {
	if g.Enabled() {
		return nil
	}
	return infraerrors.New(
		http.StatusServiceUnavailable,
		oauthRefreshDisabledReason,
		"upstream OAuth token refresh is disabled",
	)
}

func isOAuthRefreshDisabledError(err error) bool {
	return infraerrors.Reason(err) == oauthRefreshDisabledReason
}
