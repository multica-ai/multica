ALTER TABLE project
    ADD COLUMN max_runs_per_issue integer CHECK (max_runs_per_issue > 0),
    ADD COLUMN max_runs_per_agent_per_issue integer CHECK (max_runs_per_agent_per_issue > 0);

CREATE FUNCTION enforce_issue_run_limits() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    issue_limit integer;
    agent_limit integer;
    issue_runs bigint;
    agent_runs bigint;
BEGIN
    SELECT p.max_runs_per_issue, p.max_runs_per_agent_per_issue
    INTO issue_limit, agent_limit
    FROM issue i
    JOIN project p ON p.id = i.project_id AND p.workspace_id = i.workspace_id
    WHERE i.id = NEW.issue_id;

    IF issue_limit IS NULL AND agent_limit IS NULL THEN
        RETURN NEW;
    END IF;

    -- Different agents can be queued for one issue concurrently. Serialize the
    -- count and insertion independently of the task-owner row lock order.
    PERFORM pg_advisory_xact_lock(hashtextextended('issue-run-limit:' || NEW.issue_id::text, 0));

    SELECT count(*), count(*) FILTER (WHERE agent_id = NEW.agent_id)
    INTO issue_runs, agent_runs
    FROM agent_task_queue
    WHERE issue_id = NEW.issue_id;

    -- A retry that writes no row can still commit the parent's terminal state.
    IF NEW.parent_task_id IS NOT NULL AND (
        (issue_limit IS NOT NULL AND issue_runs >= issue_limit)
        OR (agent_limit IS NOT NULL AND agent_runs >= agent_limit)
    ) THEN
        RETURN NULL;
    END IF;

    IF issue_limit IS NOT NULL AND issue_runs >= issue_limit THEN
        RAISE check_violation USING CONSTRAINT = 'issue_run_limit', MESSAGE = 'issue run limit reached';
    END IF;

    IF agent_limit IS NOT NULL AND agent_runs >= agent_limit THEN
        RAISE check_violation USING CONSTRAINT = 'issue_agent_run_limit', MESSAGE = 'agent run limit for this issue reached';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER enforce_issue_run_limits
BEFORE INSERT ON agent_task_queue
FOR EACH ROW WHEN (NEW.issue_id IS NOT NULL)
EXECUTE FUNCTION enforce_issue_run_limits();
