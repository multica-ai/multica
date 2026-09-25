-- Index the task-owned input-batch lookup added in MUL-4351.
--
-- The direct-chat claim path loads its input with
--   SELECT * FROM chat_message WHERE task_id = $1 AND role = 'user' ORDER BY created_at
-- (ListChatInputMessages), keyed on the task's chat_input_task_id. Before this
-- there was no index on chat_message.task_id at all (only the
-- chat_session_id/created_at index from migration 033), so the lookup would seq
-- scan. This partial index covers exactly the user-message batch reads and the
-- existing task_id-keyed writes (LinkChatMessageToTask,
-- DeleteUserChatMessageByTask) without indexing assistant rows.
--
-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_message_input_owner
    ON chat_message (task_id, created_at)
    WHERE role = 'user';
