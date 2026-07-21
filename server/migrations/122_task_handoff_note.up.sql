-- Handoff note: a first-class, optional free-text instruction attached when an
-- issue is assigned/promoted to an agent or squad (MUL-3375). The daemon
-- renders it into the run's opening prompt and issue_context.md via a dedicated
-- "assignment handoff" branch — NOT by fabricating a comment or reusing
-- trigger_comment_id. NULL means "no handoff note" (today's behavior).
ALTER TABLE agent_task_queue ADD COLUMN handoff_note TEXT;
