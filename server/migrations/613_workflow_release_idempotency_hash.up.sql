ALTER TABLE workflow_release
  ADD COLUMN IF NOT EXISTS idempotency_hash text NOT NULL DEFAULT '';
