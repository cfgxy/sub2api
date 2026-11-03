DROP INDEX IF EXISTS idx_enterprise_employee_reservations_expired_lease;

ALTER TABLE enterprise_employee_usage_reservations
    DROP CONSTRAINT IF EXISTS ck_enterprise_employee_reservation_terminal_state;

ALTER TABLE enterprise_employee_usage_reservations
    ADD CONSTRAINT ck_enterprise_employee_reservation_terminal_state CHECK (
        (status = 'reserved' AND actual_usd IS NULL AND committed_at IS NULL AND settled_at IS NULL AND released_at IS NULL)
        OR (status = 'committed' AND actual_usd IS NOT NULL AND committed_at IS NOT NULL AND settled_at IS NULL AND released_at IS NULL)
        OR (status = 'settled' AND actual_usd IS NOT NULL AND committed_at IS NOT NULL AND settled_at IS NOT NULL AND released_at IS NULL)
        OR (status = 'released' AND actual_usd IS NULL AND committed_at IS NULL AND settled_at IS NULL AND released_at IS NOT NULL)
    );

ALTER TABLE enterprise_employee_usage_reservations
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS maximum_cost_known;
