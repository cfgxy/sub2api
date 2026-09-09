ALTER TABLE api_keys
    ADD COLUMN enterprise_attribution_candidate BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE api_keys AS api_key
SET enterprise_attribution_candidate = TRUE
WHERE EXISTS (
    SELECT 1
    FROM enterprises AS enterprise
    WHERE enterprise.dedicated_upstream_user_id = api_key.user_id
);

CREATE FUNCTION set_api_key_enterprise_attribution_candidate()
RETURNS TRIGGER AS $$
BEGIN
    NEW.enterprise_attribution_candidate := EXISTS (
        SELECT 1
        FROM enterprises AS enterprise
        WHERE enterprise.dedicated_upstream_user_id = NEW.user_id
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_api_key_enterprise_attribution_candidate
    BEFORE INSERT OR UPDATE OF user_id ON api_keys
    FOR EACH ROW EXECUTE FUNCTION set_api_key_enterprise_attribution_candidate();

CREATE FUNCTION enqueue_api_key_enterprise_candidate_invalidation()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.enterprise_attribution_candidate IS DISTINCT FROM NEW.enterprise_attribution_candidate THEN
        PERFORM enqueue_auth_cache_invalidation(NEW.key);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER enqueue_api_key_enterprise_candidate_invalidation
    AFTER UPDATE OF enterprise_attribution_candidate ON api_keys
    FOR EACH ROW EXECUTE FUNCTION enqueue_api_key_enterprise_candidate_invalidation();

CREATE FUNCTION sync_enterprise_api_key_candidates()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' OR TG_OP = 'UPDATE' THEN
        UPDATE api_keys AS api_key
        SET enterprise_attribution_candidate = EXISTS (
            SELECT 1
            FROM enterprises AS enterprise
            WHERE enterprise.dedicated_upstream_user_id = OLD.dedicated_upstream_user_id
        )
        WHERE api_key.user_id = OLD.dedicated_upstream_user_id;
    END IF;

    IF TG_OP = 'INSERT' OR TG_OP = 'UPDATE' THEN
        UPDATE api_keys AS api_key
        SET enterprise_attribution_candidate = TRUE
        WHERE api_key.user_id = NEW.dedicated_upstream_user_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER sync_enterprise_api_key_candidates
    AFTER INSERT OR UPDATE OR DELETE ON enterprises
    FOR EACH ROW EXECUTE FUNCTION sync_enterprise_api_key_candidates();

ALTER TABLE enterprise_weekly_allocations
    RENAME COLUMN weekly_window_anchor TO window_anchor;
ALTER TABLE enterprise_weekly_allocations
    ADD COLUMN window_type VARCHAR(10) NOT NULL DEFAULT 'week',
    ADD CONSTRAINT ck_enterprise_allocations_window_type CHECK (window_type IN ('day', 'week', 'month'));
ALTER TABLE enterprise_weekly_allocations
    ALTER COLUMN window_type DROP DEFAULT;
DROP INDEX IF EXISTS idx_enterprise_weekly_allocations_window;
CREATE INDEX idx_enterprise_allocations_window
    ON enterprise_weekly_allocations (
        enterprise_id, subscription_id, window_type, window_anchor, employee_id
    );
ALTER TABLE enterprise_weekly_allocations
    DROP CONSTRAINT enterprise_weekly_allocations_enterprise_id_subscription_id_key;
ALTER TABLE enterprise_weekly_allocations
    ADD CONSTRAINT uq_enterprise_allocations_scope
        UNIQUE (enterprise_id, subscription_id, window_type, window_anchor, employee_id);

ALTER TABLE enterprise_usage_attributions
    DROP CONSTRAINT enterprise_usage_attributions_usage_log_id_fkey,
    DROP CONSTRAINT enterprise_usage_attributions_usage_log_id_key;
ALTER TABLE enterprise_usage_attributions
    RENAME COLUMN weekly_window_anchor TO window_anchor;
ALTER TABLE enterprise_usage_attributions
    ADD COLUMN window_type VARCHAR(10) NOT NULL DEFAULT 'week',
    ADD COLUMN api_key_id BIGINT,
    ADD COLUMN assignment_generation BIGINT,
    ADD COLUMN request_at TIMESTAMPTZ,
    ADD CONSTRAINT ck_enterprise_attributions_window_type CHECK (window_type IN ('day', 'week', 'month'));

UPDATE enterprise_usage_attributions AS attribution
SET api_key_id = usage_log.api_key_id,
    request_at = usage_log.created_at,
    assignment_generation = COALESCE((
        SELECT assignment.generation
        FROM enterprise_key_assignments AS assignment
        JOIN enterprise_subscriptions AS enterprise_subscription
          ON enterprise_subscription.id = attribution.subscription_id
         AND enterprise_subscription.enterprise_id = attribution.enterprise_id
        WHERE assignment.enterprise_id = attribution.enterprise_id
          AND assignment.employee_id = attribution.employee_id
          AND assignment.api_key_id = usage_log.api_key_id
          AND assignment.upstream_user_subscription_id = enterprise_subscription.upstream_user_subscription_id
          AND assignment.assigned_at <= usage_log.created_at
          AND (assignment.ended_at IS NULL OR usage_log.created_at < assignment.ended_at)
        ORDER BY assignment.generation DESC
        LIMIT 1
    ), 0)
FROM usage_logs AS usage_log
WHERE usage_log.id = attribution.usage_log_id;

ALTER TABLE enterprise_usage_attributions
    ALTER COLUMN window_type DROP DEFAULT,
    ALTER COLUMN api_key_id SET NOT NULL,
    ALTER COLUMN assignment_generation SET NOT NULL,
    ALTER COLUMN request_at SET NOT NULL,
    ADD CONSTRAINT ck_enterprise_attributions_generation CHECK (assignment_generation >= 0),
    ADD CONSTRAINT uq_enterprise_attributions_usage_window UNIQUE (usage_log_id, window_type);
DROP INDEX IF EXISTS idx_enterprise_usage_attributions_window;
CREATE INDEX idx_enterprise_usage_attributions_window
    ON enterprise_usage_attributions (
        enterprise_id, subscription_id, window_type, window_anchor, employee_id
    );
COMMENT ON COLUMN enterprise_usage_attributions.usage_log_id IS
    '请求归属时保留的 usage_logs 标识；结算可在之后写入 usage_logs';
COMMENT ON COLUMN enterprise_usage_attributions.request_at IS
    '不可变请求时刻：迁移后新记录严格使用网关 PricingAt；仅 237 历史迁移兼容时以关联 usage_logs.created_at 回填';
COMMENT ON COLUMN enterprise_usage_attributions.assignment_generation IS
    '请求时冻结的企业 API Key assignment generation；0 表示 controlled_external，或历史 employee 记录无法重建具体代次';
