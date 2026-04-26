-- 1001_coderabbit_integration.down.sql

DROP INDEX IF EXISTS idx_github_webhook_delivery_received;
DROP TABLE IF EXISTS github_webhook_delivery;

DROP INDEX IF EXISTS idx_issue_pr_lookup;

ALTER TABLE issue
    DROP COLUMN IF EXISTS pr_repo,
    DROP COLUMN IF EXISTS pr_number,
    DROP COLUMN IF EXISTS pr_url;

DROP INDEX IF EXISTS idx_workspace_repo_binding_workspace;
DROP TABLE IF EXISTS workspace_repo_binding;
