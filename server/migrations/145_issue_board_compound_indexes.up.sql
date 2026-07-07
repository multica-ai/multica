-- Not CONCURRENTLY: pgx runs multi-statement migration files inside an
-- implicit transaction, which rejects CONCURRENTLY. Plain CREATE INDEX
-- briefly locks writes on issue, acceptable at current table sizes.
CREATE INDEX IF NOT EXISTS idx_issue_workspace_project_status_pos ON issue(workspace_id, project_id, status, position);
CREATE INDEX IF NOT EXISTS idx_issue_workspace_project_created ON issue(workspace_id, project_id, created_at DESC);
