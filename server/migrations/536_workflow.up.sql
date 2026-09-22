CREATE TABLE workflow (
 id uuid NOT NULL, workspace_id uuid NOT NULL, creator_id uuid NOT NULL,
 body jsonb NOT NULL, history jsonb NOT NULL DEFAULT '[]', history_cursor integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE workflow_version (
 workflow_id uuid NOT NULL, revision bigint NOT NULL, body jsonb NOT NULL,
 source_message_id uuid, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE workflow_run (
 id uuid NOT NULL, workflow_id uuid NOT NULL, workspace_id uuid NOT NULL, creator_id uuid NOT NULL,
 idempotency_key text NOT NULL, status text NOT NULL, body jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE workflow_chat (
 workflow_id uuid NOT NULL, user_id uuid NOT NULL, agent_id uuid NOT NULL, session_id uuid NOT NULL
);
CREATE TABLE workflow_chat_turn (
 workflow_id uuid NOT NULL, workspace_id uuid NOT NULL, user_id uuid NOT NULL,
 task_id uuid NOT NULL, base_revision bigint NOT NULL, processed_at timestamptz, error text
);
CREATE TABLE workflow_node_run (
 run_id uuid NOT NULL, node_id text NOT NULL, attempt integer NOT NULL, body jsonb NOT NULL
);
