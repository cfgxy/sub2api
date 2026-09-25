-- Inverse of 237_enterprise_identity.sql.
-- The 237 preflight guard guarantees the enterprise tables were empty when the
-- migration ran, so the backfill UPDATE needs no data-level inverse.

-- 237-created auth and branding tables, children first.
DROP TABLE IF EXISTS enterprise_refresh_tokens;
DROP TABLE IF EXISTS enterprise_password_reset_tokens;
DROP TABLE IF EXISTS enterprise_sessions;
DROP TABLE IF EXISTS enterprise_branding;

-- employees: drop the 237 credential columns, restore the 235 originals.
DROP INDEX IF EXISTS uq_enterprise_employees_current_email;
ALTER TABLE enterprise_employees
    DROP CONSTRAINT IF EXISTS ck_enterprise_employees_lifecycle;
ALTER TABLE enterprise_employees
    DROP COLUMN IF EXISTS must_change_password,
    DROP COLUMN IF EXISTS auth_version,
    DROP COLUMN IF EXISTS initial_password_expires_at,
    DROP COLUMN IF EXISTS password_changed_at,
    DROP COLUMN IF EXISTS terminated_at,
    DROP COLUMN IF EXISTS current_email,
    DROP COLUMN IF EXISTS password_hash;
ALTER TABLE enterprise_employees
    DROP CONSTRAINT IF EXISTS enterprise_employees_status_check;
ALTER TABLE enterprise_employees
    ADD CONSTRAINT enterprise_employees_enterprise_id_normalized_email_key
        UNIQUE (enterprise_id, normalized_email),
    ADD CONSTRAINT enterprise_employees_status_check
        CHECK (status IN ('active', 'disabled')),
    ADD CONSTRAINT ck_enterprise_employees_status_disabled_at CHECK (
        (status = 'active' AND disabled_at IS NULL)
        OR (status = 'disabled' AND disabled_at IS NOT NULL)
    );

-- enterprises: drop the portal/admin identity columns and their indexes.
DROP INDEX IF EXISTS uq_enterprises_portal_host;
DROP INDEX IF EXISTS uq_enterprises_admin_user;
ALTER TABLE enterprises
    DROP CONSTRAINT IF EXISTS ck_enterprises_admin_is_dedicated_user;
ALTER TABLE enterprises
    ALTER COLUMN portal_host DROP NOT NULL,
    ALTER COLUMN admin_user_id DROP NOT NULL;
ALTER TABLE enterprises
    DROP COLUMN IF EXISTS portal_host,
    DROP COLUMN IF EXISTS admin_user_id;
