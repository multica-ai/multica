CREATE TABLE daemon_pairing_session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token TEXT NOT NULL UNIQUE,
    daemon_id TEXT NOT NULL,
    device_name TEXT NOT NULL,
    runtime_name TEXT NOT NULL,
    runtime_type TEXT NOT NULL,
    runtime_version TEXT NOT NULL DEFAULT '',
    workspace_id UUID REFERENCES workspace(id) ON DELETE CASCADE,
    approved_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'claimed', 'expired')),
    approved_at TIMESTAMPTZ,
    claimed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_daemon_pairing_session_token ON daemon_pairing_session (token);
CREATE INDEX idx_daemon_pairing_session_status_expires ON daemon_pairing_session (status, expires_at);
