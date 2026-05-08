-- Persisted webhook delivery attempts (RFC #1964).
--
-- Each delivery survives a process restart. The dispatch worker claims
-- pending rows via the partial index below and runs them through the
-- HMAC-signed POST + retry ladder defined by the subscription. Done so
-- that "drop oldest on overflow" + "at-least-once retries" can both
-- hold simultaneously, per Bohan-J's review on issue #1964.
--
-- event_id is minted at events.Bus.Publish time (UUIDv7) and is shared
-- across every subscription's delivery for the same source event — that
-- makes cross-subscription dedup work and gives the realtime + webhook
-- layers a stable common id.
--
-- last_response_body_truncated stores up to ~4 KB. The dispatcher MUST
-- truncate before insert; the column has no length constraint here so a
-- broken dispatcher can't poison the table with massive bodies.

CREATE TABLE IF NOT EXISTS webhook_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id UUID NOT NULL REFERENCES webhook_subscription(id) ON DELETE CASCADE,
    event_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    attempt INT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'succeeded', 'failed', 'dead')),
    next_attempt_at TIMESTAMPTZ,
    last_response_status INT,
    last_response_body_truncated TEXT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

-- Hot path for the dispatch worker: scan only currently-pending rows
-- ordered by next_attempt_at. Partial index keeps it small even when
-- the table grows large with terminal-state history.
CREATE INDEX IF NOT EXISTS idx_webhook_delivery_dispatch
    ON webhook_delivery (next_attempt_at)
    WHERE status = 'pending';

-- Listing path for `multica webhook deliveries <id>`: most-recent-first
-- per subscription.
CREATE INDEX IF NOT EXISTS idx_webhook_delivery_subscription_recent
    ON webhook_delivery (subscription_id, created_at DESC);

-- Cross-subscription dedup / debugging: find every delivery for a given
-- source event_id quickly.
CREATE INDEX IF NOT EXISTS idx_webhook_delivery_event
    ON webhook_delivery (event_id);
