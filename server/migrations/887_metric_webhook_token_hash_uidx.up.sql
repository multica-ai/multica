CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS metric_webhook_token_hash_uidx ON metric_webhook_token (token_hash);
