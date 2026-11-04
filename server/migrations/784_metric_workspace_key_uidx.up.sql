-- One metric per key in a workspace. Built concurrently; the unique index also
-- serves the upsert that seeds the built-in metrics.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS metric_workspace_key_uidx ON metric (workspace_id, key);
