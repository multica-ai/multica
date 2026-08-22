-- Raise the agent-task auto-retry budget so infrastructure-shaped failures
-- get three retries after the first run (max_attempts=4 = attempts 1..4).
--
-- Historical default was 2 (first run + one retry) from 055_task_lease_and_retry.
-- Workspace owners can override per workspace via
--   workspace.settings.agent_task.max_attempts  (integer 1..10)
-- The BEFORE INSERT trigger applies that override on root tasks only; retry
-- children keep the ceiling stamped by CreateRetryTask / retryAttemptCeiling.

ALTER TABLE agent_task_queue
  ALTER COLUMN max_attempts SET DEFAULT 4;

CREATE OR REPLACE FUNCTION agent_task_apply_workspace_max_attempts()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  cfg int;
BEGIN
  -- Root tasks only. Retry/rerun children set parent_task_id and/or
  -- retry_of_task_id and already carry an explicit max_attempts.
  IF NEW.parent_task_id IS NOT NULL
     OR NEW.retry_of_task_id IS NOT NULL
     OR NEW.rerun_of_task_id IS NOT NULL
     OR NEW.attempt <> 1 THEN
    RETURN NEW;
  END IF;

  SELECT NULLIF(btrim(w.settings #>> '{agent_task,max_attempts}'), '')::int
    INTO cfg
  FROM agent a
  JOIN workspace w ON w.id = a.workspace_id
  WHERE a.id = NEW.agent_id;

  IF cfg IS NOT NULL AND cfg BETWEEN 1 AND 10 THEN
    NEW.max_attempts := cfg;
  END IF;

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_agent_task_apply_workspace_max_attempts ON agent_task_queue;
CREATE TRIGGER trg_agent_task_apply_workspace_max_attempts
  BEFORE INSERT ON agent_task_queue
  FOR EACH ROW
  EXECUTE FUNCTION agent_task_apply_workspace_max_attempts();

COMMENT ON FUNCTION agent_task_apply_workspace_max_attempts() IS
  'Applies workspace.settings.agent_task.max_attempts (1..10) to root agent tasks at insert. Absent/invalid → column default (4).';
