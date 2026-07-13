-- The forward migration only fills NULL source_task_id values. Rolling back
-- would require distinguishing rows that were NULL before the migration from
-- rows that this migration filled, which is not recoverable without an audit
-- table. Down is intentionally a no-op; restore by re-running the up file
-- after a failed forward deployment (the UPDATE is idempotent).
SELECT 1;