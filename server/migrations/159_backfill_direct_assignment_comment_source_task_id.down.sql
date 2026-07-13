-- The forward migration only fills NULL source_task_id values.
-- Down is intentionally a no-op for the same reason as migration 158:
-- the UPDATE is idempotent and the backfill cannot be reversed
-- without an audit table. See migration 158 notes for the manual
-- soft-rollback SQL (NULL out rows where workspace guard would
-- have rejected — not applicable here because the join is
-- issue_id-scoped and workspace-bounded by construction).
SELECT 1;