-- SHAN-152 migrations 237/238 的开发环境补偿回滚脚本。
-- 仅允许在企业身份功能尚未写入业务数据时执行；生产回滚应优先恢复升级前备份。

DO $$
DECLARE
    table_name TEXT;
    row_count BIGINT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'enterprise_password_reset_tokens',
        'enterprise_refresh_tokens',
        'enterprise_sessions',
        'enterprise_branding'
    ] LOOP
        EXECUTE format('SELECT COUNT(*) FROM %I', table_name) INTO row_count;
        IF row_count <> 0 THEN
            RAISE EXCEPTION 'SHAN-152 rollback requires empty table %, found % rows', table_name, row_count;
        END IF;
    END LOOP;

    SELECT COUNT(*) INTO row_count
    FROM enterprise_employees
    WHERE password_hash IS NOT NULL
       OR current_email IS DISTINCT FROM email
       OR terminated_at IS NOT NULL
       OR auth_version <> 1;
    IF row_count <> 0 THEN
        RAISE EXCEPTION 'SHAN-152 rollback requires no enterprise employee identity data, found % rows', row_count;
    END IF;

    SELECT COUNT(*) INTO row_count
    FROM enterprises
    WHERE portal_host IS NOT NULL OR admin_user_id IS NOT NULL;
    IF row_count <> 0 THEN
        RAISE EXCEPTION 'SHAN-152 rollback requires no enterprise portal mapping data, found % rows', row_count;
    END IF;
END
$$;

DROP TABLE enterprise_password_reset_tokens;
DROP TABLE enterprise_refresh_tokens;
DROP TABLE enterprise_sessions;
DROP TABLE enterprise_branding;

DROP INDEX IF EXISTS uq_enterprise_employees_current_email;
ALTER TABLE enterprise_employees
    DROP CONSTRAINT ck_enterprise_employees_lifecycle,
    DROP CONSTRAINT enterprise_employees_status_check,
    DROP COLUMN current_email,
    DROP COLUMN password_hash,
    DROP COLUMN must_change_password,
    DROP COLUMN initial_password_expires_at,
    DROP COLUMN password_changed_at,
    DROP COLUMN auth_version,
    DROP COLUMN terminated_at;
ALTER TABLE enterprise_employees
    ADD CONSTRAINT enterprise_employees_status_check CHECK (status IN ('active', 'disabled')),
    ADD CONSTRAINT ck_enterprise_employees_status_disabled_at CHECK (
        (status = 'active' AND disabled_at IS NULL)
        OR (status = 'disabled' AND disabled_at IS NOT NULL)
    ),
    ADD CONSTRAINT enterprise_employees_enterprise_id_normalized_email_key
        UNIQUE (enterprise_id, normalized_email);

ALTER TABLE enterprises
    DROP CONSTRAINT ck_enterprises_admin_is_dedicated_user,
    DROP COLUMN portal_host,
    DROP COLUMN admin_user_id;

DELETE FROM schema_migrations
WHERE filename IN (
    '237_enterprise_identity.sql',
    '238_enterprise_brand_object.sql'
);
