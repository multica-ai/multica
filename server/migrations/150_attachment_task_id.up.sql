-- Task-scoped attachment ownership. An agent producing an image/file for a
-- chat reply uploads it during the run tagged with the producing task; on
-- task completion the server binds the task's still-unclaimed rows to the
-- assistant chat_message it synthesizes. ON DELETE SET NULL keeps a
-- already-bound attachment alive if its task row is later GC'd — the durable
-- owner is chat_message_id, task_id is only the transient binding handle.
ALTER TABLE attachment
  ADD COLUMN task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL;

CREATE INDEX idx_attachment_task
  ON attachment(task_id)
  WHERE task_id IS NOT NULL;
