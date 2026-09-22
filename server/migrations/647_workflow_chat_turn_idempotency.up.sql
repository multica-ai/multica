ALTER TABLE workflow_chat_turn
  ADD COLUMN idempotency_key text,
  ADD COLUMN request_hash text,
  ADD COLUMN response_body jsonb;
