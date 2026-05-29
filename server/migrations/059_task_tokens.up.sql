-- CEREBRO-PATCH(migration-idempotent-059-task-tokens): cerebro modification of upstream file
-- Per-task scoped tokens. Replace the daemon's full PAT with a short-lived,
-- scope-limited token that the agent process is allowed to see. If the agent
-- is compromised (prompt injection, exfiltration), the blast radius is the
-- one task's issue for at most a few hours instead of the daemon owner's
-- full Multica account for 30 days.
--
-- CEREBRO-PATCH(migration-idempotent-059-task-tokens): guard the whole body on
-- table absence. Migration 108 (upstream MUL-2600) later DROPs this table and
-- recreates it WITHOUT the revoked_at column, so a second idempotency pass that
-- re-runs 059 must not touch task_token at all — the trailing partial index
-- references revoked_at and would raise "column revoked_at does not exist"
-- against the 108 schema. Skipping when the table already exists keeps both the
-- fresh-install path (059 creates, 108 upgrades) and the re-run path safe.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'task_token'
    ) THEN
        CREATE TABLE task_token (
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
        CREATE INDEX idx_task_token_task ON task_token(task_id);
        CREATE INDEX idx_task_token_active_expiry ON task_token(expires_at) WHERE revoked_at IS NULL;
    END IF;
END $$;
