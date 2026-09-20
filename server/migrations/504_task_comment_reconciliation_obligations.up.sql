-- Separate durable follow-up obligations from the inputs delivered to a task.
-- This permits another execution user's comment to wait for the serialized
-- task slot without exposing that instruction to the current task's runtime.
ALTER TABLE agent_task_queue ADD COLUMN reconciliation_comment_ids UUID[] NOT NULL DEFAULT '{}';
