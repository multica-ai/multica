CREATE TABLE agent_terminal_control_event (
    id UUID PRIMARY KEY,
    terminal_session_id UUID NOT NULL,
    user_id UUID NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN ('claim', 'renew', 'release', 'disconnect', 'expire')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
