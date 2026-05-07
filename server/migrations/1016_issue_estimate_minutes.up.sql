ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS estimate_minutes INT NULL
        CHECK (estimate_minutes IS NULL OR estimate_minutes > 0);

CREATE INDEX IF NOT EXISTS idx_issue_assignee_open
    ON issue (assignee_type, assignee_id, status)
    WHERE status IN ('todo','in_progress','planning','ready_for_dev','fixing','testing');

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'issue_id_workspace_uniq'
          AND conrelid = 'issue'::regclass
    ) THEN
        ALTER TABLE issue
            ADD CONSTRAINT issue_id_workspace_uniq UNIQUE (id, workspace_id);
    END IF;
END $$;
