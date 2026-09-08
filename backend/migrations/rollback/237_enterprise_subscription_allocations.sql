-- Execute manually only after stopping writers.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM enterprise_weekly_allocations WHERE window_type <> '7d')
       OR EXISTS (SELECT 1 FROM enterprise_usage_attributions WHERE window_type <> '7d')
       OR EXISTS (SELECT 1 FROM enterprise_subscription_windows WHERE allocation_limit_5h <> 0) THEN
        RAISE EXCEPTION 'cannot rollback SHAN-154 while 5h allocation state exists';
    END IF;
END
$$;

DROP INDEX IF EXISTS idx_enterprise_usage_attributions_window;
ALTER TABLE enterprise_usage_attributions
    DROP CONSTRAINT uq_enterprise_attributions_usage_window,
    DROP CONSTRAINT ck_enterprise_attributions_generation,
    DROP CONSTRAINT ck_enterprise_attributions_window_type,
    DROP COLUMN request_at,
    DROP COLUMN assignment_generation,
    DROP COLUMN api_key_id,
    DROP COLUMN window_type;
ALTER TABLE enterprise_usage_attributions
    RENAME COLUMN window_anchor TO weekly_window_anchor;
ALTER TABLE enterprise_usage_attributions
    ADD CONSTRAINT enterprise_usage_attributions_usage_log_id_key UNIQUE (usage_log_id),
    ADD CONSTRAINT enterprise_usage_attributions_usage_log_id_fkey
        FOREIGN KEY (usage_log_id) REFERENCES usage_logs(id) ON DELETE RESTRICT;
CREATE INDEX idx_enterprise_usage_attributions_window
    ON enterprise_usage_attributions (
        enterprise_id, subscription_id, weekly_window_anchor, employee_id
    );
COMMENT ON COLUMN enterprise_usage_attributions.usage_log_id IS
    'Unique foreign key to the authoritative usage_logs row';

ALTER TABLE enterprise_weekly_allocations
    DROP CONSTRAINT uq_enterprise_allocations_scope,
    DROP CONSTRAINT ck_enterprise_allocations_window_type,
    DROP COLUMN window_type;
ALTER TABLE enterprise_weekly_allocations
    RENAME COLUMN window_anchor TO weekly_window_anchor;
DROP INDEX IF EXISTS idx_enterprise_allocations_window;
CREATE INDEX idx_enterprise_weekly_allocations_window
    ON enterprise_weekly_allocations (
        enterprise_id, subscription_id, weekly_window_anchor, employee_id
    );
ALTER TABLE enterprise_weekly_allocations
    ADD CONSTRAINT enterprise_weekly_allocations_enterprise_id_subscription_id_key
        UNIQUE (enterprise_id, subscription_id, weekly_window_anchor, employee_id);

ALTER TABLE enterprise_subscription_windows
    DROP CONSTRAINT ck_enterprise_window_limit_actor_nonempty,
    DROP CONSTRAINT ck_enterprise_window_limit_reason_nonempty,
    DROP CONSTRAINT ck_enterprise_window_limit_5h_nonnegative,
    DROP COLUMN allocation_limit_actor_ref,
    DROP COLUMN allocation_limit_reason,
    DROP COLUMN allocation_limit_5h;
ALTER TABLE enterprise_subscription_windows
    RENAME COLUMN allocation_limit_7d TO allocation_snapshot;
