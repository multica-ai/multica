-- One scorecard per subject and period; the nightly job upserts on this key, so
-- a rerun replaces the row for the same period rather than adding one.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS scorecard_subject_period_uidx
    ON scorecard (workspace_id, subject_type, subject_id, period, period_start);
