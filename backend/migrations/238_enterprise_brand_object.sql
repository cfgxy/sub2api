-- SHAN-152 stores enterprise brand backgrounds as server-controlled object keys.
ALTER TABLE enterprise_branding
	ADD COLUMN IF NOT EXISTS enterprise_name VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS background_object_key TEXT NOT NULL DEFAULT '';

UPDATE enterprise_branding AS branding
SET enterprise_name = enterprise.name
FROM enterprises AS enterprise
WHERE enterprise.id = branding.enterprise_id
  AND BTRIM(branding.enterprise_name) = '';

ALTER TABLE enterprise_branding DROP CONSTRAINT IF EXISTS ck_enterprise_branding_background_object;
ALTER TABLE enterprise_branding ADD CONSTRAINT ck_enterprise_branding_background_object CHECK (
    background_object_key = ''
    OR
    (background_object_key <> '' AND background_content_type IN ('image/jpeg', 'image/png', 'image/webp')
        AND background_sha256 ~ '^[0-9a-f]{64}$'
        AND background_size_bytes BETWEEN 1 AND 5242880)
);
