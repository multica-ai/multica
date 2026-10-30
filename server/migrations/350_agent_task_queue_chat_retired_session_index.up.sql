-- Retired sessions need their own sparse index: the database has no CHECK
-- constraint proving retired_session_id is present only on terminal rows, so
-- folding this into migration 349's terminal predicate could resurrect a
-- retired session by silently omitting a valid non-terminal marker.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_retired_session
ON agent_task_queue (chat_session_id, retired_session_id)
WHERE chat_session_id IS NOT NULL
  AND retired_session_id IS NOT NULL;
