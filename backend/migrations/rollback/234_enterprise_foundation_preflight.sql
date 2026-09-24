-- Inverse of 234_enterprise_foundation_preflight.sql.
-- 234 is a read-only preflight guard (it blocks migration 235 when
-- pre-existing enterprise tables would collide) and changes no schema, so its
-- rollback is a no-op. The statement keeps the file an executable script.
SELECT 1;
