-- Trace pointer to the agent_task_queue row that produced this comment, used by
-- the "retry failed agent comment" affordance. No FK on source_task_id: the
-- referenced task can be GC'd independently of the comment it produced, and the
-- relationship is resolved in the application layer (a missing task simply means
-- the source is no longer retryable). Matches the repo rule that foreign keys
-- and cascades are handled by the app, not the database.
ALTER TABLE comment
  ADD COLUMN source_task_id UUID;
