-- 246_usage_log_audio_token_buckets.sql rollback
ALTER TABLE usage_logs DROP COLUMN IF EXISTS audio_output_cost;
ALTER TABLE usage_logs DROP COLUMN IF EXISTS audio_output_tokens;
ALTER TABLE usage_logs DROP COLUMN IF EXISTS audio_input_cost;
ALTER TABLE usage_logs DROP COLUMN IF EXISTS audio_input_tokens;
