-- Single-statement CONCURRENTLY migration (see 197). Backs "all events about
-- this subject" scans (a given issue / comment / task) for debug + the future
-- stage sensor:
--   WHERE subject_type = $1 AND subject_id = $2 ORDER BY seq
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_domain_event_subject
    ON domain_event (subject_type, subject_id, seq);
