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
