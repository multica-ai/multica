CREATE TABLE local_cli_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    cli_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    exit_code INT,
    work_dir TEXT,
    context_dir TEXT,
    comments_mode TEXT NOT NULL DEFAULT 'thread'
        CHECK (comments_mode IN ('thread', 'off')),
    top_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE local_cli_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES local_cli_run(id) ON DELETE CASCADE,
    comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    seq INTEGER NOT NULL,
    type TEXT NOT NULL,
    tool TEXT,
    content TEXT,
    input JSONB,
    output TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_local_cli_message_run_id_seq
    ON local_cli_message(run_id, seq);

CREATE INDEX idx_local_cli_run_issue_created
    ON local_cli_run(issue_id, created_at DESC);
