-- Serves the run list's has_recorded_changes lookup without scanning transcripts.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_message_commit_diff
    ON task_message (task_id) WHERE tool = 'multica_commit_diff';
