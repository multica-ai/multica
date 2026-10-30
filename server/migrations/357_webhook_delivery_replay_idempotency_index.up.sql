CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_webhook_delivery_replay_idempotency
    ON webhook_delivery(replayed_from_delivery_id, replay_idempotency_key)
    WHERE replayed_from_delivery_id IS NOT NULL AND replay_idempotency_key IS NOT NULL;
