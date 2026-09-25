-- Display names must stay unambiguous among ACTIVE statuses; archived rows are
-- excluded so an archived name can be reused.
CREATE UNIQUE INDEX CONCURRENTLY idx_issue_status_workspace_name_active
    ON issue_status (workspace_id, lower(name))
    WHERE archived_at IS NULL;
