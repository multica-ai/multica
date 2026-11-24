-- Agents answer doc discussions (MUL-9933). A run started from a discussion
-- records the doc, the discussion's first comment and the comment that
-- triggered it; an agent's reply records the run that wrote it. No foreign
-- keys: doc delete cancels the doc's runs in application code.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS doc_id UUID,
    ADD COLUMN IF NOT EXISTS doc_thread_id UUID,
    ADD COLUMN IF NOT EXISTS doc_comment_id UUID;

ALTER TABLE doc_comment ADD COLUMN IF NOT EXISTS source_task_id UUID;
