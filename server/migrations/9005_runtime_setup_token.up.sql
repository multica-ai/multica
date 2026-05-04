-- Runtime setup tokens — short-lived, single-use tokens used by the
-- one-line installer to bootstrap a new daemon without manual config.
-- Flow:
--   1. Logged-in user clicks "Add runtime" → backend creates a setup token
--      bound to their user_id and the active workspace.
--   2. Installer script on the new machine POSTs the setup token to
--      /api/runtime-setup/exchange and receives server_url + a long-lived
--      personal_access_token to drive the daemon.
--   3. Token is marked used and cannot be redeemed again.

CREATE TABLE IF NOT EXISTS runtime_setup_token (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash    TEXT NOT NULL UNIQUE,
    token_prefix  TEXT NOT NULL,
    user_id       UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    device_label  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    used_pat_id   UUID REFERENCES personal_access_token(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_runtime_setup_token_user      ON runtime_setup_token(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_runtime_setup_token_workspace ON runtime_setup_token(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_runtime_setup_token_expires   ON runtime_setup_token(expires_at) WHERE used_at IS NULL;
