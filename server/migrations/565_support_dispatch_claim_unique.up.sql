CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS support_dispatch_claim_issue_unique
    ON support_dispatch_claim (workspace_id, issue_id);
