package service

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type realtimeRecordingFrameConn struct {
	writes [][]byte
}

func (c *realtimeRecordingFrameConn) ReadFrame(context.Context) (coderws.MessageType, []byte, error) {
	return 0, nil, io.EOF
}

func (c *realtimeRecordingFrameConn) WriteFrame(_ context.Context, _ coderws.MessageType, payload []byte) error {
	c.writes = append(c.writes, append([]byte(nil), payload...))
	return nil
}

func (c *realtimeRecordingFrameConn) Close() error { return nil }

type realtimeLifecycleStore struct {
	GatewayCache
	mu                      sync.Mutex
	record                  *RealtimeCallRecord
	usage                   map[string]struct{}
	succeeded               map[string]struct{}
	committed               map[string]struct{}
	finalizer               string
	finalizerCommitFailures atomic.Int32
	finalizerCommitAfterSet atomic.Bool
	controllerLookupErrors  atomic.Int32
	usageCommitFailures     atomic.Int32
	markSucceededCalls      atomic.Int32
	usageCommitCalls        atomic.Int32
	saveFailures            atomic.Int32
}

type staleRealtimeControllerStore struct {
	*realtimeLifecycleStore
	blockNextLookup atomic.Bool
	lookupStarted   chan struct{}
	lookupRelease   chan struct{}
}

func (s *staleRealtimeControllerStore) GetRealtimeController(ctx context.Context, callHash string) (string, error) {
	controller, err := s.realtimeLifecycleStore.GetRealtimeController(ctx, callHash)
	if err != nil || !s.blockNextLookup.CompareAndSwap(true, false) {
		return controller, err
	}
	close(s.lookupStarted)
	<-s.lookupRelease
	return controller, nil
}

type realtimeUsageLeaseTestStore struct {
	*realtimeLifecycleStore
	claimOwner   string
	claimExpires time.Time
	renewals     atomic.Int32
}

type realtimeEmployeeTurnReservationRepo struct {
	UsageBillingRepository
	mu       sync.Mutex
	reserved map[string]EnterpriseEmployeeUsageReservationResult
	releases map[string]int
	renewals map[string]int
}

func (r *realtimeEmployeeTurnReservationRepo) ReserveEnterpriseEmployeeUsage(_ context.Context, cmd *EnterpriseEmployeeUsageReservationCommand) (*EnterpriseEmployeeUsageReservationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reserved == nil {
		r.reserved = make(map[string]EnterpriseEmployeeUsageReservationResult)
	}
	result, exists := r.reserved[cmd.RequestID]
	if !exists {
		result = EnterpriseEmployeeUsageReservationResult{
			ReservationID: 1, RequestID: cmd.RequestID, RequestFingerprint: cmd.RequestFingerprint,
			SourceAPIKeyID: cmd.SourceAPIKeyID, ReservedUSD: cmd.ReservedUSD, Status: "reserved",
		}
		r.reserved[cmd.RequestID] = result
	} else {
		result.AlreadyExists = true
	}
	copy := result
	return &copy, nil
}

func (r *realtimeEmployeeTurnReservationRepo) SettleEnterpriseEmployeeUsage(context.Context, *EnterpriseEmployeeUsageSettlementCommand) (*EnterpriseEmployeeUsageReservationResult, error) {
	return nil, errors.New("unexpected employee turn settlement")
}

func (r *realtimeEmployeeTurnReservationRepo) ReleaseEnterpriseEmployeeUsage(_ context.Context, cmd *EnterpriseEmployeeUsageReleaseCommand) (*EnterpriseEmployeeUsageReservationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.releases == nil {
		r.releases = make(map[string]int)
	}
	r.releases[cmd.RequestID]++
	return &EnterpriseEmployeeUsageReservationResult{RequestID: cmd.RequestID, SourceAPIKeyID: cmd.SourceAPIKeyID, Status: "released"}, nil
}

func (r *realtimeEmployeeTurnReservationRepo) RenewEnterpriseEmployeeUsage(_ context.Context, cmd *EnterpriseEmployeeUsageRenewCommand) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.renewals == nil {
		r.renewals = make(map[string]int)
	}
	r.renewals[cmd.RequestID]++
	return true, nil
}

func (s *realtimeUsageLeaseTestStore) ClaimRealtimeUsage(_ context.Context, callHash, responseID, owner string, lease time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Now().Before(s.claimExpires) {
		return false, nil
	}
	if s.usage == nil {
		s.usage = make(map[string]struct{})
	}
	s.usage[callHash+":"+responseID] = struct{}{}
	s.claimOwner = owner
	s.claimExpires = time.Now().Add(lease)
	return true, nil
}

func (s *realtimeUsageLeaseTestStore) RenewRealtimeUsageClaim(_ context.Context, _, _, owner string, lease time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner != s.claimOwner || !time.Now().Before(s.claimExpires) {
		return false, nil
	}
	s.claimExpires = time.Now().Add(lease)
	s.renewals.Add(1)
	return true, nil
}

func (s *realtimeUsageLeaseTestStore) MarkRealtimeUsageSucceeded(_ context.Context, _, _, owner string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner != s.claimOwner || !time.Now().Before(s.claimExpires) {
		return false, nil
	}
	s.claimExpires = time.Now().Add(ttl)
	return true, nil
}

func (s *realtimeUsageLeaseTestStore) CommitSucceededRealtimeUsages(_ context.Context, _ string, _ time.Duration) (int, error) {
	return 0, nil
}

func (s *realtimeLifecycleStore) SaveRealtimeCall(_ context.Context, record *RealtimeCallRecord, _ time.Duration) error {
	if s.saveFailures.Add(-1) >= 0 {
		return errors.New("temporary realtime ledger save failure")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *record
	s.record = &copy
	return nil
}

func (s *realtimeLifecycleStore) UpdateRealtimeEmployeeTurn(_ context.Context, callHash string, descriptor RealtimeEmployeeTurnReservation, add bool) error {
	if s.saveFailures.Add(-1) >= 0 {
		return errors.New("temporary realtime ledger save failure")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash {
		return ErrRealtimeCallNotFound
	}
	if add && (s.record.Controller == RealtimeControllerFinalizing || s.record.Controller == RealtimeControllerClosed) {
		return ErrRealtimeControllerChanged
	}
	updated := s.record.EmployeeTurnReservations[:0]
	found := false
	for _, existing := range s.record.EmployeeTurnReservations {
		if existing.ResponseID == descriptor.ResponseID {
			found = true
			if add {
				updated = append(updated, existing)
			}
			continue
		}
		updated = append(updated, existing)
	}
	if add && !found {
		updated = append(updated, descriptor)
	}
	s.record.EmployeeTurnReservations = updated
	return nil
}

func (s *realtimeLifecycleStore) RestoreRealtimeEmployeeTurnForFinalization(_ context.Context, callHash, owner string, descriptor RealtimeEmployeeTurnReservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash {
		return ErrRealtimeCallNotFound
	}
	if s.record.Controller != RealtimeControllerFinalizing || s.finalizer != owner {
		return ErrRealtimeControllerChanged
	}
	for _, existing := range s.record.EmployeeTurnReservations {
		if existing.ResponseID == descriptor.ResponseID {
			return nil
		}
	}
	s.record.EmployeeTurnReservations = append(s.record.EmployeeTurnReservations, descriptor)
	return nil
}

func (s *realtimeLifecycleStore) BindRealtimeEmployeeTurn(_ context.Context, callHash, provisionalID string, descriptor RealtimeEmployeeTurnReservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.Controller == RealtimeControllerFinalizing || s.record.Controller == RealtimeControllerClosed {
		return ErrRealtimeControllerChanged
	}
	updated := s.record.EmployeeTurnReservations[:0]
	found := false
	for _, existing := range s.record.EmployeeTurnReservations {
		if existing.ResponseID == provisionalID {
			updated = append(updated, descriptor)
			found = true
			continue
		}
		updated = append(updated, existing)
	}
	if !found {
		return ErrRealtimeCallNotFound
	}
	s.record.EmployeeTurnReservations = updated
	return nil
}

func TestRestoreRealtimeEmployeeTurnsCleansTerminalLedgerStates(t *testing.T) {
	for _, status := range []string{"committed", "settled", "released"} {
		t.Run(status, func(t *testing.T) {
			store := &realtimeLifecycleStore{}
			repo := &realtimeEmployeeTurnReservationRepo{}
			record := &RealtimeCallRecord{
				CallID: "call-terminal-" + status, CallHash: hashRealtimeCallID("call-terminal-" + status),
				Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour),
			}
			descriptor := RealtimeEmployeeTurnReservation{ResponseID: "response-1", RequestID: "realtime-turn-" + status, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1.25}
			record.EmployeeTurnReservations = []RealtimeEmployeeTurnReservation{descriptor}
			require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
			repo.reserved = map[string]EnterpriseEmployeeUsageReservationResult{descriptor.RequestID: {
				RequestID: descriptor.RequestID, RequestFingerprint: descriptor.RequestFingerprint, SourceAPIKeyID: descriptor.SourceAPIKeyID,
				ReservedUSD: descriptor.ReservedUSD, Status: status,
			}}

			svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}
			require.True(t, svc.restoreRealtimeReservation(record))
			persisted, err := store.GetRealtimeCall(context.Background(), record.CallHash)
			require.NoError(t, err)
			require.Empty(t, persisted.EmployeeTurnReservations)
		})
	}
}

func TestRestoreRealtimeEmployeeTurnsRetriesLedgerRemovalFailure(t *testing.T) {
	store := &realtimeLifecycleStore{}
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{CallID: "call-removal-retry", CallHash: hashRealtimeCallID("call-removal-retry"), Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	descriptor := RealtimeEmployeeTurnReservation{ResponseID: "response-1", RequestID: "realtime-turn-removal-retry", RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1.25}
	record.EmployeeTurnReservations = []RealtimeEmployeeTurnReservation{descriptor}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	repo.reserved = map[string]EnterpriseEmployeeUsageReservationResult{descriptor.RequestID: {
		RequestID: descriptor.RequestID, RequestFingerprint: descriptor.RequestFingerprint, SourceAPIKeyID: descriptor.SourceAPIKeyID,
		ReservedUSD: descriptor.ReservedUSD, Status: "settled",
	}}
	store.saveFailures.Store(1)

	svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}
	require.False(t, svc.restoreRealtimeReservation(record))
	require.True(t, svc.restoreRealtimeReservation(record))
	persisted, err := store.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Empty(t, persisted.EmployeeTurnReservations)
}

func TestFinalizeOfficialRealtimeCallConservativelySettlesActiveEmployeeTurn(t *testing.T) {
	store := &realtimeLifecycleStore{}
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{CallID: "call-missing-terminal", CallHash: hashRealtimeCallID("call-missing-terminal"), AccountID: 11, APIKeyID: 42, UserID: 33, LeaseID: "lease-missing-terminal", Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo, concurrencyService: NewConcurrencyService(&realtimeLifecycleConcurrencyCache{})}
	requestID := ""
	start := func(responseID, _ string) (*InflightReservation, error) {
		requestID = "realtime-call:" + record.CallHash + ":" + responseID
		result, err := repo.ReserveEnterpriseEmployeeUsage(context.Background(), &EnterpriseEmployeeUsageReservationCommand{
			RequestID: requestID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1.25,
		})
		if err != nil {
			return nil, err
		}
		return AttachEnterpriseEmployeeUsageReservation(nil, newEnterpriseEmployeeUsageReservation(repo, result)), nil
	}
	require.NoError(t, svc.PrepareOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start))
	require.NoError(t, svc.beginRealtimeEmployeeTurn(record, "response-missing", "gpt-realtime", start))
	var settlements atomic.Int32
	svc.SetOfficialRealtimeConservativeTurnCallback(func(got *RealtimeCallRecord, responseID string) error {
		require.Equal(t, record.CallHash, got.CallHash)
		require.Equal(t, "response-missing", responseID)
		settlements.Add(1)
		repo.mu.Lock()
		result := repo.reserved[requestID]
		result.Status = "settled"
		repo.reserved[result.RequestID] = result
		repo.mu.Unlock()
		return nil
	})

	svc.finalizeOfficialRealtimeCall(record)
	persisted, err := store.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerClosed, persisted.Controller)
	require.Empty(t, persisted.EmployeeTurnReservations)
	require.Equal(t, int32(1), settlements.Load())
}

func TestRealtimeEmployeeTurnRequiresPreflightBeforeProviderResponse(t *testing.T) {
	store := &realtimeLifecycleStore{}
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{CallID: "call-preflight", CallHash: hashRealtimeCallID("call-preflight"), Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}
	var admissions atomic.Int32
	start := func(responseID, _ string) (*InflightReservation, error) {
		admissions.Add(1)
		result, err := repo.ReserveEnterpriseEmployeeUsage(context.Background(), &EnterpriseEmployeeUsageReservationCommand{
			RequestID: "realtime-call:" + record.CallHash + ":" + responseID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1.25,
		})
		if err != nil {
			return nil, err
		}
		return AttachEnterpriseEmployeeUsageReservation(nil, newEnterpriseEmployeeUsageReservation(repo, result)), nil
	}

	require.ErrorIs(t, svc.beginRealtimeEmployeeTurn(record, "response-1", "gpt-realtime", start), ErrRealtimeUnavailable)
	require.Equal(t, int32(0), admissions.Load(), "response.created must never perform a new employee admission")
	require.NoError(t, svc.PrepareOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start))
	require.NoError(t, svc.beginRealtimeEmployeeTurn(record, "response-1", "gpt-realtime", start))
	require.Equal(t, int32(1), admissions.Load())
}

func TestRealtimeEmployeeTurnReusesInitialPreflightAndReplenishesAfterTerminalResponse(t *testing.T) {
	store := &realtimeLifecycleStore{}
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{CallID: "call-explicit-turns", CallHash: hashRealtimeCallID("call-explicit-turns"), Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}
	var admissions atomic.Int32
	start := func(responseID, _ string) (*InflightReservation, error) {
		admissions.Add(1)
		result, err := repo.ReserveEnterpriseEmployeeUsage(context.Background(), &EnterpriseEmployeeUsageReservationCommand{
			RequestID: "realtime-call:" + record.CallHash + ":" + responseID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1,
		})
		if err != nil {
			return nil, err
		}
		return AttachEnterpriseEmployeeUsageReservation(nil, newEnterpriseEmployeeUsageReservation(repo, result)), nil
	}

	require.NoError(t, svc.PrepareOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start))
	require.NoError(t, svc.EnsureOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start), "首个 explicit response 必须复用初始 preflight")
	require.Equal(t, int32(1), admissions.Load())
	require.NoError(t, svc.BeginOfficialRealtimeEmployeeTurn(record, "response-1", "gpt-realtime", start))
	require.NoError(t, svc.SettleOfficialRealtimeTurn(record, openaiwsv2.RelayTurnResult{RequestID: "response-1", RequestModel: "gpt-realtime"}, start, func(openaiwsv2.RelayTurnResult) error { return nil }))
	require.Equal(t, int32(2), admissions.Load(), "终态后必须为 data-channel 的下一次 explicit response 预留")
	require.NoError(t, svc.BeginOfficialRealtimeEmployeeTurn(record, "response-2", "gpt-realtime", start))
}

func TestEnsureOfficialRealtimeEmployeeTurnCoalescesConcurrentAdmissions(t *testing.T) {
	store := &realtimeLifecycleStore{}
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{CallID: "call-concurrent-preflight", CallHash: hashRealtimeCallID("call-concurrent-preflight"), Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}

	started := make(chan struct{})
	secondAdmission := make(chan struct{})
	release := make(chan struct{})
	var admissions atomic.Int32
	start := func(responseID, _ string) (*InflightReservation, error) {
		if admissions.Add(1) != 1 {
			close(secondAdmission)
			return nil, errors.New("employee allocation only permits one turn")
		}
		close(started)
		<-release
		result, err := repo.ReserveEnterpriseEmployeeUsage(context.Background(), &EnterpriseEmployeeUsageReservationCommand{
			RequestID: "realtime-call:" + record.CallHash + ":" + responseID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1,
		})
		if err != nil {
			return nil, err
		}
		return AttachEnterpriseEmployeeUsageReservation(nil, newEnterpriseEmployeeUsageReservation(repo, result)), nil
	}

	errs := make(chan error, 2)
	go func() { errs <- svc.EnsureOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start) }()
	<-started
	go func() { errs <- svc.EnsureOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start) }()
	select {
	case <-secondAdmission:
		close(release)
		require.FailNow(t, "concurrent Ensure started a second employee admission")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	require.Equal(t, int32(1), admissions.Load(), "并发 Ensure 必须只创建一笔员工额度预留")
	repo.mu.Lock()
	require.Len(t, repo.reserved, 1)
	repo.mu.Unlock()
}

func TestEnsureOfficialRealtimeEmployeeTurnRetriesSameDescriptorAfterPersistenceFailure(t *testing.T) {
	store := &realtimeLifecycleStore{}
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{CallID: "call-preflight-retry", CallHash: hashRealtimeCallID("call-preflight-retry"), Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	store.saveFailures.Store(1)
	svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}

	var admissions atomic.Int32
	start := func(responseID, _ string) (*InflightReservation, error) {
		admissions.Add(1)
		result, err := repo.ReserveEnterpriseEmployeeUsage(context.Background(), &EnterpriseEmployeeUsageReservationCommand{
			RequestID: "realtime-call:" + record.CallHash + ":" + responseID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1,
		})
		if err != nil {
			return nil, err
		}
		return AttachEnterpriseEmployeeUsageReservation(nil, newEnterpriseEmployeeUsageReservation(repo, result)), nil
	}

	require.Error(t, svc.EnsureOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start))
	turns := svc.realtimeEmployeeTurns(record.CallHash)
	turns.mu.Lock()
	require.Empty(t, turns.prepared, "未持久化 descriptor 不能作为 admission-ready")
	pendingID := turns.pending
	pending := turns.descriptors[pendingID]
	turns.mu.Unlock()
	require.NotEmpty(t, pendingID)
	require.NoError(t, svc.EnsureOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start))
	require.Equal(t, int32(1), admissions.Load(), "重试必须复用同一员工额度预留")

	persisted, err := store.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, []RealtimeEmployeeTurnReservation{pending}, persisted.EmployeeTurnReservations)
	require.NoError(t, svc.BeginOfficialRealtimeEmployeeTurn(record, "response-after-retry", "gpt-realtime", start))
	persisted, err = store.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, []RealtimeEmployeeTurnReservation{{
		ResponseID: "response-after-retry", RequestID: pending.RequestID, RequestFingerprint: pending.RequestFingerprint,
		SourceAPIKeyID: pending.SourceAPIKeyID, ReservedUSD: pending.ReservedUSD,
	}}, persisted.EmployeeTurnReservations)
}

func TestFinalizeOfficialRealtimeCallWaitsForPreparingEmployeeTurn(t *testing.T) {
	store := &realtimeLifecycleStore{}
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{CallID: "call-finalize-during-preparation", CallHash: hashRealtimeCallID("call-finalize-during-preparation"), Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}

	started := make(chan struct{})
	releaseStart := make(chan struct{})
	requestID := ""
	start := func(responseID, _ string) (*InflightReservation, error) {
		close(started)
		<-releaseStart
		requestID = "realtime-call:" + record.CallHash + ":" + responseID
		result, err := repo.ReserveEnterpriseEmployeeUsage(context.Background(), &EnterpriseEmployeeUsageReservationCommand{
			RequestID: requestID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1,
		})
		if err != nil {
			return nil, err
		}
		return AttachEnterpriseEmployeeUsageReservation(nil, newEnterpriseEmployeeUsageReservation(repo, result)), nil
	}

	prepared := make(chan error, 1)
	go func() { prepared <- svc.EnsureOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start) }()
	<-started
	finalized := make(chan struct{})
	go func() {
		svc.FinalizeOfficialRealtimeCall(record)
		close(finalized)
	}()
	turns := svc.realtimeEmployeeTurns(record.CallHash)
	require.Eventually(t, func() bool {
		turns.mu.Lock()
		closing := turns.closing
		turns.mu.Unlock()
		return closing
	}, time.Second, 10*time.Millisecond)
	select {
	case <-finalized:
		require.FailNow(t, "finalization completed before preparation cleanup")
	default:
	}

	close(releaseStart)
	require.ErrorIs(t, <-prepared, ErrRealtimeControllerChanged)
	select {
	case <-finalized:
	case <-time.After(time.Second):
		require.FailNow(t, "finalization did not wait for preparation cleanup")
	}

	persisted, err := store.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerClosed, persisted.Controller)
	require.Empty(t, persisted.EmployeeTurnReservations)
	restarted := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}
	require.True(t, restarted.restoreRealtimeEmployeeTurns(persisted))
	require.Nil(t, restarted.RealtimeEmployeeTurnReservation(persisted, "response-after-close"))
	repo.mu.Lock()
	require.Equal(t, 1, repo.releases[requestID])
	repo.mu.Unlock()
}

func TestEnsureOfficialRealtimeEmployeeTurnDoesNotAdmitAfterStaleControllerRead(t *testing.T) {
	baseStore := &realtimeLifecycleStore{}
	store := &staleRealtimeControllerStore{
		realtimeLifecycleStore: baseStore,
		lookupStarted:          make(chan struct{}),
		lookupRelease:          make(chan struct{}),
	}
	store.blockNextLookup.Store(true)
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{CallID: "call-stale-controller-read", CallHash: hashRealtimeCallID("call-stale-controller-read"), Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, baseStore.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}

	var admissions atomic.Int32
	start := func(responseID, _ string) (*InflightReservation, error) {
		admissions.Add(1)
		result, err := repo.ReserveEnterpriseEmployeeUsage(context.Background(), &EnterpriseEmployeeUsageReservationCommand{
			RequestID: "realtime-call:" + record.CallHash + ":" + responseID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1,
		})
		if err != nil {
			return nil, err
		}
		return AttachEnterpriseEmployeeUsageReservation(nil, newEnterpriseEmployeeUsageReservation(repo, result)), nil
	}

	prepared := make(chan error, 1)
	go func() { prepared <- svc.EnsureOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start) }()
	<-store.lookupStarted

	finalized := make(chan struct{})
	go func() {
		svc.FinalizeOfficialRealtimeCall(record)
		close(finalized)
	}()
	select {
	case <-finalized:
	case <-time.After(time.Second):
		require.FailNow(t, "过期 controller 读取被阻塞时，finalization 未完成")
	}
	close(store.lookupRelease)
	require.ErrorIs(t, <-prepared, ErrRealtimeControllerChanged)

	require.Equal(t, int32(0), admissions.Load(), "closed call 在读取到过期 controller 后不得准入员工回合")
	baseStore.mu.Lock()
	require.Equal(t, RealtimeControllerClosed, baseStore.record.Controller)
	require.Empty(t, baseStore.record.EmployeeTurnReservations)
	baseStore.mu.Unlock()
	_, exists := svc.realtimeEmployeeTurnReservations.Load(record.CallHash)
	require.False(t, exists, "cleanup 必须移除 closed call 的内存员工回合账本")
	repo.mu.Lock()
	require.Empty(t, repo.reserved)
	require.Empty(t, repo.releases)
	require.Empty(t, repo.renewals)
	repo.mu.Unlock()
}

func TestRealtimeWebSocketLedgerRetriesConservativeSettlement(t *testing.T) {
	store := &realtimeLifecycleStore{}
	svc := &OpenAIGatewayService{cache: store}
	var released atomic.Int32
	identity := RealtimeCallIdentity{APIKeyID: 2, UserID: 3, Model: "gpt-realtime", PricingAt: time.Now(), InboundEndpoint: "/v1/realtime", ReservationUSD: 1}
	record, err := svc.RegisterOfficialRealtimeWebSocketCall(context.Background(), &Account{ID: 1}, identity, func() { released.Add(1) })
	require.NoError(t, err)
	var attempts atomic.Int32
	svc.SetOfficialRealtimeLifecycleCallbacks(nil, func(*RealtimeCallRecord) error {
		if attempts.Add(1) == 1 {
			return errors.New("temporary usage failure")
		}
		return nil
	})

	svc.FinalizeOfficialRealtimeCall(record)
	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerFinalizing, controller)
	svc.FinalizeOfficialRealtimeCall(record)
	controller, err = store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerClosed, controller)
	require.Equal(t, int32(1), released.Load())
}

func TestCancelRejectedRealtimeTurnStopsResponseAndFinalizesCall(t *testing.T) {
	store := &realtimeLifecycleStore{}
	record := &RealtimeCallRecord{CallID: "call-rejected-turn", CallHash: hashRealtimeCallID("call-rejected-turn"), Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store}
	upstream := &realtimeRecordingFrameConn{}
	admissionErr := errors.New("weekly employee allocation exhausted")

	require.ErrorIs(t, svc.cancelRejectedRealtimeTurn(upstream, record, "response-rejected", admissionErr), admissionErr)
	require.Len(t, upstream.writes, 1)
	require.JSONEq(t, `{"type":"response.cancel","response_id":"response-rejected"}`, string(upstream.writes[0]))
	persisted, err := store.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerClosed, persisted.Controller)
}

func TestUnboundRealtimePreflightSurvivesLedgerRetryAndReleasesWithoutUsage(t *testing.T) {
	store := &realtimeLifecycleStore{}
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{CallID: "call-ledger-retry", CallHash: hashRealtimeCallID("call-ledger-retry"), AccountID: 11, APIKeyID: 42, UserID: 33, LeaseID: "lease-ledger-retry", Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))

	svc := &OpenAIGatewayService{cache: store, usageBillingRepo: repo, concurrencyService: NewConcurrencyService(&realtimeLifecycleConcurrencyCache{})}
	store.saveFailures.Store(1)
	requestID := ""
	err := svc.PrepareOfficialRealtimeEmployeeTurn(record, "gpt-realtime", func(responseID, _ string) (*InflightReservation, error) {
		requestID = "realtime-call:" + record.CallHash + ":" + responseID
		result, reserveErr := repo.ReserveEnterpriseEmployeeUsage(context.Background(), &EnterpriseEmployeeUsageReservationCommand{
			RequestID: requestID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1.25,
		})
		if reserveErr != nil {
			return nil, reserveErr
		}
		return AttachEnterpriseEmployeeUsageReservation(nil, newEnterpriseEmployeeUsageReservation(repo, result)), nil
	})
	require.Error(t, err, "Redis descriptor first write is deliberately unavailable")
	require.NotEmpty(t, svc.realtimeEmployeeTurns(record.CallHash).handles, "DB reservation must remain owned until its ledger identity persists")

	svc.finalizeOfficialRealtimeCall(record)
	persisted, getErr := store.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, getErr)
	require.Equal(t, RealtimeControllerClosed, persisted.Controller)
	require.Empty(t, persisted.EmployeeTurnReservations)
	repo.mu.Lock()
	require.Equal(t, 1, repo.releases[requestID])
	repo.mu.Unlock()
}

func TestRealtimeEmployeeTurnReservationSurvivesRecoveryAndReleasesOnce(t *testing.T) {
	store := &realtimeLifecycleStore{}
	repo := &realtimeEmployeeTurnReservationRepo{}
	record := &RealtimeCallRecord{
		CallID: "call-employee-turn", CallHash: hashRealtimeCallID("call-employee-turn"),
		Controller: RealtimeControllerPending, ExpiresAt: time.Now().Add(time.Hour),
	}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))

	service := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}
	requestID := ""
	start := func(responseID, _ string) (*InflightReservation, error) {
		requestID = "realtime-call:" + record.CallHash + ":" + responseID
		employee, err := repo.ReserveEnterpriseEmployeeUsage(context.Background(), &EnterpriseEmployeeUsageReservationCommand{
			RequestID: requestID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1.25,
		})
		if err != nil {
			return nil, err
		}
		return AttachEnterpriseEmployeeUsageReservation(nil, newEnterpriseEmployeeUsageReservation(repo, employee)), nil
	}
	require.NoError(t, service.PrepareOfficialRealtimeEmployeeTurn(record, "gpt-realtime", start))
	require.NoError(t, service.beginRealtimeEmployeeTurn(record, "response-1", "gpt-realtime", start))
	persisted, err := store.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, []RealtimeEmployeeTurnReservation{{
		ResponseID: "response-1", RequestID: requestID, RequestFingerprint: "fingerprint", SourceAPIKeyID: 42, ReservedUSD: 1.25,
	}}, persisted.EmployeeTurnReservations)

	restarted := &OpenAIGatewayService{cache: store, usageBillingRepo: repo}
	require.True(t, restarted.restoreRealtimeReservation(persisted))
	require.NotNil(t, restarted.RealtimeEmployeeTurnReservation(persisted, "response-1"))

	restarted.completeRealtimeEmployeeTurn(persisted, "response-1")
	persisted, err = store.GetRealtimeCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Empty(t, persisted.EmployeeTurnReservations)
	repo.mu.Lock()
	require.Equal(t, 1, repo.releases[requestID])
	repo.mu.Unlock()
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
	if s.controllerLookupErrors.Add(-1) >= 0 {
		return "", errors.New("temporary controller lookup failure")
	}
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

func (s *realtimeLifecycleStore) ClaimRealtimeFinalization(_ context.Context, callHash, owner string, _, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.Controller == RealtimeControllerClosed || (s.record.Controller == RealtimeControllerFinalizing && s.finalizer != "" && s.finalizer != owner) {
		return false, nil
	}
	s.record.Controller = RealtimeControllerFinalizing
	s.finalizer = owner
	return true, nil
}

func (s *realtimeLifecycleStore) ReleaseRealtimeFinalization(_ context.Context, callHash, owner string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.Controller != RealtimeControllerFinalizing || s.finalizer != owner {
		return false, nil
	}
	s.finalizer = ""
	return true, nil
}

func (s *realtimeLifecycleStore) CommitRealtimeFinalization(_ context.Context, callHash, owner string, _ time.Duration) (bool, error) {
	if s.finalizerCommitFailures.Add(-1) >= 0 {
		return false, errors.New("temporary finalizer commit failure")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.Controller != RealtimeControllerFinalizing || s.finalizer != owner {
		return false, nil
	}
	s.record.Controller = RealtimeControllerClosed
	s.finalizer = ""
	s.record.CleanupPending = true
	if s.finalizerCommitAfterSet.Swap(false) {
		return false, errors.New("transport lost after finalization commit")
	}
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
	s.usageCommitCalls.Add(1)
	if s.usageCommitFailures.Add(-1) >= 0 {
		return false, errors.New("temporary usage commit failure")
	}
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
	delete(s.usage, key)
	delete(s.succeeded, key)
	return true, nil
}

func (s *realtimeLifecycleStore) MarkRealtimeUsageSucceeded(_ context.Context, callHash, responseID, _ string, _ time.Duration) (bool, error) {
	s.markSucceededCalls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	key := callHash + ":" + responseID
	if _, ok := s.succeeded[key]; ok {
		return true, nil
	}
	if _, ok := s.usage[key]; !ok {
		return false, nil
	}
	if s.succeeded == nil {
		s.succeeded = make(map[string]struct{})
	}
	s.succeeded[key] = struct{}{}
	return true, nil
}

func (s *realtimeLifecycleStore) RenewRealtimeUsageClaim(_ context.Context, callHash, responseID, _ string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, pending := s.usage[callHash+":"+responseID]
	return pending, nil
}

func (s *realtimeLifecycleStore) CommitSucceededRealtimeUsages(_ context.Context, callHash string, _ time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	committed := 0
	for key := range s.succeeded {
		if len(key) <= len(callHash) || key[:len(callHash)+1] != callHash+":" {
			continue
		}
		if s.committed == nil {
			s.committed = make(map[string]struct{})
		}
		s.committed[key] = struct{}{}
		delete(s.succeeded, key)
		delete(s.usage, key)
		committed++
	}
	return committed, nil
}

func (s *realtimeLifecycleStore) MarkRealtimeCleanupCompleted(_ context.Context, callHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.Controller != RealtimeControllerClosed || !s.record.CleanupPending {
		return false, nil
	}
	s.record.CleanupPending = false
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
	delete(s.succeeded, key)
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

func (s *realtimeLifecycleStore) HasPendingRealtimeUsage(_ context.Context, callHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.usage {
		if len(key) > len(callHash) && key[:len(callHash)+1] == callHash+":" {
			if _, committed := s.committed[key]; !committed {
				return true, nil
			}
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
	if s.record == nil || (s.record.Controller == RealtimeControllerClosed && !s.record.CleanupPending) {
		return nil, nil
	}
	copy := *s.record
	return []*RealtimeCallRecord{&copy}, nil
}

func (s *realtimeLifecycleStore) ResetRealtimeControllerForRecovery(_ context.Context, callHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record == nil || s.record.CallHash != callHash || s.record.Controller == RealtimeControllerClosed || s.record.Controller == RealtimeControllerFinalizing {
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

func TestFinalizeOfficialRealtimeCallWaitsForPendingActualUsage(t *testing.T) {
	record := &RealtimeCallRecord{CallID: "call_pending", CallHash: hashRealtimeCallID("call_pending"), AccountID: 11, APIKeyID: 22, UserID: 33, LeaseID: "lease-pending", ReservationUSD: 0.9, Controller: RealtimeControllerPending}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	concurrency := &realtimeLifecycleConcurrencyCache{}
	svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(concurrency)}
	var settlements atomic.Int32
	svc.SetOfficialRealtimeLifecycleCallbacks(nil, func(*RealtimeCallRecord) error { settlements.Add(1); return nil })

	claimed, err := store.ClaimRealtimeUsage(context.Background(), record.CallHash, "actual", "owner", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	svc.finalizeOfficialRealtimeCall(record)
	require.Equal(t, int32(0), settlements.Load(), "pending actual usage must block conservative settlement")
	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerFinalizing, controller)

	committed, err := store.CommitRealtimeUsageClaim(context.Background(), record.CallHash, "actual", "owner", time.Hour)
	require.NoError(t, err)
	require.True(t, committed)
	svc.finalizeOfficialRealtimeCall(record)
	controller, err = store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerClosed, controller)
	require.Equal(t, int32(0), settlements.Load())
}

func TestFinalizeOfficialRealtimeCallRetriesConservativeSettlement(t *testing.T) {
	record := &RealtimeCallRecord{CallID: "call_retry", CallHash: hashRealtimeCallID("call_retry"), AccountID: 11, APIKeyID: 22, UserID: 33, LeaseID: "lease-retry", ReservationUSD: 0.9, Controller: RealtimeControllerPending}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	concurrency := &realtimeLifecycleConcurrencyCache{}
	svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(concurrency)}
	var attempts atomic.Int32
	svc.SetOfficialRealtimeLifecycleCallbacks(nil, func(*RealtimeCallRecord) error {
		if attempts.Add(1) == 1 {
			return errors.New("temporary billing failure")
		}
		return nil
	})

	svc.finalizeOfficialRealtimeCall(record)
	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerFinalizing, controller)
	concurrency.mu.Lock()
	releases := concurrency.releases
	concurrency.mu.Unlock()
	require.Equal(t, 0, releases)

	require.Eventually(t, func() bool {
		controller, err = store.GetRealtimeController(context.Background(), record.CallHash)
		return err == nil && controller == RealtimeControllerClosed
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, int32(2), attempts.Load())
	concurrency.mu.Lock()
	releases = concurrency.releases
	concurrency.mu.Unlock()
	require.Equal(t, 1, releases)
}

func TestFinalizeOfficialRealtimeCallRetriesFinalizationCommit(t *testing.T) {
	record := &RealtimeCallRecord{CallID: "call-finalization-commit", CallHash: hashRealtimeCallID("call-finalization-commit"), AccountID: 11, APIKeyID: 22, UserID: 33, LeaseID: "lease-finalization-commit", Controller: RealtimeControllerPending}
	store := &realtimeLifecycleStore{}
	store.finalizerCommitFailures.Store(1)
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	concurrency := &realtimeLifecycleConcurrencyCache{}
	svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(concurrency)}

	svc.finalizeOfficialRealtimeCall(record)
	require.Eventually(t, func() bool {
		controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
		return err == nil && controller == RealtimeControllerClosed
	}, 2*time.Second, 10*time.Millisecond)
	concurrency.mu.Lock()
	defer concurrency.mu.Unlock()
	require.Equal(t, 1, concurrency.releases, "终态提交成功前不得释放会话租约")
}

func TestFinalizeOfficialRealtimeCallCleansResourcesAfterUncertainCommit(t *testing.T) {
	record := &RealtimeCallRecord{CallID: "call-finalization-transport", CallHash: hashRealtimeCallID("call-finalization-transport"), AccountID: 11, APIKeyID: 22, UserID: 33, LeaseID: "lease-finalization-transport", Controller: RealtimeControllerPending}
	store := &realtimeLifecycleStore{}
	store.finalizerCommitAfterSet.Store(true)
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	concurrency := &realtimeLifecycleConcurrencyCache{}
	svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(concurrency)}
	var reservationReleases atomic.Int32
	svc.realtimeFinalizers.Store(record.CallHash, func() { reservationReleases.Add(1) })

	svc.finalizeOfficialRealtimeCall(record)
	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerClosed, controller)
	concurrency.mu.Lock()
	require.Equal(t, 1, concurrency.releases)
	concurrency.mu.Unlock()
	require.Equal(t, int32(1), reservationReleases.Load())
}

func TestRealtimeUsageBusinessSuccessRetriesIdempotencyCommit(t *testing.T) {
	record := &RealtimeCallRecord{CallID: "call-usage-commit", CallHash: hashRealtimeCallID("call-usage-commit"), Controller: RealtimeControllerPending}
	store := &realtimeLifecycleStore{}
	store.usageCommitFailures.Store(1)
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	svc := &OpenAIGatewayService{cache: store}
	claim, claimed, err := svc.claimRealtimeUsage(store, record.CallHash, "response-1")
	require.NoError(t, err)
	require.True(t, claimed)

	var businessCalls atomic.Int32
	businessCalls.Add(1) // 上游 RecordUsage 已成功；仅 Redis 幂等提交暂态失败。
	svc.commitRealtimeUsageClaim(record, "response-1", claim)
	require.Eventually(t, func() bool {
		used, err := store.HasCommittedRealtimeUsage(context.Background(), record.CallHash)
		return err == nil && used
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, int32(1), businessCalls.Load(), "重试不得再次执行业务扣费")
	require.Equal(t, int32(1), store.markSucceededCalls.Load(), "成功标记完成后重试只能继续提交 claim")
	require.Equal(t, int32(2), store.usageCommitCalls.Load())
}

func TestFinalizeOfficialRealtimeCallCommitsSucceededUsageBeforeConservativeSettlement(t *testing.T) {
	record := &RealtimeCallRecord{CallID: "call-succeeded-race", CallHash: hashRealtimeCallID("call-succeeded-race"), AccountID: 11, APIKeyID: 22, UserID: 33, LeaseID: "lease-succeeded-race", ReservationUSD: 0.9, Controller: RealtimeControllerPending}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	claimed, err := store.ClaimRealtimeUsage(context.Background(), record.CallHash, "actual", "owner", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	marked, err := store.MarkRealtimeUsageSucceeded(context.Background(), record.CallHash, "actual", "owner", time.Hour)
	require.NoError(t, err)
	require.True(t, marked)

	var settlements atomic.Int32
	svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(&realtimeLifecycleConcurrencyCache{})}
	svc.SetOfficialRealtimeLifecycleCallbacks(nil, func(*RealtimeCallRecord) error { settlements.Add(1); return nil })
	svc.finalizeOfficialRealtimeCall(record)

	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerClosed, controller)
	require.Equal(t, int32(0), settlements.Load(), "succeeded actual usage 不能触发保守结算")
}

type realtimeBillingOutcomeRepository struct {
	UsageBillingRepository
	hasOutcome bool
}

func (r *realtimeBillingOutcomeRepository) HasRealtimeBillingOutcome(context.Context, string) (bool, error) {
	return r.hasOutcome, nil
}

func TestFinalizeOfficialRealtimeCallUsesPersistentBillingOutcomeAfterRedisMarkerLoss(t *testing.T) {
	record := &RealtimeCallRecord{CallID: "call-persistent-outcome", CallHash: hashRealtimeCallID("call-persistent-outcome"), AccountID: 11, APIKeyID: 22, UserID: 33, LeaseID: "lease-persistent-outcome", ReservationUSD: 0.9, Controller: RealtimeControllerPending}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	var settlements atomic.Int32
	svc := &OpenAIGatewayService{
		cache:              store,
		concurrencyService: NewConcurrencyService(&realtimeLifecycleConcurrencyCache{}),
		usageBillingRepo:   &realtimeBillingOutcomeRepository{hasOutcome: true},
	}
	svc.SetOfficialRealtimeLifecycleCallbacks(nil, func(*RealtimeCallRecord) error { settlements.Add(1); return nil })

	svc.finalizeOfficialRealtimeCall(record)

	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerClosed, controller)
	require.Equal(t, int32(0), settlements.Load(), "持久化实际账务结果必须阻止保守结算")
}

func TestFinalizeOfficialRealtimeCallWaitsForPendingUsageAlongsidePersistentBillingOutcome(t *testing.T) {
	record := &RealtimeCallRecord{CallID: "call-outcome-pending", CallHash: hashRealtimeCallID("call-outcome-pending"), AccountID: 11, APIKeyID: 22, UserID: 33, LeaseID: "lease-outcome-pending", ReservationUSD: 0.9, Controller: RealtimeControllerPending}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	concurrency := &realtimeLifecycleConcurrencyCache{}
	var reservationReleases atomic.Int32
	svc := &OpenAIGatewayService{
		cache:              store,
		concurrencyService: NewConcurrencyService(concurrency),
		usageBillingRepo:   &realtimeBillingOutcomeRepository{hasOutcome: true},
	}
	svc.realtimeFinalizers.Store(record.CallHash, func() { reservationReleases.Add(1) })

	claimed, err := store.ClaimRealtimeUsage(context.Background(), record.CallHash, "pending", "owner-pending", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)

	svc.finalizeOfficialRealtimeCall(record)

	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerFinalizing, controller, "持久化 outcome 不得越过同一 call 的 pending usage")
	concurrency.mu.Lock()
	require.Equal(t, 0, concurrency.releases)
	concurrency.mu.Unlock()
	require.Equal(t, int32(0), reservationReleases.Load())
}

func TestRecordRealtimeUsageRenewsClaimDuringLongBusinessCall(t *testing.T) {
	oldRefreshInterval := realtimeLeaseRefreshInterval
	oldClaimLease := realtimeReplayClaimLease
	realtimeLeaseRefreshInterval = 5 * time.Millisecond
	realtimeReplayClaimLease = 12 * time.Millisecond
	t.Cleanup(func() {
		realtimeLeaseRefreshInterval = oldRefreshInterval
		realtimeReplayClaimLease = oldClaimLease
	})

	record := &RealtimeCallRecord{CallID: "call-long-billing", CallHash: hashRealtimeCallID("call-long-billing"), Controller: RealtimeControllerPending}
	store := &realtimeUsageLeaseTestStore{realtimeLifecycleStore: &realtimeLifecycleStore{}}
	svc := &OpenAIGatewayService{cache: store}
	var businessCalls atomic.Int32

	err := svc.recordRealtimeUsage(record, store, openaiwsv2.RelayTurnResult{RequestID: "response-long-billing"}, func(openaiwsv2.RelayTurnResult) error {
		businessCalls.Add(1)
		time.Sleep(35 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, store.renewals.Load(), int32(0))
	used, err := store.HasCommittedRealtimeUsage(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.True(t, used, "业务调用超过 claim TTL 后仍应由原 owner 完成幂等提交")
	require.Equal(t, int32(1), businessCalls.Load())
}

func TestFinalizeOfficialRealtimeCallWaitsForPendingUsageAlongsideCommittedUsage(t *testing.T) {
	record := &RealtimeCallRecord{CallID: "call-mixed-usage", CallHash: hashRealtimeCallID("call-mixed-usage"), AccountID: 11, APIKeyID: 22, UserID: 33, LeaseID: "lease-mixed-usage", ReservationUSD: 0.9, Controller: RealtimeControllerPending}
	store := &realtimeLifecycleStore{}
	require.NoError(t, store.SaveRealtimeCall(context.Background(), record, time.Hour))
	concurrency := &realtimeLifecycleConcurrencyCache{}
	svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(concurrency)}

	claimed, err := store.ClaimRealtimeUsage(context.Background(), record.CallHash, "committed", "owner-committed", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	committed, err := store.CommitRealtimeUsageClaim(context.Background(), record.CallHash, "committed", "owner-committed", time.Hour)
	require.NoError(t, err)
	require.True(t, committed)
	claimed, err = store.ClaimRealtimeUsage(context.Background(), record.CallHash, "pending", "owner-pending", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)

	svc.finalizeOfficialRealtimeCall(record)
	controller, err := store.GetRealtimeController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, RealtimeControllerFinalizing, controller, "任一 pending usage 必须优先阻止关闭")
	concurrency.mu.Lock()
	require.Equal(t, 0, concurrency.releases)
	concurrency.mu.Unlock()
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
	svc.RecoverOfficialRealtimeCalls()

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
