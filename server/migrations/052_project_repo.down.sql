-- CEREBRO-PATCH(migration-idempotent-052-project-repo): cerebro modification of upstream file
ALTER TABLE project DROP COLUMN IF EXISTS repo_url;
