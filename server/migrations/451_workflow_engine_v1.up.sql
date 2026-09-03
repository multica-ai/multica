CREATE TABLE workflow_definition (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (btrim(name) <> ''),
    version integer NOT NULL CHECK (version >= 1),
    definition jsonb NOT NULL,
    created_by uuid NOT NULL REFERENCES "user"(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name, version)
);

CREATE TABLE workflow_run (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workflow_definition_id uuid NOT NULL REFERENCES workflow_definition(id),
    definition_snapshot jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'blocked_materialization', 'completed_pending_review', 'cancelled')),
    current_stage integer NOT NULL CHECK (current_stage >= 1),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision >= 1),
    started_by_type text NOT NULL CHECK (started_by_type IN ('member', 'agent')),
    started_by_id uuid NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    cancelled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX workflow_run_one_active_per_issue
    ON workflow_run(workspace_id, issue_id)
    WHERE status IN ('running', 'blocked_materialization');

CREATE INDEX workflow_run_issue_history
    ON workflow_run(workspace_id, issue_id, created_at DESC);

CREATE TABLE workflow_transition (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    workflow_run_id uuid NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    idempotency_key text NOT NULL,
    kind text NOT NULL,
    from_stage integer,
    to_stage integer,
    from_status text,
    to_status text NOT NULL,
    actor_type text NOT NULL CHECK (actor_type IN ('system', 'member', 'agent')),
    actor_id uuid,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workflow_run_id, idempotency_key)
);

CREATE INDEX workflow_transition_run_order
    ON workflow_transition(workspace_id, workflow_run_id, created_at, id);


CREATE OR REPLACE FUNCTION enforce_issue_workflow_stage_order()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    active_stage integer;
    active_run_status text;
    stage_count integer;
    effective_status text;
BEGIN
    IF TG_OP = 'UPDATE'
       AND NEW.status IS NOT DISTINCT FROM OLD.status
       AND NEW.stage IS NOT DISTINCT FROM OLD.stage
       AND NEW.parent_issue_id IS NOT DISTINCT FROM OLD.parent_issue_id THEN
        RETURN NEW;
    END IF;
    IF NEW.parent_issue_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT current_stage, status, jsonb_array_length(definition_snapshot->'stages')
    INTO active_stage, active_run_status, stage_count
    FROM workflow_run
    WHERE workspace_id = NEW.workspace_id
      AND issue_id = NEW.parent_issue_id
      AND status IN ('running', 'blocked_materialization')
    ORDER BY created_at DESC
    LIMIT 1;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    SELECT COALESCE((
        SELECT category FROM issue_status
        WHERE workspace_id = NEW.workspace_id AND key = NEW.status
    ), NEW.status) INTO effective_status;

    IF NEW.stage IS NULL OR NEW.stage < 1 OR NEW.stage > stage_count THEN
        IF effective_status IN ('done', 'cancelled') THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'active workflow child must remain on a declared stage'
            USING ERRCODE = '23514', CONSTRAINT = 'issue_workflow_order_guard';
    END IF;

    IF NEW.stage < active_stage
       AND effective_status NOT IN ('done', 'cancelled') THEN
        RAISE EXCEPTION 'completed workflow stages must remain terminal'
            USING ERRCODE = '23514', CONSTRAINT = 'issue_workflow_order_guard';
    END IF;
    IF NEW.stage > active_stage AND effective_status <> 'backlog' THEN
        RAISE EXCEPTION 'future workflow stages must remain in backlog'
            USING ERRCODE = '23514', CONSTRAINT = 'issue_workflow_order_guard';
    END IF;
    IF NEW.stage = active_stage AND active_run_status = 'running' AND effective_status = 'backlog' THEN
        RAISE EXCEPTION 'running workflow stage cannot return to backlog'
            USING ERRCODE = '23514', CONSTRAINT = 'issue_workflow_order_guard';
    END IF;
    IF NEW.stage = active_stage
       AND active_run_status = 'blocked_materialization'
       AND effective_status NOT IN ('backlog', 'done', 'cancelled') THEN
        RAISE EXCEPTION 'blocked workflow stage must be resumed before active work begins'
            USING ERRCODE = '23514', CONSTRAINT = 'issue_workflow_order_guard';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER issue_workflow_order_guard_write
AFTER INSERT OR UPDATE OF status, stage, parent_issue_id ON issue
FOR EACH ROW EXECUTE FUNCTION enforce_issue_workflow_stage_order();
