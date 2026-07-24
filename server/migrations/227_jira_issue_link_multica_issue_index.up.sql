-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file. Connection-scoped lookups
-- (including the DeleteJiraConnection cleanup sweep) are already served by
-- the UNIQUE (connection_id, jira_issue_key) constraint index from migration
-- 224; this index covers the reverse direction — finding the Jira link(s)
-- for a Multica issue (webhook sync and DeleteIssue-style cleanup).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_jira_issue_link_multica_issue
    ON jira_issue_link (multica_issue_id);
