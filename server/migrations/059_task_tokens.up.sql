-- CEREBRO-PATCH(migration-idempotent-059-task-tokens): cerebro modification of upstream file
-- Per-task scoped tokens. Replace the daemon's full PAT with a short-lived,
-- scope-limited token that the agent process is allowed to see. If the agent
-- is compromised (prompt injection, exfiltration), the blast radius is the
-- one task's issue for at most a few hours instead of the daemon owner's
-- full Multica account for 30 days.
CREATE TABLE IF NOT EXISTS task_token (
    token_hash  BYTEA PRIMARY KEY,
    task_id     UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    issue_id    UUID REFERENCES issue(id) ON DELETE CASCADE,
    agent_id    UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    scope       TEXT NOT NULL DEFAULT 'task',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_task_token_task ON task_token(task_id);
CREATE INDEX IF NOT EXISTS idx_task_token_active_expiry ON task_token(expires_at) WHERE revoked_at IS NULL;
