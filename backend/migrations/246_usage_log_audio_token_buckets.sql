-- 246_usage_log_audio_token_buckets.sql
-- usage_logs 单独记录 Realtime 音频输入/输出 token 与成本，和现有文本、图片、缓存桶互斥。
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS audio_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS audio_input_cost DECIMAL(20, 10) NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS audio_output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS audio_output_cost DECIMAL(20, 10) NOT NULL DEFAULT 0;
