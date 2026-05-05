-- CEREBRO-PATCH(migration-idempotent-052-project-repo): cerebro modification of upstream file
ALTER TABLE project ADD COLUMN IF NOT EXISTS repo_url TEXT;
