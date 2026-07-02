CREATE TABLE IF NOT EXISTS daemon_command (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    daemon_id TEXT NOT NULL,
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    requester_user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    command_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'queued',
    claimed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT daemon_command_type_check CHECK (command_type IN ('open_intellij')),
    CONSTRAINT daemon_command_status_check CHECK (status IN ('queued', 'claimed', 'completed', 'failed', 'expired'))
);

CREATE INDEX IF NOT EXISTS idx_daemon_command_claim
    ON daemon_command (daemon_id, status, created_at);

CREATE INDEX IF NOT EXISTS idx_daemon_command_issue
    ON daemon_command (workspace_id, issue_id, created_at DESC);
