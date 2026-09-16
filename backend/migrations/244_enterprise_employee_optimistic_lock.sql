-- SHAN-239 employee concurrent-edit optimistic lock.
-- Adds an explicit version column so concurrent admin edits to the same
-- employee are serialized with a compare-and-swap instead of last-write-wins.
ALTER TABLE enterprise_employees
    ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1);
