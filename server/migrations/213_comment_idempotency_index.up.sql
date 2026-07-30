CREATE UNIQUE INDEX CONCURRENTLY comment_author_idempotency_key_idx
  ON comment (workspace_id, author_type, author_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;
