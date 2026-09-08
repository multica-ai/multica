CREATE TABLE issue_controller (
    issue_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    scope_revision text NOT NULL,
    revision bigint NOT NULL DEFAULT 0,
    authority_epoch bigint NOT NULL DEFAULT 1,
    is_stopped boolean NOT NULL DEFAULT false,
    config jsonb NOT NULL,
    state jsonb NOT NULL DEFAULT '{}',
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE controller_event (
    workspace_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    event_id text NOT NULL,
    request_hash text NOT NULL,
    revision bigint NOT NULL,
    body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE controlled_run (
    run_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    action_id text NOT NULL,
    manifest jsonb NOT NULL,
    manifest_hash text NOT NULL,
    receipt jsonb NOT NULL DEFAULT '{}',
    phase text NOT NULL DEFAULT 'queued',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE controller_effect (
    workspace_id uuid NOT NULL,
    resource_key text NOT NULL,
    operation_id text NOT NULL,
    issue_id uuid NOT NULL,
    candidate_identity text NOT NULL,
    authority_epoch bigint NOT NULL,
    fencing_token bigint NOT NULL DEFAULT 1,
    lease_expires_at timestamptz NOT NULL,
    phase text NOT NULL DEFAULT 'reserved',
    receipt jsonb NOT NULL DEFAULT '{}'
);
CREATE TABLE controller_outbox (
    workspace_id uuid NOT NULL,
    event_id text NOT NULL,
    issue_id uuid NOT NULL,
    revision bigint NOT NULL,
    body jsonb NOT NULL,
    delivered_at timestamptz
);
