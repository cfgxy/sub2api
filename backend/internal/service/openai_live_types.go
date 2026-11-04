package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	LiveControllerPending  = "pending"
	LiveControllerObserver = "observer"
	LiveControllerProxy    = "proxy"
	LiveControllerClosed   = "closed"
)

var (
	ErrLiveUnavailable       = errors.New("live is unavailable")
	ErrLiveConcurrencyFull   = errors.New("live concurrency is full")
	ErrLiveCallNotFound      = errors.New("live call not found")
	ErrLiveIdentityMismatch  = errors.New("live call identity mismatch")
	ErrLiveControllerChanged = errors.New("live controller changed")
)

type LiveAttestationUnavailableError struct {
	Reason string
}

func (e *LiveAttestationUnavailableError) Error() string {
	if e == nil || e.Reason == "" {
		return "Live attestation is unavailable"
	}
	return "Live attestation is unavailable: " + e.Reason
}

// LiveCallRequest 是两个下游创建协议归一后的请求。Session 不做结构改写。
type LiveCallRequest struct {
	SDP     string          `json:"sdp"`
	Session json.RawMessage `json:"session"`
}

type LiveCallIdentity struct {
	APIKeyID              int64
	UserID                int64
	GroupID               *int64
	SubscriptionID        *int64
	EnterpriseAttribution *EnterpriseUsageAttributionSnapshot
	UserAgent             string
	IPAddress             string
	InboundEndpoint       string
}

type LiveCallRecord struct {
	CallID          string
	CallHash        string
	AccountID       int64
	APIKeyID        int64
	UserID          int64
	GroupID         int64
	SubscriptionID  int64
	LeaseID         string
	Model           string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	Controller      string
	ControllerOwner string
	UserAgent       string
	IPAddress       string
	InboundEndpoint string
	// AttestationCiphertext 仅用于让同一会话的 Sideband 复用创建时的证明。
	AttestationCiphertext string
	EnterpriseAttribution *EnterpriseUsageAttributionSnapshot
}

type LiveCallCreated struct {
	SDP      []byte
	CallID   string
	Location string
	Account  *Account
}

// LiveCallStore 由 GatewayCache 的 Redis 实现可选提供，避免扩大旧缓存接口。
type LiveCallStore interface {
	SaveLiveCall(ctx context.Context, record *LiveCallRecord, ttl time.Duration) error
	GetLiveCall(ctx context.Context, callHash string) (*LiveCallRecord, error)
	ClaimLiveController(ctx context.Context, callHash, controller, owner string) (bool, error)
	ReleaseLiveController(ctx context.Context, callHash, owner string) (bool, error)
	GetLiveController(ctx context.Context, callHash string) (string, error)
	MarkLiveCallClosed(ctx context.Context, callHash string, ttl time.Duration) (bool, error)
}

type LiveConcurrencyCache interface {
	AcquireLiveLease(
		ctx context.Context,
		accountID int64,
		accountMax int,
		userID int64,
		userMax int,
		apiKeyID int64,
		leaseID string,
		replacingRegularSlots bool,
	) (bool, error)
	RefreshLiveLease(ctx context.Context, accountID, userID, apiKeyID int64, leaseID string) (bool, error)
	ReleaseLiveLease(ctx context.Context, accountID, userID, apiKeyID int64, leaseID string) error
}

const (
	RealtimeControllerPending    = "pending"
	RealtimeControllerObserver   = "observer"
	RealtimeControllerProxy      = "proxy"
	RealtimeControllerFinalizing = "finalizing"
	RealtimeControllerClosed     = "closed"
)

var (
	ErrRealtimeCallNotFound      = errors.New("realtime call not found")
	ErrRealtimeIdentityMismatch  = errors.New("realtime call identity mismatch")
	ErrRealtimeControllerChanged = errors.New("realtime controller changed")
	ErrRealtimeConcurrencyFull   = errors.New("realtime concurrency is full")
)

// RealtimeCallIdentity contains the immutable attribution captured when the
// authenticated WebRTC call is created. It is deliberately distinct from Live:
// this is an OpenAI Platform API call, not a ChatGPT Live session.
type RealtimeCallIdentity struct {
	APIKeyID              int64
	UserID                int64
	GroupID               *int64
	SubscriptionID        *int64
	EnterpriseAttribution *EnterpriseUsageAttributionSnapshot
	Model                 string
	PricingAt             time.Time
	UserAgent             string
	IPAddress             string
	InboundEndpoint       string
	ReservationUSD        float64
	ReservationRequestID  string
	ReservationHasBalance bool
	ReservationScopes     []string
}

// RealtimeCallRecord is the Redis-backed ownership and lifecycle ledger for a
// public OpenAI Platform Realtime Call. CallID itself is never used as a Redis
// key, preventing an upstream identifier from becoming part of the keyspace.
type RealtimeCallRecord struct {
	CallID                string
	CallHash              string
	AccountID             int64
	APIKeyID              int64
	UserID                int64
	GroupID               int64
	SubscriptionID        int64
	LeaseID               string
	Model                 string
	PricingAt             time.Time
	CreatedAt             time.Time
	ExpiresAt             time.Time
	Controller            string
	ControllerOwner       string
	UserAgent             string
	IPAddress             string
	InboundEndpoint       string
	EnterpriseAttribution *EnterpriseUsageAttributionSnapshot
	ReservationUSD        float64
	ReservationRequestID  string
	ReservationHasBalance bool
	ReservationScopes     []string
	// CleanupPending 表示终态已持久化，但并发 lease 与预留尚未完成释放。
	CleanupPending bool
}

// RealtimeCallStore is intentionally a separate cache capability so a Live
// record can never be mistaken for an official Platform Realtime call.
type RealtimeCallStore interface {
	SaveRealtimeCall(ctx context.Context, record *RealtimeCallRecord, ttl time.Duration) error
	GetRealtimeCall(ctx context.Context, callHash string) (*RealtimeCallRecord, error)
	ClaimRealtimeController(ctx context.Context, callHash, controller, owner string) (bool, error)
	ReleaseRealtimeController(ctx context.Context, callHash, owner string) (bool, error)
	GetRealtimeController(ctx context.Context, callHash string) (string, error)
	MarkRealtimeCallClosed(ctx context.Context, callHash string, ttl time.Duration) (bool, error)
	BeginRealtimeUsage(ctx context.Context, callHash, responseID string, ttl time.Duration) (bool, error)
	CommitRealtimeUsage(ctx context.Context, callHash, responseID string, ttl time.Duration) error
	ReleaseRealtimeUsage(ctx context.Context, callHash, responseID string) error
	BeginRealtimeWebhook(ctx context.Context, webhookID string, ttl time.Duration) (bool, error)
	CommitRealtimeWebhook(ctx context.Context, webhookID string, ttl time.Duration) error
	ReleaseRealtimeWebhook(ctx context.Context, webhookID string) error
}

// RealtimeUsageClaimStore upgrades the legacy idempotency keys to owner-bound
// short leases. A process that dies after a claim leaves work recoverable
// instead of suppressing retries for the whole replay window.
type RealtimeUsageClaimStore interface {
	ClaimRealtimeUsage(ctx context.Context, callHash, responseID, owner string, lease time.Duration) (bool, error)
	CommitRealtimeUsageClaim(ctx context.Context, callHash, responseID, owner string, ttl time.Duration) (bool, error)
	ReleaseRealtimeUsageClaim(ctx context.Context, callHash, responseID, owner string) (bool, error)
	HasCommittedRealtimeUsage(ctx context.Context, callHash string) (bool, error)
	HasPendingRealtimeUsage(ctx context.Context, callHash string) (bool, error)
	ClaimRealtimeWebhook(ctx context.Context, webhookID, owner string, lease time.Duration) (bool, error)
	CommitRealtimeWebhookClaim(ctx context.Context, webhookID, owner string, ttl time.Duration) (bool, error)
	ReleaseRealtimeWebhookClaim(ctx context.Context, webhookID, owner string) (bool, error)
}

// RealtimeUsageCompletionStore keeps the outcome of a completed business
// charge durable while Redis idempotency confirmation is retried.  The
// completion marker prevents another process from rerunning RecordUsage after
// the original owner has exited.
type RealtimeUsageCompletionStore interface {
	MarkRealtimeUsageSucceeded(ctx context.Context, callHash, responseID, owner string, ttl time.Duration) (bool, error)
	RenewRealtimeUsageClaim(ctx context.Context, callHash, responseID, owner string, lease time.Duration) (bool, error)
	CommitSucceededRealtimeUsages(ctx context.Context, callHash string, ttl time.Duration) (int, error)
}

// RealtimeConservativeUsageClaimStore 仅允许 finalizer owner 在 call finalizing
// 时申请合成的保守结算；该状态会拒绝普通终态 usage claim。
type RealtimeConservativeUsageClaimStore interface {
	ClaimRealtimeConservativeUsage(ctx context.Context, callHash, responseID, finalizerOwner, owner string, lease time.Duration) (bool, error)
}

// RealtimeFinalizationStore 持久化终态，直到实际 usage 或保守结算提交完成。
// 这能避免可重试的计费决策尚未持久化时提前释放额度。
type RealtimeFinalizationStore interface {
	// ClaimRealtimeFinalization grants a short, renewable owner lease while keeping
	// the call ledger itself for recordTTL. A later process may take over only after
	// the prior owner lease expires.
	ClaimRealtimeFinalization(ctx context.Context, callHash, owner string, lease, recordTTL time.Duration) (bool, error)
	ReleaseRealtimeFinalization(ctx context.Context, callHash, owner string) (bool, error)
	CommitRealtimeFinalization(ctx context.Context, callHash, owner string, ttl time.Duration) (bool, error)
}

// RealtimeCleanupStore 在 closed 账本的资源释放完成后持久化确认。这样即使
// CommitRealtimeFinalization 的响应和首次 controller 回读都丢失，重启恢复也能
// 识别并收束未完成的 cleanup。
type RealtimeCleanupStore interface {
	MarkRealtimeCleanupCompleted(ctx context.Context, callHash string) (bool, error)
}

// RealtimeCallRecoveryStore lists live records from the Redis-backed ledger at
// process startup. It is intentionally optional so non-Redis test caches keep
// the narrow RealtimeCallStore contract.
type RealtimeCallRecoveryStore interface {
	ListActiveRealtimeCalls(ctx context.Context) ([]*RealtimeCallRecord, error)
}

// RealtimeControllerRecoveryStore clears a stale in-process controller after a
// process restart. The durable call ledger remains the source of ownership.
type RealtimeControllerRecoveryStore interface {
	ResetRealtimeControllerForRecovery(ctx context.Context, callHash string) (bool, error)
}

// RealtimeConcurrencyCache owns the long lease that replaces the short HTTP
// admission slots for an actual WebRTC/SIP call.
type RealtimeConcurrencyCache interface {
	AcquireRealtimeLease(ctx context.Context, accountID int64, accountMax int, userID int64, userMax int, apiKeyID int64, leaseID string, replacingRegularSlots bool) (bool, error)
	RefreshRealtimeLease(ctx context.Context, accountID, userID, apiKeyID int64, leaseID string) (bool, error)
	ReleaseRealtimeLease(ctx context.Context, accountID, userID, apiKeyID int64, leaseID string) error
}
