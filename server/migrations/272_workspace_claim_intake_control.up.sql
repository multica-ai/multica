BEGIN;

CREATE TABLE workspace_claim_intake_control (
    workspace_id UUID NOT NULL,
    state TEXT NOT NULL DEFAULT 'resumed' CHECK (state IN ('resumed', 'paused')),
    generation BIGINT NOT NULL DEFAULT 0 CHECK (generation >= 0),
    updated_by_type TEXT NOT NULL DEFAULT 'system'
        CHECK (updated_by_type IN ('system', 'member')),
    updated_by_id UUID,
    auth_source TEXT NOT NULL DEFAULT 'system'
        CHECK (auth_source IN ('system', 'session', 'jwt', 'pat')),
    actor_display TEXT NOT NULL DEFAULT 'system',
    reason TEXT NOT NULL DEFAULT 'baseline initialization',
    authoritative_action_id UUID,
    effective_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workspace_claim_intake_action (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('pause', 'resume')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    expected_generation BIGINT,
    requested_at TIMESTAMPTZ NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member')),
    actor_id UUID NOT NULL,
    auth_source TEXT NOT NULL CHECK (auth_source IN ('session', 'jwt', 'pat')),
    actor_display TEXT NOT NULL,
    reason TEXT NOT NULL,
    previous_state TEXT NOT NULL CHECK (previous_state IN ('resumed', 'paused')),
    resulting_state TEXT NOT NULL CHECK (resulting_state IN ('resumed', 'paused')),
    generation BIGINT NOT NULL CHECK (generation >= 0),
    result TEXT NOT NULL CHECK (result IN ('applied', 'noop', 'conflict')),
    error_class TEXT,
    response_status INTEGER NOT NULL CHECK (response_status BETWEEN 200 AND 599),
    response_body BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE agent_task_queue
    ADD COLUMN claim_intake_generation BIGINT,
    ADD COLUMN claim_intake_action_id UUID,
    ADD COLUMN claim_consumer_id TEXT;

CREATE FUNCTION initialize_workspace_claim_intake_control()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO workspace_claim_intake_control (
        workspace_id,
        state,
        generation,
        actor_display,
        reason,
        effective_at,
        created_at,
        updated_at
    ) VALUES (
        NEW.id,
        'resumed',
        0,
        'system',
        'workspace initialization',
        now(),
        now(),
        now()
    );
    RETURN NEW;
END;
$$;

-- Serialize trigger installation and the baseline snapshot against Workspace
-- inserts. Without this lock, a Workspace committed after the trigger is
-- installed but before the backfill snapshot could be initialized twice before
-- migration 273 installs the unique index.
LOCK TABLE workspace IN SHARE ROW EXCLUSIVE MODE;

CREATE TRIGGER workspace_claim_intake_control_initialize
AFTER INSERT ON workspace
FOR EACH ROW
EXECUTE FUNCTION initialize_workspace_claim_intake_control();

INSERT INTO workspace_claim_intake_control (
    workspace_id,
    state,
    generation,
    actor_display,
    reason,
    effective_at,
    created_at,
    updated_at
)
SELECT
    id,
    'resumed',
    0,
    'system',
    'baseline initialization',
    now(),
    now(),
    now()
FROM workspace;

COMMIT;
