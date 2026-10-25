CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_rework_dispatch_receipt_scope_key_uidx
    ON issue_rework_dispatch_receipt (workspace_id, issue_id, operation_key);
