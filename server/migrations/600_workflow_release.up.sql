CREATE TABLE workflow_release (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  workflow_id uuid NOT NULL,
  version_number integer NOT NULL,
  draft_revision bigint NOT NULL,
  graph_schema_version integer NOT NULL,
  graph jsonb NOT NULL,
  compiled_plan jsonb NOT NULL,
  plan_version integer NOT NULL,
  content_hash text NOT NULL,
  created_by uuid NOT NULL,
  notes text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
