ALTER TABLE workflow_work_item
  ADD COLUMN IF NOT EXISTS idempotency_hash text NOT NULL DEFAULT '';
