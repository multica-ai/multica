-- The workspace's rolling window: the usefulness figure and the Secretary's
-- recent feedback read rows by workspace and updated_at.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_brief_feedback_workspace_updated ON brief_feedback (workspace_id, updated_at);
