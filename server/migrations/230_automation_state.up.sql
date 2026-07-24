-- Small durable, row-lockable state for edge latches, stage frontier and rate buckets.
CREATE TABLE automation_state (
    workspace_id UUID NOT NULL,
    state_kind TEXT NOT NULL,
    state_key TEXT NOT NULL,
    state JSONB NOT NULL,
    version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, state_kind, state_key)
);

COMMENT ON TABLE automation_state IS 'Durable row-lockable automation state, not an audit log. No foreign keys.';
