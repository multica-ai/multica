-- CEREBRO-PATCH(migration-idempotent-049-work-session): cerebro modification of upstream file
CREATE TABLE IF NOT EXISTS work_session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    session_type TEXT NOT NULL DEFAULT 'claude-code',
    session_id TEXT,
    work_dir TEXT,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'completed', 'failed')),
    summary TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_work_session_issue ON work_session(issue_id);
CREATE INDEX IF NOT EXISTS idx_work_session_user ON work_session(user_id);

-- Allow task_message to reference either agent tasks or work sessions.
ALTER TABLE task_message ALTER COLUMN task_id DROP NOT NULL;
ALTER TABLE task_message ADD COLUMN IF NOT EXISTS work_session_id UUID REFERENCES work_session(id) ON DELETE CASCADE;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'task_message_one_parent') THEN
        ALTER TABLE task_message ADD CONSTRAINT task_message_one_parent
            CHECK ((task_id IS NOT NULL) != (work_session_id IS NOT NULL));
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_task_message_work_session ON task_message(work_session_id, seq);
