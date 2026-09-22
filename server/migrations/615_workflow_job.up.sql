CREATE TABLE workflow_job (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  run_id uuid NOT NULL,
  kind text NOT NULL,
  logical_key text NOT NULL,
  due_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  lease_owner text,
  lease_until timestamptz,
  fence bigint NOT NULL DEFAULT 0,
  payload_ref jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
