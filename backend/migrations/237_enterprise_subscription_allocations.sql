ALTER TABLE enterprise_subscription_windows
    RENAME COLUMN allocation_snapshot TO allocation_limit_7d;
ALTER TABLE enterprise_subscription_windows
    ADD COLUMN allocation_limit_5h NUMERIC(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN allocation_limit_reason VARCHAR(200) NOT NULL DEFAULT 'legacy-7d-snapshot',
    ADD COLUMN allocation_limit_actor_ref TEXT NOT NULL DEFAULT 'migration:237',
    ADD CONSTRAINT ck_enterprise_window_limit_5h_nonnegative CHECK (allocation_limit_5h >= 0),
    ADD CONSTRAINT ck_enterprise_window_limit_reason_nonempty CHECK (BTRIM(allocation_limit_reason) <> ''),
    ADD CONSTRAINT ck_enterprise_window_limit_actor_nonempty CHECK (BTRIM(allocation_limit_actor_ref) <> '');

ALTER TABLE enterprise_weekly_allocations
    RENAME COLUMN weekly_window_anchor TO window_anchor;
ALTER TABLE enterprise_weekly_allocations
    ADD COLUMN window_type VARCHAR(10) NOT NULL DEFAULT '7d',
    ADD CONSTRAINT ck_enterprise_allocations_window_type CHECK (window_type IN ('5h', '7d'));
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
    ADD COLUMN window_type VARCHAR(10) NOT NULL DEFAULT '7d',
    ADD COLUMN api_key_id BIGINT,
    ADD COLUMN assignment_generation BIGINT,
    ADD COLUMN request_at TIMESTAMPTZ,
    ADD CONSTRAINT ck_enterprise_attributions_window_type CHECK (window_type IN ('5h', '7d'));

UPDATE enterprise_usage_attributions AS attribution
SET api_key_id = usage_log.api_key_id,
    request_at = usage_log.created_at,
    assignment_generation = COALESCE((
        SELECT assignment.generation
        FROM enterprise_key_assignments AS assignment
        WHERE assignment.enterprise_id = attribution.enterprise_id
          AND assignment.employee_id IS NOT DISTINCT FROM attribution.employee_id
          AND assignment.api_key_id = usage_log.api_key_id
          AND assignment.assigned_at <= usage_log.created_at
          AND (assignment.ended_at IS NULL OR usage_log.created_at < assignment.ended_at)
        ORDER BY assignment.generation DESC
        LIMIT 1
    ), 0)
FROM usage_logs AS usage_log
WHERE usage_log.id = attribution.usage_log_id;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM enterprise_usage_attributions
        WHERE api_key_id IS NULL OR assignment_generation IS NULL OR request_at IS NULL
    ) THEN
        RAISE EXCEPTION 'SHAN-154 cannot backfill request attribution snapshots';
    END IF;
END
$$;

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
    'Reserved usage_logs id captured at request attribution time; settlement may insert the usage row later';
COMMENT ON COLUMN enterprise_usage_attributions.assignment_generation IS
    'Immutable enterprise key assignment generation captured at request time; 0 means controlled external';
