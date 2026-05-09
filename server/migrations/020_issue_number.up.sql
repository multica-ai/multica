-- Add issue_prefix and issue_counter to workspace for human-readable issue IDs.
ALTER TABLE workspace
    ADD COLUMN issue_prefix TEXT NOT NULL DEFAULT '',
    ADD COLUMN issue_counter INT NOT NULL DEFAULT 0;

-- Add per-workspace issue number.
ALTER TABLE issue
    ADD COLUMN number INT NOT NULL DEFAULT 0;

-- Backfill: generate issue_prefix from workspace name (first 3 uppercase chars).
UPDATE workspace SET issue_prefix = UPPER(
    LEFT(REGEXP_REPLACE(name, '[^a-zA-Z]', '', 'g'), 3)
);

-- Fallback for workspaces with empty prefix after cleanup.
UPDATE workspace SET issue_prefix = 'WS' WHERE issue_prefix = '';

-- Backfill: assign sequential numbers to existing issues per workspace.
WITH numbered AS (
    SELECT id, workspace_id,
           ROW_NUMBER() OVER (PARTITION BY workspace_id ORDER BY created_at ASC) AS rn
    FROM issue
)
UPDATE issue SET number = numbered.rn
FROM numbered WHERE issue.id = numbered.id;

-- Update workspace counters to match.
UPDATE workspace SET issue_counter = COALESCE(
    (SELECT MAX(number) FROM issue WHERE issue.workspace_id = workspace.id), 0
);

-- Add unique constraint.
ALTER TABLE issue ADD CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number);

-- Index for fast lookup by workspace + number.
CREATE INDEX idx_issue_workspace_number ON issue(workspace_id, number);
