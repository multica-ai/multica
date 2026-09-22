CREATE TABLE workflow_outbox (
  id uuid NOT NULL,
  workspace_id uuid NOT NULL,
  event_id uuid NOT NULL,
  channel text NOT NULL,
  recipient_id text NOT NULL,
  due_at timestamptz NOT NULL DEFAULT now(),
  delivered_at timestamptz,
  delivery_attempt integer NOT NULL DEFAULT 0,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
