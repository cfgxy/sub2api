-- migration 239 can already have replaced an employee key with a tombstone.
-- The original credential is then unavailable for fingerprint backfill, so do not
-- install a partial guard over a database whose history cannot be reconstructed.
-- Wait for all legacy key mutations before inspecting history. This must remain
-- a top-level statement so the following DO block gets a fresh READ COMMITTED
-- snapshot after the lock is acquired.
LOCK TABLE api_keys IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM api_keys AS api_key
        JOIN enterprise_key_assignments AS assignment
          ON assignment.api_key_id = api_key.id
        WHERE (
                api_key.key LIKE 'revoked-%'
                OR api_key.key LIKE ':revoked:%'
              )
          AND assignment.status IN ('revoked', 'ended')
    ) THEN
        RAISE EXCEPTION 'cannot apply SHAN-153 credential guard after unrecoverable 239-only enterprise key tombstones';
    END IF;
END;
$$;

CREATE TABLE api_key_revoked_credential_reservations (
    fingerprint CHAR(64) PRIMARY KEY CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX idx_api_key_revoked_credential_reservations_key
    ON api_key_revoked_credential_reservations (api_key_id);

CREATE OR REPLACE FUNCTION guard_enterprise_revoked_api_key_credential()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    credential_fingerprint CHAR(64);
    has_employee_assignment BOOLEAN;
BEGIN
    IF TG_OP = 'INSERT' THEN
        credential_fingerprint := encode(sha256(convert_to(NEW.key, 'UTF8')), 'hex');
        PERFORM pg_advisory_xact_lock(hashtextextended(credential_fingerprint, 0));
        IF EXISTS (
            SELECT 1
            FROM api_key_revoked_credential_reservations
            WHERE fingerprint = credential_fingerprint
        ) THEN
            RAISE EXCEPTION 'api key credential has been permanently revoked'
                USING ERRCODE = '23505', CONSTRAINT = 'api_key_revoked_credential_reservations_pkey';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.key IS NOT DISTINCT FROM NEW.key
       AND NOT (OLD.status IS DISTINCT FROM 'disabled' AND NEW.status = 'disabled')
       AND NOT (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL) THEN
        RETURN NEW;
    END IF;

    SELECT EXISTS (
        SELECT 1
        FROM enterprise_key_assignments
        WHERE api_key_id = OLD.id
    ) INTO has_employee_assignment;
    IF NOT has_employee_assignment THEN
        RETURN NEW;
    END IF;

    credential_fingerprint := encode(sha256(convert_to(OLD.key, 'UTF8')), 'hex');
    IF NOT pg_try_advisory_xact_lock(hashtextextended(credential_fingerprint, 0)) THEN
        RAISE EXCEPTION 'concurrent enterprise credential mutation must be retried'
            USING ERRCODE = '40001';
    END IF;

    INSERT INTO api_key_revoked_credential_reservations (fingerprint, api_key_id)
    VALUES (credential_fingerprint, OLD.id)
    ON CONFLICT (fingerprint) DO NOTHING;

    NEW.key := ':revoked:' || OLD.id::text;
    NEW.status := 'disabled';
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_guard_enterprise_revoked_api_key_credential
BEFORE INSERT OR UPDATE OF key, status, deleted_at ON api_keys
FOR EACH ROW EXECUTE FUNCTION guard_enterprise_revoked_api_key_credential();
