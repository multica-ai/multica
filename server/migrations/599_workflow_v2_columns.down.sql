ALTER TABLE workflow_run
  DROP COLUMN IF EXISTS request_hash,
  DROP COLUMN IF EXISTS reason_code,
  DROP COLUMN IF EXISTS finished_at,
  DROP COLUMN IF EXISTS deadline_at,
  DROP COLUMN IF EXISTS state_revision,
  DROP COLUMN IF EXISTS owner_id,
  DROP COLUMN IF EXISTS input_values,
  DROP COLUMN IF EXISTS mode,
  DROP COLUMN IF EXISTS release_id,
  DROP COLUMN IF EXISTS engine_version;

ALTER TABLE workflow
  DROP COLUMN IF EXISTS archived_at,
  DROP COLUMN IF EXISTS published_release_id,
  DROP COLUMN IF EXISTS graph_schema_version,
  DROP COLUMN IF EXISTS draft_revision,
  DROP COLUMN IF EXISTS owner_id;
