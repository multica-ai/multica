CREATE TABLE qoder_connection (
 workspace_id uuid NOT NULL,
 owner_id uuid NOT NULL,
 token_id uuid NOT NULL,
 config_encrypted text NOT NULL,
 enabled boolean NOT NULL DEFAULT true,
 revision bigint NOT NULL DEFAULT 1,
 status text NOT NULL DEFAULT 'starting',
 last_error text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now()
);
