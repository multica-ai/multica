DROP TABLE IF EXISTS cerebro_agent_infisical_folder;

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
