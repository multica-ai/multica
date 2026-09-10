CREATE OR REPLACE FUNCTION sync_automation_execution_task_status()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    execution_status TEXT;
BEGIN
    IF NEW.automation_execution_id IS NULL THEN
        RETURN NEW;
    END IF;

    execution_status := CASE NEW.status
        WHEN 'queued' THEN 'queued'
        WHEN 'deferred' THEN 'queued'
        WHEN 'dispatched' THEN 'running'
        WHEN 'running' THEN 'running'
        WHEN 'waiting_local_directory' THEN 'running'
        WHEN 'completed' THEN 'completed'
        WHEN 'failed' THEN 'failed'
        WHEN 'cancelled' THEN 'cancelled'
        ELSE NULL
    END;

    IF execution_status IS NOT NULL THEN
        UPDATE automation_execution
        SET status = execution_status,
            updated_at = now()
        WHERE id = NEW.automation_execution_id
          -- Leaving a lifecycle status wins over late daemon completion.
          AND status <> 'superseded'
          -- A system retry is another attempt of this same execution, so its
          -- queued insert deliberately re-opens a failed/cancelled attempt.
          -- Other task updates cannot regress an already-terminal execution.
          AND (
              status NOT IN ('completed', 'failed', 'cancelled')
              OR (
                  NEW.retry_of_task_id IS NOT NULL
                  AND NEW.status IN ('queued', 'deferred')
              )
          );
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_sync_automation_execution_task_status
AFTER INSERT OR UPDATE OF status, automation_execution_id ON agent_task_queue
FOR EACH ROW
WHEN (NEW.automation_execution_id IS NOT NULL)
EXECUTE FUNCTION sync_automation_execution_task_status();
