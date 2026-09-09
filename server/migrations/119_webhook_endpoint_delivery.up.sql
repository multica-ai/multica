-- Outbound webhook delivery log: renamed from webhook_delivery (which migration
-- 093 repurposed for inbound autopilot webhook tracking) to avoid table name
-- collision. Recreates the outbound delivery tracking table under a distinct name.

CREATE TABLE IF NOT EXISTS webhook_endpoint_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id UUID NOT NULL REFERENCES webhook_endpoint(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivered', 'failed')),
    http_status INT,
    response_body TEXT,
    error_message TEXT,
    attempt INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_webhook_endpoint_delivery_endpoint ON webhook_endpoint_delivery(endpoint_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_endpoint_delivery_status ON webhook_endpoint_delivery(endpoint_id, status)
    WHERE status = 'pending';
