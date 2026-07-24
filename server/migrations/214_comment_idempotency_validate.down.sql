ALTER TABLE comment
  DROP CONSTRAINT IF EXISTS comment_idempotency_key_length_check,
  DROP CONSTRAINT IF EXISTS comment_idempotency_pair_check;

ALTER TABLE comment
  ADD CONSTRAINT comment_idempotency_pair_check CHECK (
    (idempotency_key IS NULL) = (idempotency_hash IS NULL)
  ) NOT VALID,
  ADD CONSTRAINT comment_idempotency_key_length_check CHECK (
    idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 255
  ) NOT VALID;
