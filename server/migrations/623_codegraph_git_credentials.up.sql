CREATE TABLE IF NOT EXISTS code_graph_git_credential (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL,
 name text NOT NULL,
 service_url text NOT NULL,
 username text NOT NULL DEFAULT 'git',
 token_encrypted text NOT NULL,
 created_by uuid,
 updated_by uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE code_graph_repository ADD COLUMN IF NOT EXISTS auth_mode text NOT NULL DEFAULT 'anonymous';
ALTER TABLE code_graph_repository ADD COLUMN IF NOT EXISTS credential_id uuid;
