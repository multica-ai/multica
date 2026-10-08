-- Bounds both chat resume-history queries to one chat instead of scanning the
-- global terminal-task population. Keep all three terminal states: the
-- completed/failed rollout-missing query implies this wider predicate, while
-- GetLastChatTaskSession also needs cancelled rows. chat_session_id IS NOT NULL
-- excludes issue tasks from this hot write-table index.
--
-- Do NOT add session_id IS NOT NULL. The resume_overflow_at CTE must read
-- codex_resume_oversized failures whose NULL session_id marks the cutoff; a
-- narrower predicate would silently send that CTE back to a full-table scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_terminal_resume
ON agent_task_queue (chat_session_id, session_id, completed_at DESC)
WHERE chat_session_id IS NOT NULL
  AND status IN ('completed', 'failed', 'cancelled');
