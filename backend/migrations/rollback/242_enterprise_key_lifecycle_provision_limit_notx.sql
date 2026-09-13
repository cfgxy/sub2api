DROP INDEX CONCURRENTLY IF EXISTS idx_enterprise_key_lifecycle_provision_limit;

DELETE FROM schema_migrations
WHERE filename = '242_enterprise_key_lifecycle_provision_limit_notx.sql';
