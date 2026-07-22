-- Account patrol cycle history (append-only operational log).
-- Stores each scheduled connectivity patrol batch for admin review.

CREATE TABLE IF NOT EXISTS account_patrol_records (
    id BIGSERIAL PRIMARY KEY,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    batch_size INT NOT NULL DEFAULT 0,
    success_count INT NOT NULL DEFAULT 0,
    failed_count INT NOT NULL DEFAULT 0,
    cursor_after BIGINT NOT NULL DEFAULT 0,
    interval_minutes INT NOT NULL DEFAULT 0,
    concurrency INT NOT NULL DEFAULT 0,
    failed_account_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    note TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_account_patrol_records_finished_id
    ON account_patrol_records (finished_at DESC, id DESC);
