-- Task-scoped attachment ownership. An agent producing an image/file for a
-- chat reply uploads it during the run tagged with the producing task; on task
-- completion the server binds the task's still-unclaimed rows to the assistant
-- chat_message it synthesizes (see BindChatAttachmentsToMessage).
--
-- task_id is a TRANSIENT binding handle, not a durable relationship: it is
-- written once at upload (against a task the upload handler has already
-- validated) and read only during that task's own completion. The durable
-- owner is chat_message_id. We deliberately add NO foreign key here:
--   - No referential integrity is needed — the sole writer validates the task
--     before setting the column, so a dangling/garbage value cannot get in.
--   - No cascade cleanup is needed — orphan uploads (task_id set,
--     chat_message_id NULL) are already reaped when their chat_session is
--     deleted, via attachment.chat_session_id's own ON DELETE CASCADE. There is
--     no app-layer path that hard-deletes agent_task_queue rows, so an
--     ON DELETE action here would never fire in practice; adding an FK on the
--     hot attachment table would only add write overhead and a cascade
--     dependency the app does not rely on.
ALTER TABLE attachment
  ADD COLUMN task_id UUID;

-- The task_id lookup index is built CONCURRENTLY in the next migration.
-- CREATE INDEX CONCURRENTLY cannot share a transaction or multi-command
-- string with the ADD COLUMN above (see 138_issue_title_trgm_index).
