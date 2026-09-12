CREATE TABLE enterprise_key_lifecycle_idempotency (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    employee_id BIGINT NOT NULL,
    operation VARCHAR(20) NOT NULL CHECK (operation IN ('create', 'disable', 'rotate')),
    idempotency_key VARCHAR(128) NOT NULL,
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    result_api_key_id BIGINT REFERENCES api_keys(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    FOREIGN KEY (enterprise_id, employee_id)
        REFERENCES enterprise_employees (enterprise_id, id) ON DELETE RESTRICT,
    CONSTRAINT ck_enterprise_key_lifecycle_completed CHECK (
        (result_api_key_id IS NULL AND completed_at IS NULL)
        OR (result_api_key_id IS NOT NULL AND completed_at IS NOT NULL)
    ),
    UNIQUE (enterprise_id, employee_id, operation, idempotency_key)
);

CREATE INDEX idx_enterprise_key_lifecycle_idempotency_created
    ON enterprise_key_lifecycle_idempotency (created_at);
