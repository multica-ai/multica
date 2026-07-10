-- Task-scoped attachment ownership. An agent producing an image/file for a
-- chat reply uploads it during the run tagged with the producing task; on
-- task completion the server binds the task's still-unclaimed rows to the
-- assistant chat_message it synthesizes. ON DELETE SET NULL keeps a
-- already-bound attachment alive if its task row is later GC'd — the durable
-- owner is chat_message_id, task_id is only the transient binding handle.
ALTER TABLE attachment
  ADD COLUMN task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL;

-- The task_id lookup index is built CONCURRENTLY in the next migration.
-- CREATE INDEX CONCURRENTLY cannot share a transaction or multi-command
-- string with the ADD COLUMN above (see 138_issue_title_trgm_index).
