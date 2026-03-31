-- Re-create the daemon_pairing_session table (from migration 005).
CREATE TABLE IF NOT EXISTS daemon_pairing_session (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token           TEXT        NOT NULL UNIQUE,
    daemon_id       TEXT        NOT NULL,
    device_name     TEXT        NOT NULL DEFAULT '',
    runtime_name    TEXT        NOT NULL DEFAULT '',
    runtime_type    TEXT        NOT NULL DEFAULT '',
    runtime_version TEXT        NOT NULL DEFAULT '',
    workspace_id    UUID        REFERENCES workspace(id),
    approved_by     UUID        REFERENCES "user"(id),
    status          TEXT        NOT NULL DEFAULT 'pending',
    approved_at     TIMESTAMPTZ,
    claimed_at      TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_daemon_pairing_session_token ON daemon_pairing_session(token);
CREATE INDEX IF NOT EXISTS idx_daemon_pairing_session_status ON daemon_pairing_session(status, expires_at);
