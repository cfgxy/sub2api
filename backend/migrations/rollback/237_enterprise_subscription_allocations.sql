-- Execute manually only after stopping writers.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM enterprise_usage_attributions AS attribution
        LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
        WHERE usage_log.id IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot rollback SHAN-154 while enterprise usage attribution references a missing usage log; settle or clean up dangling attributions first';
    END IF;

    IF EXISTS (SELECT 1 FROM enterprise_weekly_allocations WHERE window_type <> 'week')
       OR EXISTS (SELECT 1 FROM enterprise_usage_attributions WHERE window_type <> 'week') THEN
        RAISE EXCEPTION 'cannot rollback SHAN-154 while day/month allocation state exists';
    END IF;
END
$$;

DROP TRIGGER IF EXISTS sync_enterprise_api_key_candidates ON enterprises;
DROP FUNCTION IF EXISTS sync_enterprise_api_key_candidates();
DROP TRIGGER IF EXISTS enqueue_api_key_enterprise_candidate_invalidation ON api_keys;
DROP FUNCTION IF EXISTS enqueue_api_key_enterprise_candidate_invalidation();
DROP TRIGGER IF EXISTS set_api_key_enterprise_attribution_candidate ON api_keys;
DROP FUNCTION IF EXISTS set_api_key_enterprise_attribution_candidate();
ALTER TABLE api_keys DROP COLUMN enterprise_attribution_candidate;

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
