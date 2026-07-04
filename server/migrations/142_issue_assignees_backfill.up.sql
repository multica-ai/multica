CREATE TABLE IF NOT EXISTS issue_assignees (
  issue_id      UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
  assignee_type TEXT NOT NULL CHECK (assignee_type IN ('member', 'agent')),
  assignee_id   UUID NOT NULL,
  role          TEXT NOT NULL DEFAULT 'assignee' CHECK (role IN ('assignee', 'reviewer', 'observer')),
  assigned_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (issue_id, assignee_type, assignee_id)
);

-- Backfill data from issue table to issue_assignees table
INSERT INTO issue_assignees (issue_id, assignee_type, assignee_id, role, assigned_at)
SELECT id, assignee_type, assignee_id, 'assignee', updated_at
FROM issue
WHERE assignee_id IS NOT NULL 
  AND assignee_type IN ('member', 'agent')
ON CONFLICT (issue_id, assignee_type, assignee_id) DO NOTHING;
