-- Rollback only removes the capacity column. The dedicated account concurrency
-- is deliberately retained: its pre-migration value is not recoverable and
-- overwriting a later operator change would be unsafe.
ALTER TABLE enterprises
    DROP CONSTRAINT IF EXISTS chk_enterprises_seat_count;
ALTER TABLE enterprises
    DROP COLUMN IF EXISTS seat_count;
