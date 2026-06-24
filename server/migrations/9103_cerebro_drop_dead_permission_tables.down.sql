-- Recreate the dropped tables (schema only; the data was dead).

CREATE TABLE IF NOT EXISTS cerebro_share_token (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    token             TEXT NOT NULL UNIQUE,
    resource_kind     TEXT NOT NULL,
    resource_id       UUID NOT NULL,
    action            TEXT NOT NULL DEFAULT 'read',
    persona_grant_id  TEXT NOT NULL DEFAULT '',
    created_by_id     UUID NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at        TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_cerebro_share_token_workspace
    ON cerebro_share_token (workspace_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_share_token_resource
    ON cerebro_share_token (workspace_id, resource_kind, resource_id);

CREATE TABLE IF NOT EXISTS cerebro_agent_infisical_secret (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id uuid NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    env_var_name text NOT NULL,
    secret_name text NOT NULL,
    environment text NOT NULL,
    secret_path text NOT NULL DEFAULT '/',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_agent_infisical_secret_env_var_name_not_empty CHECK (btrim(env_var_name) <> ''),
    CONSTRAINT cerebro_agent_infisical_secret_secret_name_not_empty CHECK (btrim(secret_name) <> ''),
    CONSTRAINT cerebro_agent_infisical_secret_environment_not_empty CHECK (btrim(environment) <> ''),
    CONSTRAINT cerebro_agent_infisical_secret_secret_path_not_empty CHECK (btrim(secret_path) <> '')
);
CREATE UNIQUE INDEX IF NOT EXISTS cerebro_agent_infisical_secret_agent_env_var_idx
    ON cerebro_agent_infisical_secret (agent_id, upper(env_var_name));
CREATE INDEX IF NOT EXISTS cerebro_agent_infisical_secret_agent_id_idx
    ON cerebro_agent_infisical_secret (agent_id);
