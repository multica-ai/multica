ALTER TABLE workflow_work_item
  ADD COLUMN IF NOT EXISTS activation_id uuid;

CREATE TABLE workflow_scope_instance (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  run_id uuid NOT NULL,
  definition_scope_id text NOT NULL,
  parent_scope_id uuid,
  generation integer NOT NULL DEFAULT 1,
  rework_count integer NOT NULL DEFAULT 0,
  state text NOT NULL DEFAULT 'active',
  selected_branch text,
  branch_states jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);

CREATE TABLE workflow_node_activation (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  run_id uuid NOT NULL,
  node_id text NOT NULL,
  scope_instance_id uuid NOT NULL,
  generation integer NOT NULL DEFAULT 1,
  activation_no integer NOT NULL DEFAULT 1,
  automatic_retries_used integer NOT NULL DEFAULT 0,
  status text NOT NULL,
  reason_code text NOT NULL DEFAULT '',
  issue_id uuid,
  effective_output_id uuid,
  handled_by_activation_id uuid,
  assignee_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  input_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  override jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);

CREATE TABLE workflow_node_attempt (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  activation_id uuid NOT NULL,
  attempt_no integer NOT NULL,
  task_id uuid,
  retry_of_attempt_id uuid,
  status text NOT NULL,
  error_class text NOT NULL DEFAULT '',
  error_detail text NOT NULL DEFAULT '',
  started_at timestamptz,
  finished_at timestamptz,
  effect_state jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE workflow_output (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  activation_id uuid NOT NULL,
  schema_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  "values" jsonb NOT NULL DEFAULT '{}'::jsonb,
  artifact_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
  content_hash text NOT NULL DEFAULT '',
  superseded_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE workflow_transition (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  run_id uuid NOT NULL,
  source_activation_id uuid NOT NULL,
  outlet text NOT NULL,
  target_scope_id uuid NOT NULL,
  generation integer NOT NULL DEFAULT 1,
  branch_id text NOT NULL DEFAULT '',
  payload_ref jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
