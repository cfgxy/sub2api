-- SHAN-151 frozen enterprise contract correction.
-- Migration 235 has already been applied in dev and must remain immutable. The
-- enterprise tables are not yet in use, so structural corrections fail closed
-- if any enterprise mapping or history row exists.

DO $$
DECLARE
    table_name TEXT;
    row_count BIGINT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'enterprise_audit_events',
        'enterprise_usage_attributions',
        'enterprise_allocation_revisions',
        'enterprise_weekly_allocations',
        'enterprise_key_assignments',
        'enterprise_subscription_windows',
        'enterprise_subscriptions',
        'enterprise_employees',
        'enterprises'
    ] LOOP
        EXECUTE format('SELECT COUNT(*) FROM %I', table_name) INTO row_count;
        IF row_count <> 0 THEN
            RAISE EXCEPTION 'SHAN-151 migration 236 requires empty enterprise tables; % has % rows',
                table_name, row_count;
        END IF;
    END LOOP;
END
$$;

-- Immutability and lifecycle validation belong to the application repository.
-- Keep database-expressible foreign keys, checks, and unique indexes only.
DROP TRIGGER IF EXISTS validate_enterprise_subscription_owner ON enterprise_subscriptions;
DROP TRIGGER IF EXISTS protect_enterprise_subscription_update ON enterprise_subscriptions;
DROP TRIGGER IF EXISTS validate_enterprise_key_owner ON enterprise_key_assignments;
DROP TRIGGER IF EXISTS validate_enterprise_usage_attribution ON enterprise_usage_attributions;
DROP TRIGGER IF EXISTS enterprise_subscription_windows_immutable ON enterprise_subscription_windows;
DROP TRIGGER IF EXISTS enterprise_allocation_revisions_immutable ON enterprise_allocation_revisions;
DROP TRIGGER IF EXISTS enterprise_usage_attributions_immutable ON enterprise_usage_attributions;
DROP TRIGGER IF EXISTS enterprise_audit_events_immutable ON enterprise_audit_events;
DROP TRIGGER IF EXISTS enterprise_subscriptions_no_delete ON enterprise_subscriptions;
DROP TRIGGER IF EXISTS enterprise_employees_no_delete ON enterprise_employees;
DROP TRIGGER IF EXISTS enterprise_weekly_allocations_no_delete ON enterprise_weekly_allocations;
DROP TRIGGER IF EXISTS enterprise_key_assignments_history ON enterprise_key_assignments;

DROP FUNCTION IF EXISTS validate_enterprise_subscription_owner();
DROP FUNCTION IF EXISTS protect_enterprise_subscription_update();
DROP FUNCTION IF EXISTS validate_enterprise_key_owner();
DROP FUNCTION IF EXISTS validate_enterprise_usage_attribution();
DROP FUNCTION IF EXISTS reject_enterprise_history_mutation();
DROP FUNCTION IF EXISTS protect_enterprise_subscription_history();
DROP FUNCTION IF EXISTS protect_enterprise_key_assignment_history();

CREATE TABLE enterprise_departments (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    name VARCHAR(255) NOT NULL,
    normalized_name VARCHAR(255) GENERATED ALWAYS AS (LOWER(BTRIM(name))) STORED,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    disabled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_enterprise_departments_name_nonempty CHECK (BTRIM(name) <> ''),
    CONSTRAINT ck_enterprise_departments_status_disabled_at CHECK (
        (status = 'active' AND disabled_at IS NULL)
        OR (status = 'disabled' AND disabled_at IS NOT NULL)
    ),
    UNIQUE (enterprise_id, id)
);

CREATE UNIQUE INDEX uq_enterprise_departments_active_name
    ON enterprise_departments (enterprise_id, normalized_name)
    WHERE status = 'active';

ALTER TABLE enterprise_employees
    ADD COLUMN department_id BIGINT,
    ADD CONSTRAINT enterprise_employees_department_fkey
        FOREIGN KEY (enterprise_id, department_id)
        REFERENCES enterprise_departments (enterprise_id, id) ON DELETE RESTRICT;

CREATE INDEX idx_enterprise_employees_department
    ON enterprise_employees (enterprise_id, department_id)
    WHERE department_id IS NOT NULL;

ALTER TABLE enterprise_subscriptions
    DROP CONSTRAINT ck_enterprise_subscriptions_status_timestamps;
ALTER TABLE enterprise_subscriptions
    RENAME COLUMN effective_window_anchor TO observed_weekly_window_start;

ALTER TABLE enterprise_subscriptions
    ALTER COLUMN observed_weekly_window_start DROP NOT NULL,
    ADD CONSTRAINT ck_enterprise_subscriptions_status_timestamps CHECK (
        (status = 'scheduled' AND observed_weekly_window_start IS NULL AND activated_at IS NULL AND ended_at IS NULL)
        OR (status = 'active' AND observed_weekly_window_start IS NOT NULL AND activated_at IS NOT NULL AND ended_at IS NULL)
        OR (status = 'ended' AND observed_weekly_window_start IS NOT NULL AND activated_at IS NOT NULL AND ended_at IS NOT NULL)
        OR (status = 'cancelled' AND activated_at IS NULL AND ended_at IS NOT NULL)
    );

DROP INDEX IF EXISTS idx_enterprise_subscriptions_effective_anchor;
CREATE UNIQUE INDEX uq_enterprise_subscriptions_one_scheduled
    ON enterprise_subscriptions (enterprise_id)
    WHERE status = 'scheduled';
CREATE INDEX idx_enterprise_subscriptions_observed_window
    ON enterprise_subscriptions (
        enterprise_id,
        upstream_user_subscription_id,
        observed_weekly_window_start DESC
    ) WHERE observed_weekly_window_start IS NOT NULL;

ALTER TABLE enterprise_subscription_windows
    RENAME COLUMN source_anchor TO observed_weekly_window_start;
ALTER TABLE enterprise_subscription_windows
    ADD COLUMN upstream_user_subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
    ADD CONSTRAINT ck_enterprise_subscription_windows_observed_start CHECK (
        observed_weekly_window_start = window_start
    );

CREATE UNIQUE INDEX uq_enterprise_subscription_windows_upstream_window
    ON enterprise_subscription_windows (upstream_user_subscription_id, observed_weekly_window_start);
DROP INDEX IF EXISTS idx_enterprise_subscription_windows_anchor;
CREATE INDEX idx_enterprise_subscription_windows_anchor
    ON enterprise_subscription_windows (
        enterprise_id,
        upstream_user_subscription_id,
        observed_weekly_window_start DESC
    );

DROP INDEX IF EXISTS uq_enterprise_key_assignments_api_key_history;
ALTER TABLE enterprise_key_assignments
    DROP CONSTRAINT ck_enterprise_key_assignments_status_revoked_at,
    ADD COLUMN upstream_user_subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
    ADD COLUMN upstream_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    ADD COLUMN generation BIGINT NOT NULL DEFAULT 1 CHECK (generation >= 1),
    ADD COLUMN ended_at TIMESTAMPTZ,
    ADD CONSTRAINT ck_enterprise_key_assignments_status_times CHECK (
        (status = 'active' AND ended_at IS NULL AND revoked_at IS NULL)
        OR (status = 'ended' AND ended_at IS NOT NULL AND revoked_at IS NULL)
        OR (status = 'revoked' AND ended_at IS NOT NULL AND revoked_at IS NOT NULL AND ended_at = revoked_at)
    ),
    ADD CONSTRAINT ck_enterprise_key_assignments_time_order CHECK (
        ended_at IS NULL OR ended_at >= assigned_at
    ),
    DROP CONSTRAINT enterprise_key_assignments_status_check;
ALTER TABLE enterprise_key_assignments
    ADD CONSTRAINT enterprise_key_assignments_status_check CHECK (status IN ('active', 'ended', 'revoked'));

CREATE UNIQUE INDEX uq_enterprise_key_assignments_active_api_key
    ON enterprise_key_assignments (api_key_id)
    WHERE status = 'active';
CREATE INDEX idx_enterprise_key_assignments_segments
    ON enterprise_key_assignments (api_key_id, generation, assigned_at, ended_at);

ALTER TABLE enterprise_subscription_windows
    ALTER COLUMN allocation_snapshot TYPE NUMERIC(20,8);
ALTER TABLE enterprise_weekly_allocations
    ALTER COLUMN amount TYPE NUMERIC(20,8);
ALTER TABLE enterprise_allocation_revisions
    ALTER COLUMN previous_amount TYPE NUMERIC(20,8),
    ALTER COLUMN new_amount TYPE NUMERIC(20,8);

ALTER TABLE enterprise_usage_attributions
    ADD CONSTRAINT enterprise_usage_attributions_usage_log_id_fkey
        FOREIGN KEY (usage_log_id) REFERENCES usage_logs(id) ON DELETE RESTRICT;
COMMENT ON COLUMN enterprise_usage_attributions.usage_log_id IS
    'Unique foreign key to the authoritative usage_logs row';
