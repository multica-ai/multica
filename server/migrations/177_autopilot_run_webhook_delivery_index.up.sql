CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_autopilot_run_webhook_delivery
    ON autopilot_run(webhook_delivery_id)
    WHERE webhook_delivery_id IS NOT NULL;
