package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type realtimeLifecycleStore struct {
	GatewayCache
	mu     sync.Mutex
	record *RealtimeCallRecord
	usage  map[string]struct{}
}

func (s *realtimeLifecycleStore) SaveRealtimeCall(_ context.Context, record *RealtimeCallRecord, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *record
	s.record = &copy
	return nil
}

func (s *realtimeLifecycleStore) GetRealtimeCall(_ context.Context, callHash string) (*RealtimeCallRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash {
		return nil, ErrRealtimeCallNotFound
	}
	copy := *s.record
	return &copy, nil
}

func (s *realtimeLifecycleStore) ClaimRealtimeController(_ context.Context, callHash, controller, owner string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.Controller == RealtimeControllerClosed {
		return false, nil
	}
	if controller == RealtimeControllerObserver && s.record.Controller != RealtimeControllerPending {
		return false, nil
	}
	if controller == RealtimeControllerProxy && s.record.Controller != RealtimeControllerPending && s.record.Controller != RealtimeControllerObserver {
		return false, nil
	}
	s.record.Controller = controller
	s.record.ControllerOwner = owner
	return true, nil
}

func (s *realtimeLifecycleStore) ReleaseRealtimeController(_ context.Context, callHash, owner string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.ControllerOwner != owner {
		return false, nil
	}
	s.record.Controller = RealtimeControllerPending
	s.record.ControllerOwner = ""
	return true, nil
}

func (s *realtimeLifecycleStore) GetRealtimeController(_ context.Context, callHash string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash {
		return "", ErrRealtimeCallNotFound
	}
	return s.record.Controller, nil
}

func (s *realtimeLifecycleStore) MarkRealtimeCallClosed(_ context.Context, callHash string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.Controller == RealtimeControllerClosed {
		return false, nil
	}
	s.record.Controller = RealtimeControllerClosed
	s.record.ControllerOwner = ""
	return true, nil
}

func (s *realtimeLifecycleStore) BeginRealtimeUsage(_ context.Context, callHash, responseID string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.usage == nil {
		s.usage = make(map[string]struct{})
	}
	key := callHash + ":" + responseID
	if _, ok := s.usage[key]; ok {
		return false, nil
	}
	s.usage[key] = struct{}{}
	return true, nil
}

func (*realtimeLifecycleStore) CommitRealtimeUsage(context.Context, string, string, time.Duration) error {
	return nil
}

func (s *realtimeLifecycleStore) ReleaseRealtimeUsage(_ context.Context, callHash, responseID string) error {
	s.mu.Lock()
	delete(s.usage, callHash+":"+responseID)
	s.mu.Unlock()
	return nil
}

func (*realtimeLifecycleStore) BeginRealtimeWebhook(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}

func (*realtimeLifecycleStore) CommitRealtimeWebhook(context.Context, string, time.Duration) error {
	return nil
}
func (*realtimeLifecycleStore) ReleaseRealtimeWebhook(context.Context, string) error { return nil }

type realtimeLifecycleConcurrencyCache struct {
	ConcurrencyCache
	mu       sync.Mutex
	releases int
}

func (*realtimeLifecycleConcurrencyCache) AcquireRealtimeLease(context.Context, int64, int, int64, int, int64, string, bool) (bool, error) {
	return true, nil
}

func (*realtimeLifecycleConcurrencyCache) RefreshRealtimeLease(context.Context, int64, int64, int64, string) (bool, error) {
	return true, nil
}

func (c *realtimeLifecycleConcurrencyCache) ReleaseRealtimeLease(context.Context, int64, int64, int64, string) error {
	c.mu.Lock()
	c.releases++
	c.mu.Unlock()
	return nil
}

func TestFinalizeOfficialRealtimeCallIsIdempotent(t *testing.T) {
	record := &RealtimeCallRecord{
		CallID: "call_secret", CallHash: hashRealtimeCallID("call_secret"), AccountID: 11,
		APIKeyID: 22, UserID: 33, LeaseID: "lease-1", Controller: RealtimeControllerPending,
	}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	concurrency := &realtimeLifecycleConcurrencyCache{}
	service := &OpenAIGatewayService{
		cache:              store,
		concurrencyService: NewConcurrencyService(concurrency),
	}
	var finalizerCalls int
	service.realtimeFinalizers.Store(record.CallHash, func() { finalizerCalls++ })

	service.finalizeOfficialRealtimeCall(record)
	service.finalizeOfficialRealtimeCall(record)

	concurrency.mu.Lock()
	require.Equal(t, 1, concurrency.releases)
	concurrency.mu.Unlock()
	require.Equal(t, 1, finalizerCalls)
	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerClosed, controller)
}

func TestGetOfficialRealtimeCallForIdentityRejectsMismatchedCaller(t *testing.T) {
	groupID := int64(44)
	record := &RealtimeCallRecord{
		CallID: "call_identity", CallHash: hashRealtimeCallID("call_identity"), APIKeyID: 22,
		UserID: 33, GroupID: groupID, Controller: RealtimeControllerPending,
	}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	service := &OpenAIGatewayService{cache: store}

	_, err := service.GetOfficialRealtimeCallForIdentity(context.Background(), record.CallID, RealtimeCallIdentity{
		APIKeyID: 99, UserID: record.UserID, GroupID: &groupID,
	})
	require.ErrorIs(t, err, ErrRealtimeIdentityMismatch)

	loaded, err := service.GetOfficialRealtimeCallForIdentity(context.Background(), record.CallID, RealtimeCallIdentity{
		APIKeyID: record.APIKeyID, UserID: record.UserID, GroupID: &groupID,
	})
	require.NoError(t, err)
	require.Equal(t, record.CallID, loaded.CallID)
}

func TestRealtimeObserverSessionEndedRecognizesProviderEvents(t *testing.T) {
	require.True(t, realtimeObserverSessionEnded("session.closed"))
	require.True(t, realtimeObserverSessionEnded("realtime.call.ended"))
	require.False(t, realtimeObserverSessionEnded("response.done"))
}
