CREATE TABLE agent_runtime_binding (
  agent_id uuid NOT NULL,
  user_id uuid NOT NULL,
  runtime_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (agent_id, user_id)
);
