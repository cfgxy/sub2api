-- Realtime 实际用量与 Redis 会话账本跨存储时，以该表作为数据库事务内的权威结果。
-- call_hash 是上游 call_id 的 SHA-256，不保存原始会话标识、音频或事件正文。

CREATE TABLE IF NOT EXISTS realtime_billing_outcomes (
    call_hash VARCHAR(64) PRIMARY KEY,
    request_id VARCHAR(255) NOT NULL,
    api_key_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_realtime_billing_outcomes_created_at
    ON realtime_billing_outcomes (created_at);
