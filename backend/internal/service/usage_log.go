package service

import (
	"fmt"
	"strings"
	"time"
)

const (
	BillingTypeBalance      int8 = 0 // 钱包余额
	BillingTypeSubscription int8 = 1 // 订阅套餐
)

type RequestType int16

const (
	RequestTypeUnknown      RequestType = 0
	RequestTypeSync         RequestType = 1
	RequestTypeStream       RequestType = 2
	RequestTypeWSV2         RequestType = 3
	RequestTypeCyberBlocked RequestType = 4 // cyber_policy 命中（透传但被上游安全策略拒绝）
	RequestTypeLive         RequestType = 5
)

func (t RequestType) IsValid() bool {
	switch t {
	case RequestTypeUnknown, RequestTypeSync, RequestTypeStream, RequestTypeWSV2, RequestTypeCyberBlocked, RequestTypeLive:
		return true
	default:
		return false
	}
}

func (t RequestType) Normalize() RequestType {
	if t.IsValid() {
		return t
	}
	return RequestTypeUnknown
}

func (t RequestType) String() string {
	switch t.Normalize() {
	case RequestTypeSync:
		return "sync"
	case RequestTypeStream:
		return "stream"
	case RequestTypeWSV2:
		return "ws_v2"
	case RequestTypeCyberBlocked:
		return "cyber"
	case RequestTypeLive:
		return "live"
	default:
		return "unknown"
	}
}

func RequestTypeFromInt16(v int16) RequestType {
	return RequestType(v).Normalize()
}

func ParseUsageRequestType(value string) (RequestType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "unknown":
		return RequestTypeUnknown, nil
	case "sync":
		return RequestTypeSync, nil
	case "stream":
		return RequestTypeStream, nil
	case "ws_v2":
		return RequestTypeWSV2, nil
	case "cyber":
		return RequestTypeCyberBlocked, nil
	case "live":
		return RequestTypeLive, nil
	default:
		return RequestTypeUnknown, fmt.Errorf("invalid request_type, allowed values: unknown, sync, stream, ws_v2, cyber, live")
	}
}

func RequestTypeFromLegacy(stream bool, openAIWSMode bool) RequestType {
	if openAIWSMode {
		return RequestTypeWSV2
	}
	if stream {
		return RequestTypeStream
	}
	return RequestTypeSync
}

func ApplyLegacyRequestFields(requestType RequestType, fallbackStream bool, fallbackOpenAIWSMode bool) (stream bool, openAIWSMode bool) {
	switch requestType.Normalize() {
	case RequestTypeSync:
		return false, false
	case RequestTypeStream:
		return true, false
	case RequestTypeWSV2:
		return true, true
	default:
		return fallbackStream, fallbackOpenAIWSMode
	}
}

type UsageLog struct {
	ID        int64
	UserID    int64
	APIKeyID  int64
	AccountID int64
	RequestID string
	Model     string
	// RequestedModel is the client-requested model name recorded for stable user/admin display.
	// Empty should be treated as Model for backward compatibility with historical rows.
	RequestedModel string
	// UpstreamModel is the actual model sent to the upstream provider after mapping.
	// Nil means no mapping was applied (requested model was used as-is).
	UpstreamModel *string
	// UpstreamResponseModel is the model declared by the successful upstream
	// response before client-facing model rewrites or protocol conversion.
	UpstreamResponseModel *string
	// UpstreamModelMismatch is nil when no upstream model was observed. Otherwise
	// it compares UpstreamResponseModel with the actual model sent upstream.
	UpstreamModelMismatch *bool
	// ChannelID 渠道 ID
	ChannelID *int64
	// ModelMappingChain 模型映射链，如 "a→b→c"
	ModelMappingChain *string
	// BillingTier 计费层级标签（per_request/image 模式）
	BillingTier *string
	// BillingMode 计费模式：token/image
	BillingMode *string
	// ServiceTier records the billable request tier, e.g. OpenAI "priority" / "flex"
	// or Anthropic "fast".
	ServiceTier *string
	// ReasoningEffort is the effective effort recorded for this request after
	// group policy rewriting and model-family remapping (e.g. max -> xhigh).
	// OpenAI: "low" / "medium" / "high" / "xhigh"; Claude: "low" / "medium" / "high" / "max".
	// Nil means not provided / not applicable.
	ReasoningEffort *string
	// RequestedReasoningEffort is the client-requested effort before mapping.
	// Nil means historical rows, or that no explicit/suffix-derived effort was observed.
	RequestedReasoningEffort *string
	// InboundEndpoint is the client-facing API endpoint path, e.g. /v1/chat/completions.
	InboundEndpoint *string
	// UpstreamEndpoint is the normalized upstream endpoint path, e.g. /v1/responses.
	UpstreamEndpoint *string

	GroupID        *int64
	SubscriptionID *int64

	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int

	CacheCreation5mTokens int `gorm:"column:cache_creation_5m_tokens"`
	CacheCreation1hTokens int `gorm:"column:cache_creation_1h_tokens"`

	ImageInputTokens  int
	ImageInputCost    float64
	ImageOutputTokens int
	ImageOutputCost   float64

	InputCost                 float64
	OutputCost                float64
	CacheCreationCost         float64
	CacheReadCost             float64
	TotalCost                 float64
	ActualCost                float64
	RateMultiplier            float64
	LongContextBillingApplied bool
	// AccountRateMultiplier 账号计费倍率快照（nil 表示历史数据，按 1.0 处理）
	AccountRateMultiplier *float64
	// AccountStatsCost 账号统计定价预计算费用（nil = 使用默认公式 total_cost × account_rate_multiplier）
	AccountStatsCost *float64

	BillingType        int8
	RequestType        RequestType
	Stream             bool
	OpenAIWSMode       bool
	NativeCompactionV2 bool
	DurationMs         *int
	FirstTokenMs       *int
	UserAgent          *string
	IPAddress          *string
	// SessionID is the explicit client-provided request correlation identifier
	// (e.g. the session_id / X-Session-Id headers). Nil when the client sent no
	// valid session header. It is never derived from prompt_cache_key or content.
	SessionID *string
	// UpstreamRequestID 是直接上游在响应头中声明的请求标识，只读账户
	// extra.upstream_request_id_header 指定的头；账户未指定头名、WS 轮次
	// 与上游没有该头的路径为 nil。
	UpstreamRequestID *string

	// Cache TTL Override 标记（管理员强制替换了缓存 TTL 计费）
	CacheTTLOverridden bool

	// 图片生成字段
	ImageCount         int
	ImageSize          *string
	ImageInputSize     *string
	ImageOutputSize    *string
	ImageSizeSource    *string
	ImageSizeBreakdown map[string]int
	MediaType          *string

	// 视频生成字段（Grok 视频按秒计费；video_count>0 的行不要求 image_size）
	VideoCount           int
	VideoResolution      *string
	VideoDurationSeconds *int

	CreatedAt time.Time

	// 企业积分归属仅保存请求时快照，不改变原生 usage_logs 结构与 actual_cost 协议。
	AttributionRequestAt           time.Time
	AttributionDailyWindowAnchor   *time.Time
	AttributionWeeklyWindowAnchor  *time.Time
	AttributionMonthlyWindowAnchor *time.Time
	EnterpriseAttributionCandidate bool
	EnterpriseAttribution          *EnterpriseUsageAttributionSnapshot

	User         *User
	APIKey       *APIKey
	Account      *Account
	Group        *Group
	Subscription *UserSubscription
}

type EnterpriseUsageAttributionSnapshot struct {
	EnterpriseID         int64
	SubscriptionID       int64
	EmployeeID           *int64
	AssignmentGeneration int64
	Classification       string
	RequestAt            time.Time
	DailyWindowAnchor    *time.Time
	WeeklyWindowAnchor   *time.Time
	MonthlyWindowAnchor  *time.Time
}

// EnterpriseUsageAttributionIdentity 是 API Key 认证成功时冻结的企业归属身份。
// 定价时刻和窗口锚点由实际 usage 产生处补齐，避免改变现有 PricingAt 契约。
type EnterpriseUsageAttributionIdentity struct {
	EnterpriseID             int64
	EnterpriseSubscriptionID int64
	UpstreamSubscriptionID   int64
	EmployeeID               *int64
	AssignmentGeneration     int64
	Classification           string
	// WindowAnchorsResolved 表示三个窗口锚点来自认证时的主库快照。
	// 为 false 时，调用方仍可用当前订阅对象作为兼容回退。
	WindowAnchorsResolved bool
	DailyWindowAnchor     *time.Time
	WeeklyWindowAnchor    *time.Time
	MonthlyWindowAnchor   *time.Time
}

func CloneEnterpriseUsageAttributionSnapshot(value *EnterpriseUsageAttributionSnapshot) *EnterpriseUsageAttributionSnapshot {
	if value == nil {
		return nil
	}
	copy := *value
	copy.EmployeeID = cloneUsageAttributionInt64(value.EmployeeID)
	copy.DailyWindowAnchor = cloneUsageAttributionTime(value.DailyWindowAnchor)
	copy.WeeklyWindowAnchor = cloneUsageAttributionTime(value.WeeklyWindowAnchor)
	copy.MonthlyWindowAnchor = cloneUsageAttributionTime(value.MonthlyWindowAnchor)
	return &copy
}

func SnapshotEnterpriseUsageAttribution(
	identity *EnterpriseUsageAttributionIdentity,
	requestAt time.Time,
	dailyWindowAnchor, weeklyWindowAnchor, monthlyWindowAnchor *time.Time,
) *EnterpriseUsageAttributionSnapshot {
	if identity == nil || identity.EnterpriseID <= 0 || identity.EnterpriseSubscriptionID <= 0 || requestAt.IsZero() {
		return nil
	}
	if identity.WindowAnchorsResolved {
		dailyWindowAnchor = identity.DailyWindowAnchor
		weeklyWindowAnchor = identity.WeeklyWindowAnchor
		monthlyWindowAnchor = identity.MonthlyWindowAnchor
	}
	return &EnterpriseUsageAttributionSnapshot{
		EnterpriseID:         identity.EnterpriseID,
		SubscriptionID:       identity.EnterpriseSubscriptionID,
		EmployeeID:           cloneUsageAttributionInt64(identity.EmployeeID),
		AssignmentGeneration: identity.AssignmentGeneration,
		Classification:       identity.Classification,
		RequestAt:            requestAt.UTC(),
		DailyWindowAnchor:    cloneUsageAttributionTime(dailyWindowAnchor),
		WeeklyWindowAnchor:   cloneUsageAttributionTime(weeklyWindowAnchor),
		MonthlyWindowAnchor:  cloneUsageAttributionTime(monthlyWindowAnchor),
	}
}

func ApplyEnterpriseUsageAttribution(
	log *UsageLog,
	apiKey *APIKey,
	subscription *UserSubscription,
	requestAt time.Time,
) {
	if log == nil {
		return
	}
	// 异步载体传入的快照已在创建请求认证时冻结，轮询时的 API Key 或订阅对象
	// 只能用于兼容旧路径，不能覆盖已经冻结的员工、代次和窗口。
	if log.EnterpriseAttribution != nil {
		applyEnterpriseUsageAttributionSnapshot(log, log.EnterpriseAttribution)
		return
	}
	if apiKey == nil {
		return
	}
	log.EnterpriseAttributionCandidate = apiKey.EnterpriseAttributionCandidate
	if subscription == nil {
		return
	}
	log.AttributionDailyWindowAnchor = cloneUsageAttributionTime(subscription.DailyWindowStart)
	log.AttributionWeeklyWindowAnchor = cloneUsageAttributionTime(subscription.WeeklyWindowStart)
	log.AttributionMonthlyWindowAnchor = cloneUsageAttributionTime(subscription.MonthlyWindowStart)
	identity := apiKey.EnterpriseAttributionIdentity
	if identity == nil || identity.UpstreamSubscriptionID != subscription.ID {
		return
	}
	log.EnterpriseAttribution = SnapshotEnterpriseUsageAttribution(
		identity,
		requestAt,
		log.AttributionDailyWindowAnchor,
		log.AttributionWeeklyWindowAnchor,
		log.AttributionMonthlyWindowAnchor,
	)
	if log.EnterpriseAttribution == nil {
		return
	}
	applyEnterpriseUsageAttributionSnapshot(log, log.EnterpriseAttribution)
}

func applyEnterpriseUsageAttributionSnapshot(log *UsageLog, snapshot *EnterpriseUsageAttributionSnapshot) {
	if log == nil || snapshot == nil {
		return
	}
	log.EnterpriseAttributionCandidate = true
	log.EnterpriseAttribution = CloneEnterpriseUsageAttributionSnapshot(snapshot)
	log.AttributionRequestAt = snapshot.RequestAt
	log.AttributionDailyWindowAnchor = cloneUsageAttributionTime(snapshot.DailyWindowAnchor)
	log.AttributionWeeklyWindowAnchor = cloneUsageAttributionTime(snapshot.WeeklyWindowAnchor)
	log.AttributionMonthlyWindowAnchor = cloneUsageAttributionTime(snapshot.MonthlyWindowAnchor)
}

func cloneUsageAttributionInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneUsageAttributionTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func (u *UsageLog) TotalTokens() int {
	return u.InputTokens + u.OutputTokens + u.CacheCreationTokens + u.CacheReadTokens
}

func (u *UsageLog) EffectiveRequestType() RequestType {
	if u == nil {
		return RequestTypeUnknown
	}
	if normalized := u.RequestType.Normalize(); normalized != RequestTypeUnknown {
		return normalized
	}
	return RequestTypeFromLegacy(u.Stream, u.OpenAIWSMode)
}

func (u *UsageLog) SyncRequestTypeAndLegacyFields() {
	if u == nil {
		return
	}
	requestType := u.EffectiveRequestType()
	u.RequestType = requestType
	u.Stream, u.OpenAIWSMode = ApplyLegacyRequestFields(requestType, u.Stream, u.OpenAIWSMode)
}
