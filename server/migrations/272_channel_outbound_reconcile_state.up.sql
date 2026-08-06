CREATE TABLE channel_outbound_reconcile_state (
    channel_type       TEXT PRIMARY KEY,
    cursor_at          TIMESTAMPTZ NOT NULL,
    lease_token        TEXT,
    lease_expires_at   TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
