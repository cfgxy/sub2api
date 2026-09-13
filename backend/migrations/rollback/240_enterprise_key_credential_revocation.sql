DO $$
BEGIN
    IF to_regclass('public.api_key_revoked_credential_reservations') IS NOT NULL THEN
        -- Freeze key mutations before checking reservations. The lock is held
        -- until this rollback statement finishes, so a concurrent revocation
        -- cannot pass the check and then lose its reservation.
        EXECUTE 'LOCK TABLE api_keys IN ACCESS EXCLUSIVE MODE';
        EXECUTE 'LOCK TABLE api_key_revoked_credential_reservations IN ACCESS EXCLUSIVE MODE';
        IF EXISTS (
            SELECT 1 FROM api_key_revoked_credential_reservations LIMIT 1
        ) THEN
            RAISE EXCEPTION 'cannot rollback SHAN-153 credential guard while revoked credential reservations exist';
        END IF;
    END IF;

    EXECUTE 'DROP TRIGGER IF EXISTS trg_guard_enterprise_revoked_api_key_credential ON api_keys';
    EXECUTE 'DROP FUNCTION IF EXISTS guard_enterprise_revoked_api_key_credential()';
    EXECUTE 'DROP TABLE IF EXISTS api_key_revoked_credential_reservations';

    DELETE FROM schema_migrations
    WHERE filename = '240_enterprise_key_credential_revocation.sql';
END;
$$;
