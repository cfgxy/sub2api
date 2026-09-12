-- SHAN-152 enterprise portal identity and administration runtime.

-- 237 introduces mandatory portal and credential fields. Existing enterprise
-- records must be explicitly backfilled in a separate migration before this
-- contract can be installed; do not let later NOT NULL checks fail ambiguously.
DO $$
DECLARE
    table_name TEXT;
    row_count BIGINT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'enterprises',
        'enterprise_employees',
        'enterprise_departments',
        'enterprise_subscriptions',
        'enterprise_subscription_windows',
        'enterprise_key_assignments',
        'enterprise_weekly_allocations',
        'enterprise_allocation_revisions',
        'enterprise_usage_attributions',
        'enterprise_audit_events'
    ] LOOP
        EXECUTE format('SELECT COUNT(*) FROM %I', table_name) INTO row_count;
        IF row_count <> 0 THEN
            RAISE EXCEPTION
                'SHAN-152 237 refuses existing enterprise data in % (% rows): explicitly backfill portal_host, admin_user_id, and employee password_hash before running a separate migration',
                table_name, row_count;
        END IF;
    END LOOP;
END
$$;

ALTER TABLE enterprises
    ADD COLUMN IF NOT EXISTS portal_host VARCHAR(255),
    ADD COLUMN IF NOT EXISTS admin_user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_enterprises_portal_host
    ON enterprises (LOWER(BTRIM(portal_host))) WHERE portal_host IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_enterprises_admin_user
    ON enterprises (admin_user_id) WHERE admin_user_id IS NOT NULL;
ALTER TABLE enterprises DROP CONSTRAINT IF EXISTS ck_enterprises_admin_is_dedicated_user;
ALTER TABLE enterprises ADD CONSTRAINT ck_enterprises_admin_is_dedicated_user
    CHECK (admin_user_id IS NULL OR admin_user_id = dedicated_upstream_user_id);
ALTER TABLE enterprises ALTER COLUMN portal_host SET NOT NULL;
ALTER TABLE enterprises ALTER COLUMN admin_user_id SET NOT NULL;

ALTER TABLE enterprise_employees
    ADD COLUMN IF NOT EXISTS current_email VARCHAR(255),
    ADD COLUMN IF NOT EXISTS password_hash TEXT,
    ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS initial_password_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS auth_version BIGINT NOT NULL DEFAULT 1 CHECK (auth_version >= 1),
    ADD COLUMN IF NOT EXISTS terminated_at TIMESTAMPTZ;

UPDATE enterprise_employees
SET current_email = email,
    initial_password_expires_at = COALESCE(initial_password_expires_at, created_at + INTERVAL '24 hours')
WHERE current_email IS NULL AND status <> 'terminated';

ALTER TABLE enterprise_employees DROP CONSTRAINT IF EXISTS enterprise_employees_enterprise_id_normalized_email_key;
ALTER TABLE enterprise_employees DROP CONSTRAINT IF EXISTS enterprise_employees_status_check;
ALTER TABLE enterprise_employees DROP CONSTRAINT IF EXISTS ck_enterprise_employees_status_disabled_at;
ALTER TABLE enterprise_employees ADD CONSTRAINT enterprise_employees_status_check
    CHECK (status IN ('active', 'disabled', 'terminated'));
ALTER TABLE enterprise_employees ADD CONSTRAINT ck_enterprise_employees_lifecycle CHECK (
    (status = 'active' AND disabled_at IS NULL AND terminated_at IS NULL AND current_email IS NOT NULL)
    OR (status = 'disabled' AND disabled_at IS NOT NULL AND terminated_at IS NULL AND current_email IS NOT NULL)
    OR (status = 'terminated' AND terminated_at IS NOT NULL AND current_email IS NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_enterprise_employees_current_email
    ON enterprise_employees (enterprise_id, LOWER(BTRIM(current_email)))
    WHERE status <> 'terminated';
ALTER TABLE enterprise_employees ALTER COLUMN password_hash SET NOT NULL;

CREATE TABLE IF NOT EXISTS enterprise_sessions (
    id UUID PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    principal_type VARCHAR(20) NOT NULL CHECK (principal_type IN ('admin', 'employee')),
    principal_id BIGINT NOT NULL,
    refresh_family_id UUID NOT NULL,
    auth_version BIGINT NOT NULL,
    user_agent VARCHAR(512) NOT NULL DEFAULT '',
    ip_address VARCHAR(64) NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (enterprise_id, id)
);
CREATE INDEX IF NOT EXISTS idx_enterprise_sessions_principal
    ON enterprise_sessions (enterprise_id, principal_type, principal_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_enterprise_sessions_family
    ON enterprise_sessions (enterprise_id, refresh_family_id);

CREATE TABLE IF NOT EXISTS enterprise_refresh_tokens (
    id UUID PRIMARY KEY,
    enterprise_id BIGINT NOT NULL,
    session_id UUID NOT NULL,
    refresh_family_id UUID NOT NULL,
    token_hash CHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (enterprise_id, session_id)
        REFERENCES enterprise_sessions (enterprise_id, id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_enterprise_refresh_tokens_family
    ON enterprise_refresh_tokens (enterprise_id, refresh_family_id, created_at DESC);

CREATE TABLE IF NOT EXISTS enterprise_password_reset_tokens (
    id UUID PRIMARY KEY,
    enterprise_id BIGINT NOT NULL,
    employee_id BIGINT NOT NULL,
    token_hash CHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (enterprise_id, employee_id)
        REFERENCES enterprise_employees (enterprise_id, id) ON DELETE RESTRICT,
    UNIQUE (enterprise_id, id)
);
CREATE INDEX IF NOT EXISTS idx_enterprise_password_reset_employee
    ON enterprise_password_reset_tokens (enterprise_id, employee_id, created_at DESC);

CREATE TABLE IF NOT EXISTS enterprise_branding (
    enterprise_id BIGINT PRIMARY KEY REFERENCES enterprises(id) ON DELETE RESTRICT,
    title VARCHAR(40) NOT NULL DEFAULT '',
    body VARCHAR(120) NOT NULL DEFAULT '',
    slogan VARCHAR(60) NOT NULL DEFAULT '',
    background_url TEXT NOT NULL DEFAULT '',
    background_content_type VARCHAR(32) NOT NULL DEFAULT '',
    background_sha256 CHAR(64) NOT NULL DEFAULT '',
    background_size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (background_size_bytes BETWEEN 0 AND 5242880),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
