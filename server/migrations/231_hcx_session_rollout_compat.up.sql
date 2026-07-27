-- HCX deployed the provider-failover migrations through 229 before upstream's
-- independent 224_agent_task_session_rollout_missing migration was merged.
-- Migration identity is the full filename, so clean installs apply upstream
-- 224 normally; existing HCX databases need this idempotent bridge to receive
-- the column without replaying or rewriting production migration history.
ALTER TABLE agent_task_queue
  ADD COLUMN IF NOT EXISTS session_rollout_missing BOOLEAN NOT NULL DEFAULT FALSE;
