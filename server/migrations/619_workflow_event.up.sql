CREATE TABLE workflow_event (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  run_id uuid NOT NULL,
  sequence bigint NOT NULL,
  event_type text NOT NULL,
  actor_type text NOT NULL,
  actor_id text,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
