ALTER TABLE comment
  ADD COLUMN idempotency_key TEXT,
  ADD COLUMN idempotency_hash BYTEA;

ALTER TABLE comment
  ADD CONSTRAINT comment_idempotency_pair_check CHECK (
    (idempotency_key IS NULL) = (idempotency_hash IS NULL)
  ) NOT VALID,
  ADD CONSTRAINT comment_idempotency_key_length_check CHECK (
    idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 255
  ) NOT VALID;

CREATE UNIQUE INDEX CONCURRENTLY comment_author_idempotency_key_idx
  ON comment (workspace_id, author_type, author_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

ALTER TABLE comment VALIDATE CONSTRAINT comment_idempotency_pair_check;
ALTER TABLE comment VALIDATE CONSTRAINT comment_idempotency_key_length_check;
