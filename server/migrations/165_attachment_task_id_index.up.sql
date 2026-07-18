-- Lookup index for BindChatAttachmentsToMessage (find a task's still-unbound
-- rows on chat-task completion). Single-statement + CONCURRENTLY: CREATE INDEX
-- CONCURRENTLY cannot run in a transaction or multi-command string (see
-- 138_issue_title_trgm_index). Partial on task_id IS NOT NULL — only in-flight,
-- not-yet-bound uploads qualify, so the index stays tiny.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_attachment_task
  ON attachment(task_id)
  WHERE task_id IS NOT NULL;
