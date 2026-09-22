ALTER TABLE workflow_release ADD COLUMN IF NOT EXISTS idempotency_key text NOT NULL DEFAULT '';
