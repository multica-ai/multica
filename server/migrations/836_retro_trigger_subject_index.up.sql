-- The window check reads the newest firing of one subject.
CREATE INDEX CONCURRENTLY IF NOT EXISTS retro_trigger_subject_fired_idx ON retro_trigger (workspace_id, subject_type, subject_id, fired_at DESC);
