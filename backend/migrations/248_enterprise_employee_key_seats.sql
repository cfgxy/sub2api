-- 企业员工 API Key 席位数；新企业由服务层显式传入，默认值只保障旧客户端和历史数据可读。
ALTER TABLE enterprises
    ADD COLUMN IF NOT EXISTS seat_count INTEGER NOT NULL DEFAULT 1;

ALTER TABLE enterprises
    DROP CONSTRAINT IF EXISTS chk_enterprises_seat_count;
ALTER TABLE enterprises
    ADD CONSTRAINT chk_enterprises_seat_count CHECK (seat_count BETWEEN 1 AND 10000);

-- 已存在的有效员工 Key 不会在迁移时失效：将历史企业容量提升到至少当前占用数。
-- 过期、已删除、已禁用的 Key 本来就不占用席位。
UPDATE enterprises AS enterprise
SET seat_count = GREATEST(
    1,
    COALESCE((
        SELECT COUNT(*)
        FROM enterprise_key_assignments AS assignment
        JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id
        WHERE assignment.enterprise_id = enterprise.id
          AND assignment.status = 'active'
          AND api_key.status = 'active'
          AND api_key.deleted_at IS NULL
          AND (api_key.expires_at IS NULL OR api_key.expires_at > statement_timestamp())
          AND (api_key.quota <= 0 OR api_key.quota_used < api_key.quota)
    ), 0)
);

-- dedicated_upstream_user_id 是企业网关官方账号。席位和该账号并发必须同源，
-- 因此在同一迁移中校正历史账号的 concurrency，后续由服务事务原子维护。
UPDATE users AS user_record
SET concurrency = enterprise.seat_count,
    updated_at = NOW()
FROM enterprises AS enterprise
WHERE enterprise.dedicated_upstream_user_id = user_record.id
  AND user_record.concurrency IS DISTINCT FROM enterprise.seat_count;
