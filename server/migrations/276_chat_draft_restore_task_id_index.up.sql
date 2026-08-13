-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- chat_draft_restore.task_id is an unindexed FK. Workspace teardown now deletes
-- drafts one bounded task batch at a time (MUL-5999), and without an index each
-- batch would scan the table; the FK's own ON DELETE SET NULL probe has the same
-- problem on every ordinary task delete.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_draft_restore_task_id
    ON chat_draft_restore (task_id);
