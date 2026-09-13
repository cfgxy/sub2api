CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_enterprise_key_lifecycle_provision_limit
    ON enterprise_key_lifecycle_idempotency (enterprise_id, employee_id, completed_at)
    WHERE completed_at IS NOT NULL AND operation IN ('create', 'rotate');
