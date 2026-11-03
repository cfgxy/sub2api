CREATE TABLE IF NOT EXISTS enterprise_employee_usage_reservations (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    request_fingerprint CHAR(64) NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    source_api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    enterprise_subscription_id BIGINT NOT NULL,
    upstream_user_subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
    employee_id BIGINT NOT NULL,
    assignment_generation BIGINT NOT NULL CHECK (assignment_generation >= 1),
    weekly_window_anchor TIMESTAMPTZ NOT NULL,
    reserved_usd NUMERIC(20,8) NOT NULL CHECK (reserved_usd > 0),
    actual_usd NUMERIC(20,8) CHECK (actual_usd >= 0),
    status VARCHAR(20) NOT NULL DEFAULT 'reserved' CHECK (status IN ('reserved', 'committed', 'settled', 'released')),
    reserved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    committed_at TIMESTAMPTZ,
    settled_at TIMESTAMPTZ,
    released_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (enterprise_id, enterprise_subscription_id)
        REFERENCES enterprise_subscriptions (enterprise_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (enterprise_id, employee_id)
        REFERENCES enterprise_employees (enterprise_id, id) ON DELETE RESTRICT,
    CONSTRAINT ck_enterprise_employee_reservation_terminal_state CHECK (
        (status = 'reserved' AND actual_usd IS NULL AND committed_at IS NULL AND settled_at IS NULL AND released_at IS NULL)
        OR (status = 'committed' AND actual_usd IS NOT NULL AND committed_at IS NOT NULL AND settled_at IS NULL AND released_at IS NULL)
        OR (status = 'settled' AND actual_usd IS NOT NULL AND committed_at IS NOT NULL AND settled_at IS NOT NULL AND released_at IS NULL)
        OR (status = 'released' AND actual_usd IS NULL AND committed_at IS NULL AND settled_at IS NULL AND released_at IS NOT NULL)
    ),
    UNIQUE (request_id, source_api_key_id)
);

CREATE INDEX IF NOT EXISTS idx_enterprise_employee_reservations_active_window
    ON enterprise_employee_usage_reservations (
        employee_id, enterprise_subscription_id, weekly_window_anchor, status
    );

CREATE INDEX IF NOT EXISTS idx_enterprise_employee_reservations_committed_recovery
    ON enterprise_employee_usage_reservations (committed_at, id)
    WHERE status = 'committed';

COMMENT ON TABLE enterprise_employee_usage_reservations IS
    'Request-lifecycle holds and committed employee settlements; api_keys.usage_7d remains settled usage';
