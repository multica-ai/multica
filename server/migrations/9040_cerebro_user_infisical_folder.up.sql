-- Admin-defined allow-list of Infisical folders a user may grant to their agents.
-- Agent folder grants (cerebro_agent_infisical_folder) are gated against the
-- agent owner's allow-list: a folder can only be saved on an agent, and only
-- injected at spawn, if it appears here for the agent's owner. This lets an
-- admin control which keys a given user is even allowed to hand to their agents.

CREATE TABLE IF NOT EXISTS cerebro_user_infisical_folder (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    environment text NOT NULL,
    secret_path text NOT NULL DEFAULT '/',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_user_infisical_folder_environment_not_empty CHECK (btrim(environment) <> ''),
    CONSTRAINT cerebro_user_infisical_folder_secret_path_not_empty CHECK (btrim(secret_path) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS cerebro_user_infisical_folder_ws_user_env_path_idx
    ON cerebro_user_infisical_folder (workspace_id, user_id, environment, secret_path);

CREATE INDEX IF NOT EXISTS cerebro_user_infisical_folder_ws_user_idx
    ON cerebro_user_infisical_folder (workspace_id, user_id);
