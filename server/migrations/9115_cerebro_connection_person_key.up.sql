-- Per-person session keys for workspace connections (FIR-2564 fase 2).
--
-- When a connection has session exchange enabled, the backend exchanges the
-- connection's shared system key for the triggering person's own short-lived
-- key (POST <connection>/sessions/exchange on the Firtal Data Registry) and
-- caches it here, one row per (connection, member). Data calls then run on
-- the person's key, so the remote API enforces THAT person's access and a
-- revoked person stops immediately without touching anyone else.
--
-- key_ciphertext is encrypted with MULTICA_CREDENTIALS_KEY (AES-256-GCM via
-- internal/cerebro/credentials). Plaintext never lands in the row, in logs,
-- or in any API response. Rows are cache entries: expired ones are replaced
-- on the next exchange and are never served (expires_at is always checked).

CREATE TABLE IF NOT EXISTS cerebro_connection_person_key (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id uuid NOT NULL,
    member_id uuid NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    key_ciphertext bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS cerebro_connection_person_key_conn_member_idx
    ON cerebro_connection_person_key (connection_id, member_id);
