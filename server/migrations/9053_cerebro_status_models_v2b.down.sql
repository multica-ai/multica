-- Down migration for cerebro workflow v2b (FIR-1550).

DROP INDEX IF EXISTS idx_cerebro_issue_status_base;
DROP INDEX IF EXISTS idx_cerebro_issue_status_workspace_model;
DROP TABLE IF EXISTS cerebro_issue_status;
