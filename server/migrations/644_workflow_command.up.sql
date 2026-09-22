CREATE TABLE workflow_command (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  actor_id uuid NOT NULL,
  operation text NOT NULL,
  resource_id uuid NOT NULL,
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  status_code integer NOT NULL DEFAULT 0,
  response_body jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
