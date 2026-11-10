CREATE UNIQUE INDEX CONCURRENTLY issue_blocker_unique ON issue_blocker(workspace_id,issue_id,blocker_issue_id);
