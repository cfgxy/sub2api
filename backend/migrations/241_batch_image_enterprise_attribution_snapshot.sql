ALTER TABLE batch_image_jobs
    ADD COLUMN enterprise_attribution_snapshot JSONB;

COMMENT ON COLUMN batch_image_jobs.enterprise_attribution_snapshot IS
    '批量生图提交时冻结的企业用量归属快照，不包含 API Key 明文';
