//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type createEnterpriseUserRepoStub struct {
	UserRepository
	user *User
}

func (s *createEnterpriseUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	return s.user, nil
}

type createEnterpriseAPIKeyRepoStub struct {
	APIKeyRepository
	dedicated   bool
	createCount int
}

func (s *createEnterpriseAPIKeyRepoStub) IsEnterpriseDedicatedUser(context.Context, int64) (bool, error) {
	return s.dedicated, nil
}

func (s *createEnterpriseAPIKeyRepoStub) Create(context.Context, *APIKey) error {
	s.createCount++
	return nil
}

func (s *createEnterpriseAPIKeyRepoStub) ExistsByKey(context.Context, string) (bool, error) {
	return false, nil
}

func TestAPIKeyCreateRejectsEnterpriseDedicatedUserBeforeCredentialCreation(t *testing.T) {
	repo := &createEnterpriseAPIKeyRepoStub{dedicated: true}
	svc := &APIKeyService{
		apiKeyRepo: repo,
		userRepo:   &createEnterpriseUserRepoStub{user: &User{ID: 42}},
	}

	_, err := svc.Create(context.Background(), 42, CreateAPIKeyRequest{Name: "blocked"})

	require.ErrorIs(t, err, ErrInsufficientPerms)
	require.Zero(t, repo.createCount)
}

func TestAPIKeyCreateKeepsNonEnterpriseUserPath(t *testing.T) {
	repo := &createEnterpriseAPIKeyRepoStub{}
	svc := &APIKeyService{
		apiKeyRepo: repo,
		userRepo:   &createEnterpriseUserRepoStub{user: &User{ID: 42}},
	}

	customKey := "sk-test-enterprise-path"
	key, err := svc.Create(context.Background(), 42, CreateAPIKeyRequest{Name: "allowed", CustomKey: &customKey})

	require.NoError(t, err)
	require.NotNil(t, key)
	require.Equal(t, 1, repo.createCount)
}
