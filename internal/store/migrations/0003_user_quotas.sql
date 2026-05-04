-- Per-user storage and upload quotas (admin-editable).

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS quota_max_pool_bytes BIGINT NOT NULL DEFAULT 104857600,
    ADD COLUMN IF NOT EXISTS quota_max_single_file_bytes BIGINT NOT NULL DEFAULT 10485760,
    ADD COLUMN IF NOT EXISTS quota_daily_max_uploads INTEGER NOT NULL DEFAULT 10;

CREATE TABLE IF NOT EXISTS user_daily_uploads (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    upload_date DATE NOT NULL,
    upload_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, upload_date)
);

CREATE INDEX IF NOT EXISTS user_daily_uploads_upload_date_idx ON user_daily_uploads (upload_date);
