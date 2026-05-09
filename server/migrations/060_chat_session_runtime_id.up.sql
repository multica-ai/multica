ALTER TABLE chat_session
ADD COLUMN runtime_id UUID REFERENCES agent_runtime(id) ON DELETE SET NULL;

-- Backfill only sessions with a recorded resume pointer from a completed or
-- failed task. Sessions with no prior task remain NULL and will fail closed
-- until a future successful task writes a session_id/runtime_id pair.
UPDATE chat_session cs
SET runtime_id = latest.runtime_id
FROM (
    SELECT DISTINCT ON (chat_session_id)
        chat_session_id,
        runtime_id,
        session_id
    FROM agent_task_queue
    WHERE chat_session_id IS NOT NULL
      AND session_id IS NOT NULL
      AND status IN ('completed', 'failed')
    ORDER BY chat_session_id, COALESCE(completed_at, started_at, dispatched_at, created_at) DESC
) latest
WHERE latest.chat_session_id = cs.id
  AND latest.session_id = cs.session_id;
