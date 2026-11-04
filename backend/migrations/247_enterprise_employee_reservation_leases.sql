ALTER TABLE enterprise_employee_usage_reservations
    ADD COLUMN IF NOT EXISTS maximum_cost_known BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ;

UPDATE enterprise_employee_usage_reservations
SET lease_expires_at = reserved_at + INTERVAL '15 minutes'
WHERE status = 'reserved' AND lease_expires_at IS NULL;

ALTER TABLE enterprise_employee_usage_reservations
    DROP CONSTRAINT IF EXISTS ck_enterprise_employee_reservation_terminal_state;

ALTER TABLE enterprise_employee_usage_reservations
    ADD CONSTRAINT ck_enterprise_employee_reservation_terminal_state CHECK (
        (status = 'reserved' AND actual_usd IS NULL AND committed_at IS NULL AND settled_at IS NULL AND released_at IS NULL AND lease_expires_at IS NOT NULL)
        OR (status = 'committed' AND actual_usd IS NOT NULL AND committed_at IS NOT NULL AND settled_at IS NULL AND released_at IS NULL AND lease_expires_at IS NULL)
        OR (status = 'settled' AND actual_usd IS NOT NULL AND committed_at IS NOT NULL AND settled_at IS NOT NULL AND released_at IS NULL AND lease_expires_at IS NULL)
        OR (status = 'released' AND actual_usd IS NULL AND committed_at IS NULL AND settled_at IS NULL AND released_at IS NOT NULL AND lease_expires_at IS NULL)
    );

CREATE INDEX IF NOT EXISTS idx_enterprise_employee_reservations_expired_lease
    ON enterprise_employee_usage_reservations (lease_expires_at, id)
    WHERE status = 'reserved';
