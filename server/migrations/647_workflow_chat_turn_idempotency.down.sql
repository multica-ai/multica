ALTER TABLE workflow_chat_turn
  DROP COLUMN IF EXISTS response_body,
  DROP COLUMN IF EXISTS request_hash,
  DROP COLUMN IF EXISTS idempotency_key;
