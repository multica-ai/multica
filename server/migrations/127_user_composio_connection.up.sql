-- Composio integration (Stage 2 MVP): one row per user-connected Composio
-- account. The row is the local mirror of a Composio "connected account" so the
-- product can list / disconnect connections and build per-user MCP sessions
-- without round-tripping Composio on every read.
--
-- No foreign keys / cascades by design: Multica enforces cross-table
-- relationships at the application layer (see migration 118 dropping the
-- agent_task_queue.initiator_user_id FK). user_id is a "user".id but is left
-- unconstrained here so a user delete does not require a migration-ordered
-- cascade across integration tables.
--
-- composio_user_id always equals the Multica user_id.String() — the
-- application keeps that mapping as an invariant so a Composio session can be
-- created from the Multica user id alone. It is stored explicitly so a future
-- change to the mapping does not silently break already-connected accounts.
CREATE TABLE user_composio_connection (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID NOT NULL,
    toolkit_slug         TEXT NOT NULL,
    auth_config_id       TEXT NOT NULL,
    connected_account_id TEXT NOT NULL,
    composio_user_id     TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'active',
    connected_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, connected_account_id)
);

-- The hot read path is "active connections for this user" (list endpoint and
-- MCP session builder both filter on user_id + status).
CREATE INDEX user_composio_connection_user_status_idx
    ON user_composio_connection(user_id, status);

-- Webhook / callback paths look a row up by its Composio connected_account_id.
CREATE INDEX user_composio_connection_account_idx
    ON user_composio_connection(connected_account_id);
