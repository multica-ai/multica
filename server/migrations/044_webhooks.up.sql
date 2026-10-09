-- Outbound webhook endpoints: workspace-level webhook configuration for
-- pushing action_required notifications to external services (Slack, email, etc).

CREATE TABLE IF NOT EXISTS webhook_endpoint (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    secret TEXT NOT NULL,
    description TEXT,
    event_types TEXT[] NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_webhook_endpoint_workspace ON webhook_endpoint(workspace_id);
CREATE INDEX IF NOT EXISTS idx_webhook_endpoint_enabled ON webhook_endpoint(workspace_id)
    WHERE enabled = true;

-- Delivery log: tracks each outbound webhook attempt for debugging and audit.
CREATE TABLE IF NOT EXISTS webhook_delivery (
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

CREATE INDEX IF NOT EXISTS idx_webhook_delivery_endpoint ON webhook_delivery(endpoint_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_delivery_status ON webhook_delivery(endpoint_id, status)
    WHERE status = 'pending';
