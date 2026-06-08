-- Custom issue statuses per workspace.
-- Each status belongs to a category so business-logic queries
-- ("is this issue done?") keep working without hardcoding names.

CREATE TABLE workspace_issue_status (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,           -- slug used in API / DB (e.g. "human_review")
    label       TEXT NOT NULL,           -- display label (e.g. "Human Review")
    color       TEXT NOT NULL DEFAULT '#6b7280',  -- hex color for UI
    category    TEXT NOT NULL DEFAULT 'started'
        CHECK (category IN ('not_started', 'started', 'completed', 'cancelled')),
    position    INT NOT NULL DEFAULT 0,  -- ordering within the workspace
    is_default  BOOLEAN NOT NULL DEFAULT false, -- is this a built-in status?
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE INDEX idx_workspace_issue_status_ws ON workspace_issue_status(workspace_id, position);

-- Seed the 7 built-in statuses for every existing workspace.
INSERT INTO workspace_issue_status (workspace_id, name, label, color, category, position, is_default)
SELECT w.id, s.name, s.label, s.color, s.category, s.position, true
FROM workspace w
CROSS JOIN (VALUES
    ('backlog',     'Backlog',      '#6b7280', 'not_started', 0),
    ('todo',        'Todo',         '#6b7280', 'not_started', 1),
    ('in_progress', 'In Progress',  '#f59e0b', 'started',     2),
    ('in_review',   'In Review',    '#8b5cf6', 'started',     3),
    ('done',        'Done',         '#22c55e', 'completed',   4),
    ('blocked',     'Blocked',      '#ef4444', 'started',     5),
    ('cancelled',   'Cancelled',    '#6b7280', 'cancelled',   6)
) AS s(name, label, color, category, position);

-- Drop the hardcoded CHECK constraint on issue.status.
-- Status validation is now done at the application layer against workspace_issue_status.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;

-- Fallback: the constraint might have been created with an auto-generated name.
-- Try the common pattern too.
DO $$
BEGIN
    -- Find and drop any CHECK constraint on issue.status
    PERFORM 1
    FROM information_schema.constraint_column_usage
    WHERE table_name = 'issue' AND column_name = 'status'
      AND constraint_name != 'issue_pkey';
    IF FOUND THEN
        EXECUTE (
            SELECT 'ALTER TABLE issue DROP CONSTRAINT ' || quote_ident(constraint_name)
            FROM information_schema.constraint_column_usage
            WHERE table_name = 'issue' AND column_name = 'status'
              AND constraint_name != 'issue_pkey'
            LIMIT 1
        );
    END IF;
END $$;
