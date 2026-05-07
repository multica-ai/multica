-- CEREBRO-PATCH(migration-idempotent-020-issue-number): cerebro modification of upstream file
-- Add issue_prefix and issue_counter to workspace for human-readable issue IDs.
ALTER TABLE workspace
    ADD COLUMN IF NOT EXISTS issue_prefix TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS issue_counter INT NOT NULL DEFAULT 0;

-- Add per-workspace issue number.
ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS number INT NOT NULL DEFAULT 0;

-- Backfill: generate issue_prefix from workspace name (first 3 uppercase chars).
UPDATE workspace SET issue_prefix = UPPER(
    LEFT(REGEXP_REPLACE(name, '[^a-zA-Z]', '', 'g'), 3)
)
WHERE issue_prefix = '';

-- Fallback for workspaces with empty prefix after cleanup.
UPDATE workspace SET issue_prefix = 'WS' WHERE issue_prefix = '';

-- Backfill: assign sequential numbers to existing issues per workspace.
-- Only fills issues that haven't been numbered yet (number = 0).
WITH numbered AS (
    SELECT id, workspace_id,
           ROW_NUMBER() OVER (PARTITION BY workspace_id ORDER BY created_at ASC) AS rn
    FROM issue
    WHERE number = 0
)
UPDATE issue SET number = numbered.rn
FROM numbered WHERE issue.id = numbered.id;

-- Update workspace counters to match (only if counter is behind actual max).
UPDATE workspace SET issue_counter = COALESCE(
    (SELECT MAX(number) FROM issue WHERE issue.workspace_id = workspace.id), 0
)
WHERE issue_counter < COALESCE(
    (SELECT MAX(number) FROM issue WHERE issue.workspace_id = workspace.id), 0
);

-- Add unique constraint.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uq_issue_workspace_number') THEN
        ALTER TABLE issue ADD CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number);
    END IF;
END $$;

-- Index for fast lookup by workspace + number.
CREATE INDEX IF NOT EXISTS idx_issue_workspace_number ON issue(workspace_id, number);
