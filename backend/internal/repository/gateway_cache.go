package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const stickySessionPrefix = "sticky_session:"
const openAIResponsesSessionWindowPrefix = "openai_responses_session_window:"
const liveCallPrefix = "live:call:"
const realtimeCallPrefix = "realtime:call:"
const realtimeUsagePrefix = "realtime:usage:"
const realtimeWebhookPrefix = "realtime:webhook:"

type gatewayCache struct {
	rdb *redis.Client
}

func NewGatewayCache(rdb *redis.Client) service.GatewayCache {
	return &gatewayCache{rdb: rdb}
}

// buildSessionKey 构建 session key，包含 groupID 实现分组隔离
// 格式: sticky_session:{groupID}:{sessionHash}
func buildSessionKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", stickySessionPrefix, groupID, sessionHash)
}

func buildOpenAIResponsesSessionWindowKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", openAIResponsesSessionWindowPrefix, groupID, sessionHash)
}

func (c *gatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	key := buildSessionKey(groupID, sessionHash)
	accountID, err := c.rdb.Get(ctx, key).Int64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, service.ErrStickySessionNotFound
		}
		return 0, err
	}
	return accountID, nil
}

func (c *gatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Set(ctx, key, accountID, ttl).Err()
}

func (c *gatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// DeleteSessionAccountID 删除粘性会话与账号的绑定关系。
// 当检测到绑定的账号不可用（如状态错误、禁用、不可调度等）时调用，
// 以便下次请求能够重新选择可用账号。
//
// DeleteSessionAccountID removes the sticky session binding for the given session.
// Called when the bound account becomes unavailable (e.g., error status, disabled,
// or unschedulable), allowing subsequent requests to select a new available account.
func (c *gatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Del(ctx, key).Err()
}

var claimOpenAIResponsesSessionWindowScript = redis.NewScript(`
local previous = redis.call('GET', KEYS[1])
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return previous
`)

var compareAndRefreshOpenAIResponsesSessionWindowScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current == false or current ~= ARGV[1] then
  return 0
end
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`)

var compareAndDeleteOpenAIResponsesSessionWindowScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current == false or current ~= ARGV[1] then
  return 0
end
redis.call('DEL', KEYS[1])
return 1
`)

func (c *gatewayCache) ClaimOpenAIResponsesSessionWindow(ctx context.Context, groupID int64, sessionHash string, owner []byte, ttl time.Duration) ([]byte, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	if len(owner) == 0 || strings.TrimSpace(sessionHash) == "" || ttl <= 0 {
		return nil, errors.New("invalid OpenAI Responses session-window claim")
	}
	result, err := claimOpenAIResponsesSessionWindowScript.Run(
		ctx,
		c.rdb,
		[]string{buildOpenAIResponsesSessionWindowKey(groupID, sessionHash)},
		owner,
		ttl.Milliseconds(),
	).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch value := result.(type) {
	case nil:
		return nil, nil
	case string:
		return []byte(value), nil
	case []byte:
		return append([]byte(nil), value...), nil
	default:
		return nil, fmt.Errorf("unexpected OpenAI Responses session-window claim result %T", result)
	}
}

func (c *gatewayCache) CompareAndRefreshOpenAIResponsesSessionWindow(ctx context.Context, groupID int64, sessionHash string, expected []byte, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	if len(expected) == 0 || strings.TrimSpace(sessionHash) == "" || ttl <= 0 {
		return false, errors.New("invalid OpenAI Responses session-window refresh")
	}
	n, err := compareAndRefreshOpenAIResponsesSessionWindowScript.Run(
		ctx,
		c.rdb,
		[]string{buildOpenAIResponsesSessionWindowKey(groupID, sessionHash)},
		expected,
		ttl.Milliseconds(),
	).Int()
	return n == 1, err
}

func (c *gatewayCache) CompareAndDeleteOpenAIResponsesSessionWindow(ctx context.Context, groupID int64, sessionHash string, expected []byte) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	if len(expected) == 0 || strings.TrimSpace(sessionHash) == "" {
		return false, errors.New("invalid OpenAI Responses session-window delete")
	}
	n, err := compareAndDeleteOpenAIResponsesSessionWindowScript.Run(
		ctx,
		c.rdb,
		[]string{buildOpenAIResponsesSessionWindowKey(groupID, sessionHash)},
		expected,
	).Int()
	return n == 1, err
}

var _ service.OpenAIWSSessionPreemptionCache = (*gatewayCache)(nil)

const (
	grokVideoPendingBillingPrefix = "grok_video_pending:"
	grokVideoBilledPrefix         = "grok_video_billed:"
)

func (c *gatewayCache) SetGrokVideoPendingBilling(ctx context.Context, key string, payload []byte, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" || len(payload) == 0 {
		return errors.New("invalid grok video pending billing payload")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return c.rdb.Set(ctx, grokVideoPendingBillingPrefix+key, payload, ttl).Err()
}

func (c *gatewayCache) GetGrokVideoPendingBilling(ctx context.Context, key string) ([]byte, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("invalid grok video pending billing key")
	}
	val, err := c.rdb.Get(ctx, grokVideoPendingBillingPrefix+key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	return val, nil
}

func (c *gatewayCache) ClaimGrokVideoBilled(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false, errors.New("invalid grok video billed key")
	}
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}
	return c.rdb.SetNX(ctx, grokVideoBilledPrefix+key, "1", ttl).Result()
}

func (c *gatewayCache) ReleaseGrokVideoBilled(ctx context.Context, key string) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("invalid grok video billed key")
	}
	return c.rdb.Del(ctx, grokVideoBilledPrefix+key).Err()
}

// Compile-time assertion: gatewayCache must implement CyberSessionBlockStore.
var _ service.CyberSessionBlockStore = (*gatewayCache)(nil)
var _ service.LiveCallStore = (*gatewayCache)(nil)
var _ service.RealtimeCallStore = (*gatewayCache)(nil)

const reasoningContentPrefix = "reasoning_content:"

// reasoningContentDefaultTTL 是 reasoning 缓存的默认过期时间。Codex 会话可能
// 跨多天恢复，取 7 天；调用方传入非正 TTL 时兜底。
const reasoningContentDefaultTTL = 7 * 24 * time.Hour

// SetReasoningContent 按 reasoning item id 缓存 reasoning 全文。
// itemID 或 content 为空时直接返回 nil（无可缓存内容，属正常情况而非错误）。
func (c *gatewayCache) SetReasoningContent(ctx context.Context, itemID string, content string, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || content == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = reasoningContentDefaultTTL
	}
	return c.rdb.Set(ctx, reasoningContentPrefix+itemID, content, ttl).Err()
}

// GetReasoningContent 返回缓存的 reasoning 全文；未命中返回
// service.ErrReasoningContentNotFound。
func (c *gatewayCache) GetReasoningContent(ctx context.Context, itemID string) (string, error) {
	if c == nil || c.rdb == nil {
		return "", errors.New("gateway cache unavailable")
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", service.ErrReasoningContentNotFound
	}
	val, err := c.rdb.Get(ctx, reasoningContentPrefix+itemID).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", service.ErrReasoningContentNotFound
		}
		return "", err
	}
	return val, nil
}

const (
	cyberSessionBlockPrefix         = "cyber_session_block:"
	cyberSessionScopePrefix         = "cyber_session_scope:"
	cyberSessionRedisCommandMaxKeys = 128
)

// SetCyberSessionBlocked writes exact blocks in bounded transactions. The
// coarse scope is activated only after all exact blocks have been stored.
func (c *gatewayCache) SetCyberSessionBlocked(ctx context.Context, scopeKey string, keys []string, ttl time.Duration) error {
	if len(keys) == 0 {
		return nil
	}
	exactKeys := make([]string, 0, cyberSessionRedisCommandMaxKeys)
	flush := func() error {
		if len(exactKeys) == 0 {
			return nil
		}
		pipe := c.rdb.TxPipeline()
		for _, key := range exactKeys {
			pipe.Set(ctx, cyberSessionBlockPrefix+key, "1", ttl)
		}
		_, err := pipe.Exec(ctx)
		exactKeys = exactKeys[:0]
		return err
	}
	for _, key := range keys {
		if key != "" {
			exactKeys = append(exactKeys, key)
			if len(exactKeys) == cyberSessionRedisCommandMaxKeys {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if scopeKey != "" {
		return c.rdb.Set(ctx, cyberSessionScopePrefix+scopeKey, "1", ttl).Err()
	}
	return nil
}

func (c *gatewayCache) IsCyberSessionScopeActive(ctx context.Context, scopeKey string) (bool, error) {
	n, err := c.rdb.Exists(ctx, cyberSessionScopePrefix+scopeKey).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// FindCyberSessionBlocked checks bounded batches in caller order and stops at
// the first blocked key, preserving the original earliest-match behavior.
func (c *gatewayCache) FindCyberSessionBlocked(ctx context.Context, keys []string) (string, error) {
	if len(keys) == 0 {
		return "", nil
	}
	for start := 0; start < len(keys); start += cyberSessionRedisCommandMaxKeys {
		end := start + cyberSessionRedisCommandMaxKeys
		if end > len(keys) {
			end = len(keys)
		}
		redisKeys := make([]string, end-start)
		for i, key := range keys[start:end] {
			redisKeys[i] = cyberSessionBlockPrefix + key
		}
		values, err := c.rdb.MGet(ctx, redisKeys...).Result()
		if err != nil {
			return "", err
		}
		for i, value := range values {
			if value != nil {
				return keys[start+i], nil
			}
		}
	}
	return "", nil
}

var claimLiveControllerScript = redis.NewScript(`
	local key = KEYS[1]
	local target = ARGV[1]
	local owner = ARGV[2]
	local current = redis.call('HGET', key, 'controller')
	if current == false or current == 'closed' then
		return 0
	end
	if target == 'observer' and current ~= 'pending' then
		return 0
	end
	if target == 'proxy' and current ~= 'pending' and current ~= 'observer' and
		(current ~= 'proxy' or redis.call('HGET', key, 'controller_owner') ~= owner) then
		return 0
	end
	redis.call('HSET', key, 'controller', target, 'controller_owner', owner)
	return 1
`)

var markLiveCallClosedScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('EXISTS', key) == 0 then
		return 0
	end
	if redis.call('HGET', key, 'controller') == 'closed' then
		return 0
	end
	redis.call('HSET', key, 'controller', 'closed', 'controller_owner', '')
	redis.call('EXPIRE', key, ARGV[1])
	return 1
`)

var claimRealtimeControllerScript = redis.NewScript(`
	local key = KEYS[1]
	local target = ARGV[1]
	local owner = ARGV[2]
	local current = redis.call('HGET', key, 'controller')
	if current == false or current == 'closed' then return 0 end
	if target == 'observer' and current ~= 'pending' then return 0 end
	if target == 'proxy' and current ~= 'pending' and current ~= 'observer' and
		(current ~= 'proxy' or redis.call('HGET', key, 'controller_owner') ~= owner) then return 0 end
	redis.call('HSET', key, 'controller', target, 'controller_owner', owner)
	return 1
`)

var markRealtimeCallClosedScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('EXISTS', key) == 0 or redis.call('HGET', key, 'controller') == 'closed' then return 0 end
	redis.call('HSET', key, 'controller', 'closed', 'controller_owner', '')
	redis.call('EXPIRE', key, ARGV[1])
	return 1
`)

var claimRealtimeFinalizationScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('EXISTS', key) == 0 or redis.call('HGET', key, 'controller') == 'closed' then return 0 end
	local controller = redis.call('HGET', key, 'controller')
	local current_owner = redis.call('HGET', key, 'finalizer_owner')
	local time = redis.call('TIME')
	local now = time[1] * 1000 + math.floor(time[2] / 1000)
	local owner_until = tonumber(redis.call('HGET', key, 'finalizer_owner_until')) or 0
	if controller == 'finalizing' and current_owner ~= '' and current_owner ~= ARGV[1] and owner_until > now then return 0 end
	redis.call('HSET', key, 'controller', 'finalizing', 'controller_owner', '', 'finalizer_owner', ARGV[1], 'finalizer_owner_until', now + ARGV[2])
	redis.call('EXPIRE', key, ARGV[3])
	return 1
`)

var releaseRealtimeFinalizationScript = redis.NewScript(`
	if redis.call('HGET', KEYS[1], 'controller') ~= 'finalizing' or redis.call('HGET', KEYS[1], 'finalizer_owner') ~= ARGV[1] then return 0 end
	redis.call('HSET', KEYS[1], 'finalizer_owner', '', 'finalizer_owner_until', '0')
	return 1
`)

var commitRealtimeFinalizationScript = redis.NewScript(`
	if redis.call('HGET', KEYS[1], 'controller') ~= 'finalizing' or redis.call('HGET', KEYS[1], 'finalizer_owner') ~= ARGV[1] then return 0 end
	local time = redis.call('TIME')
	local now = time[1] * 1000 + math.floor(time[2] / 1000)
	local owner_until = tonumber(redis.call('HGET', KEYS[1], 'finalizer_owner_until')) or 0
	if owner_until <= now then return 0 end
	redis.call('HSET', KEYS[1], 'controller', 'closed', 'controller_owner', '', 'finalizer_owner', '', 'finalizer_owner_until', '0')
	redis.call('EXPIRE', KEYS[1], ARGV[2])
	return 1
`)

var claimRealtimeReplayScript = redis.NewScript(`
	local current = redis.call('GET', KEYS[1])
	if current == false then
		redis.call('PSETEX', KEYS[1], ARGV[2], 'pending:' .. ARGV[1])
		return 1
	end
	return 0
`)

var claimRealtimeUsageScript = redis.NewScript(`
	local controller = redis.call('HGET', KEYS[2], 'controller')
	if controller == 'finalizing' or controller == 'closed' then return 0 end
	local current = redis.call('GET', KEYS[1])
	if current == false then
		redis.call('PSETEX', KEYS[1], ARGV[2], 'pending:' .. ARGV[1])
		return 1
	end
	return 0
`)

var claimRealtimeConservativeUsageScript = redis.NewScript(`
	if redis.call('HGET', KEYS[2], 'controller') ~= 'finalizing' or redis.call('HGET', KEYS[2], 'finalizer_owner') ~= ARGV[1] then return 0 end
	local time = redis.call('TIME')
	local now = time[1] * 1000 + math.floor(time[2] / 1000)
	local owner_until = tonumber(redis.call('HGET', KEYS[2], 'finalizer_owner_until')) or 0
	if owner_until <= now then return 0 end
	local current = redis.call('GET', KEYS[1])
	if current == false then
		redis.call('PSETEX', KEYS[1], ARGV[3], 'pending:' .. ARGV[2])
		return 1
	end
	return 0
`)

var commitRealtimeReplayScript = redis.NewScript(`
	if redis.call('GET', KEYS[1]) ~= 'pending:' .. ARGV[1] then return 0 end
	redis.call('PSETEX', KEYS[1], ARGV[2], 'committed')
	return 1
`)

var releaseRealtimeReplayScript = redis.NewScript(`
	if redis.call('GET', KEYS[1]) ~= 'pending:' .. ARGV[1] then return 0 end
	redis.call('DEL', KEYS[1])
	return 1
`)

var releaseRealtimeControllerScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('HGET', key, 'controller') ~= 'proxy' or redis.call('HGET', key, 'controller_owner') ~= ARGV[1] then return 0 end
	redis.call('HSET', key, 'controller', 'pending', 'controller_owner', '')
	return 1
`)

var resetRealtimeControllerForRecoveryScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('EXISTS', key) == 0 or redis.call('HGET', key, 'controller') == 'closed' then return 0 end
	redis.call('HSET', key, 'controller', 'pending', 'controller_owner', '')
	return 1
`)

var releaseLiveControllerScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('HGET', key, 'controller') ~= 'proxy' or
		redis.call('HGET', key, 'controller_owner') ~= ARGV[1] then
		return 0
	end
	redis.call('HSET', key, 'controller', 'pending', 'controller_owner', '')
	return 1
`)

func liveCallKey(callHash string) string {
	return liveCallPrefix + callHash
}

func realtimeCallKey(callHash string) string {
	return realtimeCallPrefix + callHash
}

func realtimeUsageKey(callHash, responseID string) string {
	sum := sha256.Sum256([]byte(responseID))
	return realtimeUsagePrefix + callHash + ":" + hex.EncodeToString(sum[:])
}

func realtimeWebhookKey(webhookID string) string {
	hash := sha256.Sum256([]byte(webhookID))
	return realtimeWebhookPrefix + hex.EncodeToString(hash[:])
}

func (c *gatewayCache) SaveLiveCall(ctx context.Context, record *service.LiveCallRecord, ttl time.Duration) error {
	if record == nil || record.CallHash == "" || record.CallID == "" {
		return fmt.Errorf("invalid live call record")
	}
	var enterpriseAttribution string
	if record.EnterpriseAttribution != nil {
		raw, err := json.Marshal(record.EnterpriseAttribution)
		if err != nil {
			return err
		}
		enterpriseAttribution = string(raw)
	}
	values := map[string]any{
		"call_id":                record.CallID,
		"account_id":             record.AccountID,
		"api_key_id":             record.APIKeyID,
		"user_id":                record.UserID,
		"group_id":               record.GroupID,
		"subscription_id":        record.SubscriptionID,
		"lease_id":               record.LeaseID,
		"model":                  record.Model,
		"created_at":             record.CreatedAt.UnixMilli(),
		"expires_at":             record.ExpiresAt.UnixMilli(),
		"controller":             record.Controller,
		"controller_owner":       record.ControllerOwner,
		"user_agent":             record.UserAgent,
		"ip_address":             record.IPAddress,
		"inbound_endpoint":       record.InboundEndpoint,
		"attestation":            record.AttestationCiphertext,
		"enterprise_attribution": enterpriseAttribution,
	}
	key := liveCallKey(record.CallHash)
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, key, values)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *gatewayCache) GetLiveCall(ctx context.Context, callHash string) (*service.LiveCallRecord, error) {
	values, err := c.rdb.HGetAll(ctx, liveCallKey(callHash)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, service.ErrLiveCallNotFound
	}
	parseInt := func(field string) int64 {
		value, _ := strconv.ParseInt(values[field], 10, 64)
		return value
	}
	createdAt := time.UnixMilli(parseInt("created_at"))
	expiresAt := time.UnixMilli(parseInt("expires_at"))
	record := &service.LiveCallRecord{
		CallID:                values["call_id"],
		CallHash:              callHash,
		AccountID:             parseInt("account_id"),
		APIKeyID:              parseInt("api_key_id"),
		UserID:                parseInt("user_id"),
		GroupID:               parseInt("group_id"),
		SubscriptionID:        parseInt("subscription_id"),
		LeaseID:               values["lease_id"],
		Model:                 values["model"],
		CreatedAt:             createdAt,
		ExpiresAt:             expiresAt,
		Controller:            values["controller"],
		ControllerOwner:       values["controller_owner"],
		UserAgent:             values["user_agent"],
		IPAddress:             values["ip_address"],
		InboundEndpoint:       values["inbound_endpoint"],
		AttestationCiphertext: values["attestation"],
	}
	if raw := values["enterprise_attribution"]; raw != "" {
		var snapshot service.EnterpriseUsageAttributionSnapshot
		if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
			return nil, err
		}
		record.EnterpriseAttribution = &snapshot
	}
	return record, nil
}

func (c *gatewayCache) ClaimLiveController(ctx context.Context, callHash, controller, owner string) (bool, error) {
	result, err := claimLiveControllerScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, controller, owner).Int()
	return result == 1, err
}

func (c *gatewayCache) GetLiveController(ctx context.Context, callHash string) (string, error) {
	value, err := c.rdb.HGet(ctx, liveCallKey(callHash), "controller").Result()
	if err == redis.Nil {
		return "", service.ErrLiveCallNotFound
	}
	return value, err
}

func (c *gatewayCache) ReleaseLiveController(ctx context.Context, callHash, owner string) (bool, error) {
	result, err := releaseLiveControllerScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, owner).Int()
	return result == 1, err
}

func (c *gatewayCache) MarkLiveCallClosed(ctx context.Context, callHash string, ttl time.Duration) (bool, error) {
	result, err := markLiveCallClosedScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, int64(ttl.Seconds())).Int()
	return result == 1, err
}

func (c *gatewayCache) SaveRealtimeCall(ctx context.Context, record *service.RealtimeCallRecord, ttl time.Duration) error {
	if record == nil || record.CallHash == "" || record.CallID == "" || record.PricingAt.IsZero() {
		return errors.New("invalid realtime call record")
	}
	var attribution string
	if record.EnterpriseAttribution != nil {
		raw, err := json.Marshal(record.EnterpriseAttribution)
		if err != nil {
			return err
		}
		attribution = string(raw)
	}
	values := map[string]any{
		"call_id": record.CallID, "account_id": record.AccountID, "api_key_id": record.APIKeyID,
		"user_id": record.UserID, "group_id": record.GroupID, "subscription_id": record.SubscriptionID,
		"lease_id": record.LeaseID, "model": record.Model, "pricing_at": record.PricingAt.UnixMilli(),
		"created_at": record.CreatedAt.UnixMilli(), "expires_at": record.ExpiresAt.UnixMilli(),
		"controller": record.Controller, "controller_owner": record.ControllerOwner,
		"user_agent": record.UserAgent, "ip_address": record.IPAddress, "inbound_endpoint": record.InboundEndpoint,
		"enterprise_attribution": attribution, "reservation_usd": strconv.FormatFloat(record.ReservationUSD, 'f', -1, 64),
		"reservation_request_id": record.ReservationRequestID, "reservation_has_balance": strconv.FormatBool(record.ReservationHasBalance),
		"reservation_scopes": strings.Join(record.ReservationScopes, ","),
	}
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, realtimeCallKey(record.CallHash), values)
	pipe.Expire(ctx, realtimeCallKey(record.CallHash), ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *gatewayCache) GetRealtimeCall(ctx context.Context, callHash string) (*service.RealtimeCallRecord, error) {
	values, err := c.rdb.HGetAll(ctx, realtimeCallKey(callHash)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, service.ErrRealtimeCallNotFound
	}
	parseInt := func(field string) int64 { value, _ := strconv.ParseInt(values[field], 10, 64); return value }
	pricingAtMillis, err := strconv.ParseInt(values["pricing_at"], 10, 64)
	if err != nil || pricingAtMillis <= 0 {
		return nil, errors.New("realtime call record has no pricing timestamp")
	}
	record := &service.RealtimeCallRecord{
		CallID: values["call_id"], CallHash: callHash, AccountID: parseInt("account_id"), APIKeyID: parseInt("api_key_id"),
		UserID: parseInt("user_id"), GroupID: parseInt("group_id"), SubscriptionID: parseInt("subscription_id"),
		LeaseID: values["lease_id"], Model: values["model"], PricingAt: time.UnixMilli(pricingAtMillis),
		CreatedAt: time.UnixMilli(parseInt("created_at")), ExpiresAt: time.UnixMilli(parseInt("expires_at")),
		Controller: values["controller"], ControllerOwner: values["controller_owner"], UserAgent: values["user_agent"],
		IPAddress: values["ip_address"], InboundEndpoint: values["inbound_endpoint"],
	}
	if reservationUSD, err := strconv.ParseFloat(values["reservation_usd"], 64); err == nil {
		record.ReservationUSD = reservationUSD
	}
	record.ReservationRequestID = values["reservation_request_id"]
	record.ReservationHasBalance, _ = strconv.ParseBool(values["reservation_has_balance"])
	if scopes := strings.TrimSpace(values["reservation_scopes"]); scopes != "" {
		record.ReservationScopes = strings.Split(scopes, ",")
	}
	if raw := values["enterprise_attribution"]; raw != "" {
		var snapshot service.EnterpriseUsageAttributionSnapshot
		if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
			return nil, err
		}
		record.EnterpriseAttribution = &snapshot
	}
	return record, nil
}

func (c *gatewayCache) ListActiveRealtimeCalls(ctx context.Context) ([]*service.RealtimeCallRecord, error) {
	var cursor uint64
	var records []*service.RealtimeCallRecord
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, realtimeCallPrefix+"*", 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			callHash := strings.TrimPrefix(key, realtimeCallPrefix)
			record, err := c.GetRealtimeCall(ctx, callHash)
			if err != nil || record.Controller == service.RealtimeControllerClosed {
				continue
			}
			records = append(records, record)
		}
		cursor = next
		if cursor == 0 {
			return records, nil
		}
	}
}

func (c *gatewayCache) ClaimRealtimeController(ctx context.Context, callHash, controller, owner string) (bool, error) {
	result, err := claimRealtimeControllerScript.Run(ctx, c.rdb, []string{realtimeCallKey(callHash)}, controller, owner).Int()
	return result == 1, err
}

func (c *gatewayCache) GetRealtimeController(ctx context.Context, callHash string) (string, error) {
	value, err := c.rdb.HGet(ctx, realtimeCallKey(callHash), "controller").Result()
	if errors.Is(err, redis.Nil) {
		return "", service.ErrRealtimeCallNotFound
	}
	return value, err
}

func (c *gatewayCache) ReleaseRealtimeController(ctx context.Context, callHash, owner string) (bool, error) {
	result, err := releaseRealtimeControllerScript.Run(ctx, c.rdb, []string{realtimeCallKey(callHash)}, owner).Int()
	return result == 1, err
}

func (c *gatewayCache) ResetRealtimeControllerForRecovery(ctx context.Context, callHash string) (bool, error) {
	result, err := resetRealtimeControllerForRecoveryScript.Run(ctx, c.rdb, []string{realtimeCallKey(callHash)}).Int()
	return result == 1, err
}

func (c *gatewayCache) MarkRealtimeCallClosed(ctx context.Context, callHash string, ttl time.Duration) (bool, error) {
	result, err := markRealtimeCallClosedScript.Run(ctx, c.rdb, []string{realtimeCallKey(callHash)}, int64(ttl.Seconds())).Int()
	return result == 1, err
}

func (c *gatewayCache) ClaimRealtimeFinalization(ctx context.Context, callHash, owner string, lease, recordTTL time.Duration) (bool, error) {
	if strings.TrimSpace(owner) == "" || lease <= 0 || recordTTL <= 0 {
		return false, errors.New("invalid realtime finalization claim")
	}
	result, err := claimRealtimeFinalizationScript.Run(ctx, c.rdb, []string{realtimeCallKey(callHash)}, owner, lease.Milliseconds(), int64(recordTTL.Seconds())).Int()
	return result == 1, err
}

func (c *gatewayCache) ReleaseRealtimeFinalization(ctx context.Context, callHash, owner string) (bool, error) {
	result, err := releaseRealtimeFinalizationScript.Run(ctx, c.rdb, []string{realtimeCallKey(callHash)}, owner).Int()
	return result == 1, err
}

func (c *gatewayCache) CommitRealtimeFinalization(ctx context.Context, callHash, owner string, ttl time.Duration) (bool, error) {
	result, err := commitRealtimeFinalizationScript.Run(ctx, c.rdb, []string{realtimeCallKey(callHash)}, owner, int64(ttl.Seconds())).Int()
	return result == 1, err
}

func (c *gatewayCache) BeginRealtimeUsage(ctx context.Context, callHash, responseID string, ttl time.Duration) (bool, error) {
	if strings.TrimSpace(responseID) == "" || ttl <= 0 {
		return false, errors.New("invalid realtime response id")
	}
	return c.rdb.SetNX(ctx, realtimeUsageKey(callHash, responseID), "pending", ttl).Result()
}

func (c *gatewayCache) CommitRealtimeUsage(ctx context.Context, callHash, responseID string, ttl time.Duration) error {
	return c.rdb.Set(ctx, realtimeUsageKey(callHash, responseID), "committed", ttl).Err()
}

func (c *gatewayCache) ReleaseRealtimeUsage(ctx context.Context, callHash, responseID string) error {
	return c.rdb.Del(ctx, realtimeUsageKey(callHash, responseID)).Err()
}

func (c *gatewayCache) ClaimRealtimeUsage(ctx context.Context, callHash, responseID, owner string, lease time.Duration) (bool, error) {
	if strings.TrimSpace(responseID) == "" || strings.TrimSpace(owner) == "" || lease <= 0 {
		return false, errors.New("invalid realtime usage claim")
	}
	claimed, err := claimRealtimeUsageScript.Run(ctx, c.rdb, []string{realtimeUsageKey(callHash, responseID), realtimeCallKey(callHash)}, owner, lease.Milliseconds()).Int()
	return claimed == 1, err
}

func (c *gatewayCache) ClaimRealtimeConservativeUsage(ctx context.Context, callHash, responseID, finalizerOwner, owner string, lease time.Duration) (bool, error) {
	if strings.TrimSpace(responseID) == "" || strings.TrimSpace(finalizerOwner) == "" || strings.TrimSpace(owner) == "" || lease <= 0 {
		return false, errors.New("invalid realtime conservative usage claim")
	}
	claimed, err := claimRealtimeConservativeUsageScript.Run(ctx, c.rdb, []string{realtimeUsageKey(callHash, responseID), realtimeCallKey(callHash)}, finalizerOwner, owner, lease.Milliseconds()).Int()
	return claimed == 1, err
}

func (c *gatewayCache) CommitRealtimeUsageClaim(ctx context.Context, callHash, responseID, owner string, ttl time.Duration) (bool, error) {
	committed, err := commitRealtimeReplayScript.Run(ctx, c.rdb, []string{realtimeUsageKey(callHash, responseID)}, owner, ttl.Milliseconds()).Int()
	return committed == 1, err
}

func (c *gatewayCache) ReleaseRealtimeUsageClaim(ctx context.Context, callHash, responseID, owner string) (bool, error) {
	released, err := releaseRealtimeReplayScript.Run(ctx, c.rdb, []string{realtimeUsageKey(callHash, responseID)}, owner).Int()
	return released == 1, err
}

func (c *gatewayCache) HasCommittedRealtimeUsage(ctx context.Context, callHash string) (bool, error) {
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, realtimeUsagePrefix+callHash+":*", 100).Result()
		if err != nil {
			return false, err
		}
		if len(keys) > 0 {
			values, err := c.rdb.MGet(ctx, keys...).Result()
			if err != nil {
				return false, err
			}
			for _, value := range values {
				if value == "committed" {
					return true, nil
				}
			}
		}
		cursor = next
		if cursor == 0 {
			return false, nil
		}
	}
}

func (c *gatewayCache) HasPendingRealtimeUsage(ctx context.Context, callHash string) (bool, error) {
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, realtimeUsagePrefix+callHash+":*", 100).Result()
		if err != nil {
			return false, err
		}
		if len(keys) > 0 {
			values, err := c.rdb.MGet(ctx, keys...).Result()
			if err != nil {
				return false, err
			}
			for _, value := range values {
				if text, ok := value.(string); ok && strings.HasPrefix(text, "pending:") {
					return true, nil
				}
			}
		}
		cursor = next
		if cursor == 0 {
			return false, nil
		}
	}
}

func (c *gatewayCache) BeginRealtimeWebhook(ctx context.Context, webhookID string, ttl time.Duration) (bool, error) {
	if strings.TrimSpace(webhookID) == "" || ttl <= 0 {
		return false, errors.New("invalid realtime webhook id")
	}
	return c.rdb.SetNX(ctx, realtimeWebhookKey(webhookID), "pending", ttl).Result()
}

func (c *gatewayCache) CommitRealtimeWebhook(ctx context.Context, webhookID string, ttl time.Duration) error {
	return c.rdb.Set(ctx, realtimeWebhookKey(webhookID), "committed", ttl).Err()
}

func (c *gatewayCache) ReleaseRealtimeWebhook(ctx context.Context, webhookID string) error {
	return c.rdb.Del(ctx, realtimeWebhookKey(webhookID)).Err()
}

func (c *gatewayCache) ClaimRealtimeWebhook(ctx context.Context, webhookID, owner string, lease time.Duration) (bool, error) {
	if strings.TrimSpace(webhookID) == "" || strings.TrimSpace(owner) == "" || lease <= 0 {
		return false, errors.New("invalid realtime webhook claim")
	}
	claimed, err := claimRealtimeReplayScript.Run(ctx, c.rdb, []string{realtimeWebhookKey(webhookID)}, owner, lease.Milliseconds()).Int()
	return claimed == 1, err
}

func (c *gatewayCache) CommitRealtimeWebhookClaim(ctx context.Context, webhookID, owner string, ttl time.Duration) (bool, error) {
	committed, err := commitRealtimeReplayScript.Run(ctx, c.rdb, []string{realtimeWebhookKey(webhookID)}, owner, ttl.Milliseconds()).Int()
	return committed == 1, err
}

func (c *gatewayCache) ReleaseRealtimeWebhookClaim(ctx context.Context, webhookID, owner string) (bool, error) {
	released, err := releaseRealtimeReplayScript.Run(ctx, c.rdb, []string{realtimeWebhookKey(webhookID)}, owner).Int()
	return released == 1, err
}
