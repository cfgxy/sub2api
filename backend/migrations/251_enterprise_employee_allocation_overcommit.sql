-- SHAN-580: employee allocation overcommit is opt-in per enterprise.
-- Existing rows and new enterprises both default to the safe (disabled) policy.
ALTER TABLE enterprises
    ADD COLUMN IF NOT EXISTS allow_employee_allocation_overcommit BOOLEAN NOT NULL DEFAULT FALSE;
