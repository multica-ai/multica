-- Workspace teardown keeps the denormalized workspace predicate so it also
-- removes orphaned identities whose installation row is already gone.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_dingtalk_bot_identity_workspace
ON dingtalk_bot_identity (workspace_id);
