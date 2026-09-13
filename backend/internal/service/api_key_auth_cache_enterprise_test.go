//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotPreservesEnterpriseAttributionCandidate(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := &APIKey{
		ID: 11, UserID: 22, EnterpriseAttributionCandidate: true,
		User: &User{ID: 22, Status: StatusActive},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.True(t, snapshot.EnterpriseAttributionCandidate)
	require.Equal(t, 24, snapshot.Version)

	materialized := svc.snapshotToAPIKey("sk-test", snapshot)
	require.True(t, materialized.EnterpriseAttributionCandidate)
}

func TestAPIKeyServiceBypassesPositiveAuthCacheForEnterpriseCandidate(t *testing.T) {
	stale := &APIKey{
		ID: 11, UserID: 22, Status: StatusActive, EnterpriseAttributionCandidate: true,
		User: &User{ID: 22, Status: StatusActive, Role: RoleUser},
	}
	fresh := &APIKey{
		ID: 12, UserID: 22, Status: StatusActive, EnterpriseAttributionCandidate: true,
		User: &User{ID: 22, Status: StatusActive, Role: RoleUser},
	}
	cache := &authCacheStub{}
	staleSnapshot := (&APIKeyService{}).snapshotFromAPIKey(context.Background(), stale)
	cache.getAuthCache = func(context.Context, string) (*APIKeyAuthCacheEntry, error) {
		return &APIKeyAuthCacheEntry{Snapshot: staleSnapshot}, nil
	}
	repoCalls := 0
	repo := &authRepoStub{getByKeyForAuth: func(context.Context, string) (*APIKey, error) {
		repoCalls++
		return fresh, nil
	}}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, cache, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60},
	})

	apiKey, err := svc.GetByKey(context.Background(), "enterprise-key")

	require.NoError(t, err)
	require.Equal(t, fresh.ID, apiKey.ID)
	require.Equal(t, 1, repoCalls)
	require.Empty(t, cache.setAuthKeys)
}

func TestAPIKeyServiceInvalidationForgetsEnterpriseCandidateSingleflight(t *testing.T) {
	oldKey := &APIKey{
		ID: 11, UserID: 22, Status: StatusActive, EnterpriseAttributionCandidate: true,
		User: &User{ID: 22, Status: StatusActive, Role: RoleUser},
	}
	newKey := &APIKey{
		ID: 12, UserID: 22, Status: StatusActive, EnterpriseAttributionCandidate: true,
		User: &User{ID: 22, Status: StatusActive, Role: RoleUser},
	}
	lookupStarted := make(chan struct{})
	releaseOldLookup := make(chan struct{})
	var calls atomic.Int32
	repo := &authRepoStub{getByKeyForAuth: func(context.Context, string) (*APIKey, error) {
		if calls.Add(1) == 1 {
			close(lookupStarted)
			<-releaseOldLookup
			return oldKey, nil
		}
		return newKey, nil
	}}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{Singleflight: true},
	})
	type lookupResult struct {
		key *APIKey
		err error
	}
	first := make(chan lookupResult, 1)
	go func() {
		key, err := svc.GetByKey(context.Background(), "enterprise-key")
		first <- lookupResult{key: key, err: err}
	}()
	<-lookupStarted
	svc.invalidateLocalAuthCache(svc.authCacheKey("enterprise-key"))
	second := make(chan lookupResult, 1)
	go func() {
		key, err := svc.GetByKey(context.Background(), "enterprise-key")
		second <- lookupResult{key: key, err: err}
	}()

	select {
	case result := <-second:
		require.NoError(t, result.err)
		require.Equal(t, newKey.ID, result.key.ID)
	case <-time.After(time.Second):
		t.Fatal("post-invalidation lookup joined the stale singleflight")
	}
	close(releaseOldLookup)
	result := <-first
	require.NoError(t, result.err)
	require.Equal(t, oldKey.ID, result.key.ID)
	require.Equal(t, int32(2), calls.Load())
}

func TestAPIKeyServiceStartsInvalidationSubscriberForSingleflightOnly(t *testing.T) {
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, &authCacheStub{}, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{Singleflight: true},
	})
	svc.StartAuthCacheInvalidationSubscriber(context.Background())
	t.Cleanup(svc.StopAuthCacheInvalidationSubscriber)

	require.NotNil(t, svc.authInvalidationCancel)
}
