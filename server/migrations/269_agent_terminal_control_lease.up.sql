CREATE TABLE agent_terminal_control_lease (
    session_id UUID PRIMARY KEY,
    controller_user_id UUID NOT NULL,
    lease_token_hash BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
