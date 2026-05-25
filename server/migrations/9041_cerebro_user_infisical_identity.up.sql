-- Per-user scoped Infisical machine identity. The admin identity (backend env)
-- provisions one of these for every user that has an allowed-folder entry;
-- the identity's additional-privileges in Infisical restrict it to ONLY the
-- paths in cerebro_user_infisical_folder for that (workspace_id, user_id).
--
-- Agent secret fetches at claim time go through this scoped identity, so an
-- internal filtering bug in Multica still fails closed at Infisical: a path
-- the admin never granted is not reachable from this identity at all.
--
-- client_secret is encrypted with MULTICA_CREDENTIALS_KEY (AES-256-GCM via
-- internal/cerebro/credentials). Plaintext never lands in the row, in logs,
-- or in any API response.

CREATE TABLE IF NOT EXISTS cerebro_user_infisical_identity (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    identity_id text NOT NULL,
    client_id text NOT NULL,
    client_secret_ciphertext bytea NOT NULL,
    scoped_paths_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_user_infisical_identity_identity_id_not_empty CHECK (btrim(identity_id) <> ''),
    CONSTRAINT cerebro_user_infisical_identity_client_id_not_empty CHECK (btrim(client_id) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS cerebro_user_infisical_identity_ws_user_idx
    ON cerebro_user_infisical_identity (workspace_id, user_id);
