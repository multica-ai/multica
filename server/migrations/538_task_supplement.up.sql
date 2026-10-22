-- Keep negotiated capability outside agent_task_queue. Older application
-- builds use SELECT * for that hot table, so adding columns would make a
-- server rollback fail while scanning otherwise-valid task rows.
CREATE TABLE task_supplement_capability (
    task_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    capability TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Durable delivery receipts for explicit, human-authored additions to one
-- exact run. Relationships intentionally have no database foreign keys: task,
-- issue, comment and workspace teardown remain application-owned and never
-- acquire a new cascade/lock tree.
CREATE TABLE task_supplement (
    task_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    comment_id UUID NOT NULL,
    author_id UUID NOT NULL,
    client_request_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'delivering', 'delivered', 'failed')),
    failure_reason TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ
);

-- Terminal transition and receipt settlement share the task row update's
-- transaction and lock. This prevents a run from ending between two
-- application statements and leaving a pending receipt that can never move.
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
FOR EACH ROW EXECUTE FUNCTION settle_terminal_task_supplements();
