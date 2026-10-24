-- Keep an explicit task-level limit ahead of workspace defaults.
-- PostgreSQL executes BEFORE INSERT triggers in alphabetical name order.
-- The zzz_ trigger runs after PR #7250's trg_agent_task_apply_workspace_max_attempts.
-- Both the task INSERT and its RETURNING row observe the final limit.
CREATE OR REPLACE FUNCTION enforce_explicit_single_attempt_budget()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.context @> '{"explicit_single_attempt_budget": true}'::jsonb THEN
    NEW.max_attempts := 1;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER zzz_enforce_explicit_single_attempt_budget
  BEFORE INSERT ON agent_task_queue
  FOR EACH ROW EXECUTE FUNCTION enforce_explicit_single_attempt_budget();
