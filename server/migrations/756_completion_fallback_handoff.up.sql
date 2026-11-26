-- Durable identity and recovery state for a platform-generated delegated
-- completion fallback (GH #8719). The exact comment ID is the fallback
-- identity; NULL state is legacy/not-applicable, and only explicit pending
-- rows are replayed. No default avoids rewriting the task table.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS completion_fallback_comment_id UUID;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS completion_fallback_state TEXT
  CHECK (
    completion_fallback_state IS NULL
    OR completion_fallback_state IN ('pending', 'recorded', 'settled')
  );
