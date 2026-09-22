ALTER TABLE workflow_run
  ADD COLUMN IF NOT EXISTS active_execution_ms bigint NOT NULL DEFAULT 0;
