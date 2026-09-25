CREATE FUNCTION settle_terminal_task_supplements() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status IN ('queued', 'deferred', 'dispatched', 'waiting_local_directory', 'running')
       AND NEW.status IN ('completed', 'failed', 'cancelled') THEN
        UPDATE task_supplement
        SET status = 'failed',
            failure_reason = 'turn_ended',
            updated_at = now()
        WHERE task_id = NEW.id
          AND status IN ('pending', 'delivering');
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_settle_terminal_task_supplements
AFTER UPDATE OF status ON agent_task_queue
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION settle_terminal_task_supplements();
