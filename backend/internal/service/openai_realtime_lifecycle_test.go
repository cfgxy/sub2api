package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type realtimeLifecycleStore struct {
	GatewayCache
	mu        sync.Mutex
	record    *RealtimeCallRecord
	usage     map[string]struct{}
	committed map[string]struct{}
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

func (s *realtimeLifecycleStore) ClaimRealtimeUsage(ctx context.Context, callHash, responseID, _ string, ttl time.Duration) (bool, error) {
	return s.BeginRealtimeUsage(ctx, callHash, responseID, ttl)
}

func (s *realtimeLifecycleStore) CommitRealtimeUsageClaim(_ context.Context, callHash, responseID, _ string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := callHash + ":" + responseID
	if _, ok := s.usage[key]; !ok {
		return false, nil
	}
	if s.committed == nil {
		s.committed = make(map[string]struct{})
	}
	s.committed[key] = struct{}{}
	return true, nil
}

func (s *realtimeLifecycleStore) ReleaseRealtimeUsageClaim(_ context.Context, callHash, responseID, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := callHash + ":" + responseID
	if _, ok := s.usage[key]; !ok {
		return false, nil
	}
	delete(s.usage, key)
	return true, nil
}

func (s *realtimeLifecycleStore) HasCommittedRealtimeUsage(_ context.Context, callHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.committed {
		if len(key) > len(callHash) && key[:len(callHash)+1] == callHash+":" {
			return true, nil
		}
	}
	return false, nil
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

func (*realtimeLifecycleStore) ClaimRealtimeWebhook(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (*realtimeLifecycleStore) CommitRealtimeWebhookClaim(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (*realtimeLifecycleStore) ReleaseRealtimeWebhookClaim(context.Context, string, string) (bool, error) {
	return true, nil
}

func (s *realtimeLifecycleStore) ListActiveRealtimeCalls(context.Context) ([]*RealtimeCallRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.Controller == RealtimeControllerClosed {
		return nil, nil
	}
	copy := *s.record
	return []*RealtimeCallRecord{&copy}, nil
}

func (s *realtimeLifecycleStore) ResetRealtimeControllerForRecovery(_ context.Context, callHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.Controller == RealtimeControllerClosed {
		return false, nil
	}
	s.record.Controller = RealtimeControllerPending
	s.record.ControllerOwner = ""
	return true, nil
}

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

func TestFinalizeOfficialRealtimeCallConservativelySettlesOnlyWithoutUsage(t *testing.T) {
	record := &RealtimeCallRecord{
		CallID: "call_settlement", CallHash: hashRealtimeCallID("call_settlement"), AccountID: 11,
		APIKeyID: 22, UserID: 33, LeaseID: "lease-1", Controller: RealtimeControllerPending, ReservationUSD: 0.9,
	}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	concurrency := &realtimeLifecycleConcurrencyCache{}
	svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(concurrency)}
	var settlements atomic.Int32
	svc.SetOfficialRealtimeLifecycleCallbacks(nil, func(got *RealtimeCallRecord) error {
		require.Equal(t, record.CallHash, got.CallHash)
		settlements.Add(1)
		return nil
	})

	svc.finalizeOfficialRealtimeCall(record)
	svc.finalizeOfficialRealtimeCall(record)
	require.Equal(t, int32(1), settlements.Load())

	withUsage := &RealtimeCallRecord{
		CallID: "call_usage", CallHash: hashRealtimeCallID("call_usage"), AccountID: 11,
		APIKeyID: 22, UserID: 33, LeaseID: "lease-2", Controller: RealtimeControllerPending, ReservationUSD: 0.9,
	}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), withUsage, time.Hour))
	claimed, err := store.ClaimRealtimeUsage(context.Background(), withUsage.CallHash, "response", "owner", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	committed, err := store.CommitRealtimeUsageClaim(context.Background(), withUsage.CallHash, "response", "owner", time.Hour)
	require.NoError(t, err)
	require.True(t, committed)
	svc.finalizeOfficialRealtimeCall(withUsage)
	require.Equal(t, int32(1), settlements.Load(), "actual usage must suppress conservative settlement")
}

func TestRecoverOfficialRealtimeCallsRestartsObserverFromLedger(t *testing.T) {
	record := &RealtimeCallRecord{
		CallID: "call_recover", CallHash: hashRealtimeCallID("call_recover"), AccountID: 11,
		APIKeyID: 22, UserID: 33, LeaseID: "lease-recover", Controller: RealtimeControllerObserver,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(&realtimeLifecycleConcurrencyCache{})}
	restarted := make(chan *RealtimeCallRecord, 1)
	svc.SetOfficialRealtimeLifecycleCallbacks(func(got *RealtimeCallRecord) { restarted <- got }, nil)

	require.Eventually(t, func() bool {
		select {
		case got := <-restarted:
			return got.CallHash == record.CallHash
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerPending, controller)
}
