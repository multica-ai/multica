-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- Same keyset-paging reason as migration 278, for teardown's issue path.
--
-- This makes idx_agent_task_queue_issue_id (migration 035) redundant: a btree on
-- (issue_id, id) serves every `issue_id = $1` predicate that the single-column
-- index served, and additionally produces id order. The old index is left in
-- place deliberately. Dropping it needs its own migration with a
-- direction-aware INVALID-index cleanup, because the down path of such a drop
-- is a CREATE INDEX CONCURRENTLY. An interrupted rollback would otherwise leave
-- no usable issue_id index at all.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_issue_id_keyset
    ON agent_task_queue (issue_id, id);
