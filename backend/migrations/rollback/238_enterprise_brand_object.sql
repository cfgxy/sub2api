-- Inverse of 238_enterprise_brand_object.sql.
-- The enterprise_name backfill UPDATE has no data-level inverse; the 237
-- preflight guard guarantees the table was empty when 238 ran.
ALTER TABLE enterprise_branding
    DROP CONSTRAINT IF EXISTS ck_enterprise_branding_background_object;
ALTER TABLE enterprise_branding
    DROP COLUMN IF EXISTS enterprise_name,
    DROP COLUMN IF EXISTS background_object_key;
