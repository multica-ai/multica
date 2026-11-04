-- Delivery history for the built-in metrics (spec 4.1: delivery.cycle_time_days
-- and delivery.rework_rate) and the scorecards (spec 4.6). done_at is the time
-- the issue last entered a done status; reopen_count counts transitions out of
-- done, and last_reopened_at is the latest one. Migration 881 keeps them current
-- on every status write, from every path.
--
-- Issues done before this migration have no done_at. Readers fall back to
-- updated_at for the done time, as delivery.issues_done already did, and no
-- reopen history exists for them.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS done_at TIMESTAMPTZ;
ALTER TABLE issue ADD COLUMN IF NOT EXISTS reopen_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE issue ADD COLUMN IF NOT EXISTS last_reopened_at TIMESTAMPTZ;
