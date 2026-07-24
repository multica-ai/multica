ALTER TABLE comment
  DROP CONSTRAINT IF EXISTS comment_idempotency_key_length_check,
  DROP CONSTRAINT IF EXISTS comment_idempotency_pair_check,
  DROP COLUMN IF EXISTS idempotency_hash,
  DROP COLUMN IF EXISTS idempotency_key;
