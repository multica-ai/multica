-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- task_token has carried workspace_id NOT NULL since migration 108, but only
-- token_hash and task_id were indexed. Workspace teardown is the one consumer
-- that filters on workspace_id, and without this index it had no choice but to
-- scan the whole table (MUL-5999).
--
-- The column is also the natural key for any future per-tenant token audit, so
-- the index is not teardown-only.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_token_workspace_id
    ON task_token (workspace_id);
