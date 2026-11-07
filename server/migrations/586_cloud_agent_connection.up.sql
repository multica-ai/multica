-- One managed cloud-agent provider connection per workspace and provider.
-- Secrets and the run journal are sealed with MULTICA_VCS_SECRET_KEY.
CREATE TABLE cloud_agent_connection (
 workspace_id uuid NOT NULL,
 provider text NOT NULL,
 owner_id uuid NOT NULL,
 token_id uuid NOT NULL,
 config_encrypted text NOT NULL,
 journal_encrypted text NOT NULL DEFAULT '',
 enabled boolean NOT NULL DEFAULT true,
 revision bigint NOT NULL DEFAULT 1,
 status text NOT NULL DEFAULT 'starting',
 last_error text NOT NULL DEFAULT '',
 busy_until timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now()
);
