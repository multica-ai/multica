ALTER TABLE workflow
  ADD COLUMN IF NOT EXISTS owner_id uuid,
  ADD COLUMN IF NOT EXISTS draft_revision bigint NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS graph_schema_version integer NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS published_release_id uuid,
  ADD COLUMN IF NOT EXISTS archived_at timestamptz;

ALTER TABLE workflow_run
  ADD COLUMN IF NOT EXISTS engine_version integer NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS release_id uuid,
  ADD COLUMN IF NOT EXISTS mode text NOT NULL DEFAULT 'production',
  ADD COLUMN IF NOT EXISTS input_values jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS owner_id uuid,
  ADD COLUMN IF NOT EXISTS state_revision bigint NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS deadline_at timestamptz,
  ADD COLUMN IF NOT EXISTS finished_at timestamptz,
  ADD COLUMN IF NOT EXISTS reason_code text,
  ADD COLUMN IF NOT EXISTS request_hash text;
