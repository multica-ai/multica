-- Per-task signal that the daemon withheld this task's Codex session from the
-- (agent_id, issue_id) resume lookup because its rollout was never written to
-- the per-issue store (MUL-5305). When the next follow-up falls back to an
-- older good session, the claim reads this flag on the most recent terminal
-- task to still disclose that the most recent turn's context could not be
-- carried over (PriorSessionResumeUnavailable), preserving MUL-4424's
-- transparent-degradation contract. Default FALSE keeps every existing task
-- and every non-Codex task unaffected.
ALTER TABLE agent_task_queue
  ADD COLUMN session_rollout_missing BOOLEAN NOT NULL DEFAULT FALSE;
