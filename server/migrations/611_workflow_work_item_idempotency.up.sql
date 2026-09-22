ALTER TABLE workflow_work_item
  ADD COLUMN IF NOT EXISTS idempotency_key text NOT NULL DEFAULT '';
