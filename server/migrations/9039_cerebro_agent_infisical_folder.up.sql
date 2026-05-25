-- Replace per-secret references with per-agent Infisical folder mappings.
-- An agent is granted whole folders (environment + path); at spawn the daemon
-- lists every secret in the folder and injects each one under its own
-- Infisical key name, so the agent never sees the value in its transcript and
-- the user never maps secrets line by line.

CREATE TABLE IF NOT EXISTS cerebro_agent_infisical_folder (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id uuid NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    environment text NOT NULL,
    secret_path text NOT NULL DEFAULT '/',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_agent_infisical_folder_environment_not_empty CHECK (btrim(environment) <> ''),
    CONSTRAINT cerebro_agent_infisical_folder_secret_path_not_empty CHECK (btrim(secret_path) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS cerebro_agent_infisical_folder_agent_env_path_idx
    ON cerebro_agent_infisical_folder (agent_id, environment, secret_path);

CREATE INDEX IF NOT EXISTS cerebro_agent_infisical_folder_agent_id_idx
    ON cerebro_agent_infisical_folder (agent_id);

DROP TABLE IF EXISTS cerebro_agent_infisical_secret;
