CREATE TABLE agent_runtime_binding (
  agent_id uuid NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  runtime_id uuid NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (agent_id, user_id)
);

CREATE INDEX idx_agent_runtime_binding_runtime ON agent_runtime_binding(runtime_id);
