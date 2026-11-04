-- One review per subject and period start, so the monthly routine cannot review
-- the same subject twice.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS review_subject_period_uidx
    ON review (workspace_id, subject_type, subject_id, period_start);
