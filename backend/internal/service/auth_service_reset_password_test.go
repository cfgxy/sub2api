//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type passwordResetCacheStub struct {
	emailCacheStub
	consumed int
}

func (s *passwordResetCacheStub) GetPasswordResetToken(context.Context, string) (*PasswordResetTokenData, error) {
	return &PasswordResetTokenData{Token: "reset-token"}, nil
}

func (s *passwordResetCacheStub) DeletePasswordResetToken(context.Context, string) error {
	s.consumed++
	return nil
}

func TestAuthService_ResetPassword_RequiresEightCharactersBeforeConsumingToken(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		accepted bool
	}{
		{name: "7 位被拒", password: "abcdefg"},
		{name: "8 位纯字母通过", password: "abcdefgh", accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &passwordResetCacheStub{}
			user := &User{ID: 42, Email: "user@example.com", Status: StatusActive}
			repo := &userRepoStub{user: user}
			svc := newAuthService(repo, map[string]string{
				SettingKeyEmailVerifyEnabled:   "true",
				SettingKeyPasswordResetEnabled: "true",
			}, cache, nil)
			err := svc.ResetPassword(context.Background(), user.Email, "reset-token", tc.password)
			if tc.accepted {
				require.NoError(t, err)
				require.Equal(t, 1, cache.consumed)
				require.Len(t, repo.updated, 1)
				require.True(t, user.CheckPassword(tc.password))
			} else {
				require.ErrorIs(t, err, ErrPasswordTooShort)
				require.ErrorContains(t, err, "密码至少需要 8 位")
				require.Zero(t, cache.consumed)
				require.Empty(t, repo.updated)
			}
		})
	}
}
