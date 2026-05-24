-- Rollback for 9037_cerebro_approvals.

DROP INDEX IF EXISTS idx_cerebro_approval_audit_workspace;
DROP INDEX IF EXISTS idx_cerebro_approval_audit_approval;
DROP TABLE IF EXISTS cerebro_approval_audit;

DROP INDEX IF EXISTS idx_cerebro_approval_request_pending_expiry;
DROP INDEX IF EXISTS idx_cerebro_approval_request_workspace_created;
DROP INDEX IF EXISTS idx_cerebro_approval_request_workspace_status;
DROP TABLE IF EXISTS cerebro_approval_request;
