-- Inverse of 236_enterprise_frozen_contract.sql.
-- Restores the 235-original shapes that 236 replaced. The guard triggers and
-- functions that 236 dropped belong to migration 235 and are not recreated
-- here; when the chain is rolled back in full they are removed together with
-- the tables by rollback/235.

-- usage attributions: drop the 236 foreign key and column comment.
ALTER TABLE enterprise_usage_attributions
    DROP CONSTRAINT IF EXISTS enterprise_usage_attributions_usage_log_id_fkey;
COMMENT ON COLUMN enterprise_usage_attributions.usage_log_id IS NULL;

-- key assignments: drop 236 constraints/indexes/columns, restore 235 originals.
ALTER TABLE enterprise_key_assignments
    DROP CONSTRAINT IF EXISTS ck_enterprise_key_assignments_status_times,
    DROP CONSTRAINT IF EXISTS ck_enterprise_key_assignments_time_order;
DROP INDEX IF EXISTS uq_enterprise_key_assignments_active_api_key;
DROP INDEX IF EXISTS idx_enterprise_key_assignments_segments;
ALTER TABLE enterprise_key_assignments
    DROP COLUMN IF EXISTS upstream_user_subscription_id,
    DROP COLUMN IF EXISTS upstream_group_id,
    DROP COLUMN IF EXISTS generation,
    DROP COLUMN IF EXISTS ended_at;
ALTER TABLE enterprise_key_assignments
    DROP CONSTRAINT IF EXISTS enterprise_key_assignments_status_check;
ALTER TABLE enterprise_key_assignments
    ADD CONSTRAINT enterprise_key_assignments_status_check CHECK (status IN ('active', 'revoked')),
    ADD CONSTRAINT ck_enterprise_key_assignments_status_revoked_at CHECK (
        (status = 'active' AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL)
    );
CREATE UNIQUE INDEX IF NOT EXISTS uq_enterprise_key_assignments_api_key_history
    ON enterprise_key_assignments (api_key_id);

-- subscription windows: restore the source_anchor shape and precision.
DROP INDEX IF EXISTS uq_enterprise_subscription_windows_upstream_window;
DROP INDEX IF EXISTS idx_enterprise_subscription_windows_anchor;
ALTER TABLE enterprise_subscription_windows
    DROP CONSTRAINT IF EXISTS ck_enterprise_subscription_windows_observed_start;
ALTER TABLE enterprise_subscription_windows
    ALTER COLUMN allocation_snapshot TYPE NUMERIC(20,10),
    DROP COLUMN IF EXISTS upstream_user_subscription_id;
ALTER TABLE enterprise_subscription_windows
    RENAME COLUMN observed_weekly_window_start TO source_anchor;
CREATE INDEX IF NOT EXISTS idx_enterprise_subscription_windows_anchor
    ON enterprise_subscription_windows (enterprise_id, subscription_id, source_anchor DESC);

-- subscriptions: restore effective_window_anchor and the 235 status check.
DROP INDEX IF EXISTS uq_enterprise_subscriptions_one_scheduled;
DROP INDEX IF EXISTS idx_enterprise_subscriptions_observed_window;
ALTER TABLE enterprise_subscriptions
    DROP CONSTRAINT IF EXISTS ck_enterprise_subscriptions_status_timestamps;
ALTER TABLE enterprise_subscriptions
    ALTER COLUMN observed_weekly_window_start SET NOT NULL;
ALTER TABLE enterprise_subscriptions
    RENAME COLUMN observed_weekly_window_start TO effective_window_anchor;
ALTER TABLE enterprise_subscriptions
    ADD CONSTRAINT ck_enterprise_subscriptions_status_timestamps CHECK (
        (status = 'scheduled' AND activated_at IS NULL AND ended_at IS NULL)
        OR (status = 'active' AND activated_at IS NOT NULL AND ended_at IS NOT NULL)
        OR (status = 'ended' AND activated_at IS NOT NULL AND ended_at IS NOT NULL)
        OR (status = 'cancelled' AND activated_at IS NULL AND ended_at IS NOT NULL)
    );
CREATE INDEX IF NOT EXISTS idx_enterprise_subscriptions_effective_anchor
    ON enterprise_subscriptions (enterprise_id, effective_window_anchor DESC);

-- allocation amounts back to the 235 precision.
ALTER TABLE enterprise_weekly_allocations
    ALTER COLUMN amount TYPE NUMERIC(20,10);
ALTER TABLE enterprise_allocation_revisions
    ALTER COLUMN previous_amount TYPE NUMERIC(20,10),
    ALTER COLUMN new_amount TYPE NUMERIC(20,10);

-- employees: drop the 236 department linkage and the departments table.
DROP INDEX IF EXISTS idx_enterprise_employees_department;
ALTER TABLE enterprise_employees
    DROP CONSTRAINT IF EXISTS enterprise_employees_department_fkey;
ALTER TABLE enterprise_employees
    DROP COLUMN IF EXISTS department_id;
DROP TABLE IF EXISTS enterprise_departments;
