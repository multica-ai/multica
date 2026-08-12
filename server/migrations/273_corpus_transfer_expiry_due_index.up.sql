CREATE INDEX CONCURRENTLY corpus_transfer_expiry_due_idx ON corpus_transfer (expires_at, created_at) WHERE state IN ('created', 'uploading', 'uploaded', 'verifying', 'confirmed', 'acked');
