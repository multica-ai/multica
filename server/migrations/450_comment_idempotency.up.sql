-- Idempotent comment retries (Reply Admission hardening).
--
-- The key is scoped to a workspace so a lost HTTP response can be retried
-- without creating a second comment. The request hash prevents a caller from
-- reusing a key for different content. The comment foreign key makes the
-- record disappear with the comment, rather than retaining a stale replay.
CREATE TABLE comment_idempotency (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    comment_id UUID NOT NULL REFERENCES comment(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, idempotency_key),
    CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE INDEX idx_comment_idempotency_comment
    ON comment_idempotency (comment_id);
