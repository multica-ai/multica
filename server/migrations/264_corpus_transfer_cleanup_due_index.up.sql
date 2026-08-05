CREATE INDEX CONCURRENTLY corpus_transfer_cleanup_due_idx ON corpus_transfer (cleanup_next_attempt_at, created_at) WHERE cleanup_pending;
