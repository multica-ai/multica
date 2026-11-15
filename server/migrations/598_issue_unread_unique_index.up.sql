CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_unread_workspace_user_issue_uidx ON issue_unread (workspace_id, user_id, issue_id);
