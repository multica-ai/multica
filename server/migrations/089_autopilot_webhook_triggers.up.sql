-- Webhook trigger ingress: enforce uniqueness on webhook tokens so the
-- public ingress route /api/webhooks/autopilots/{token} can resolve a trigger
-- in O(1) without ambiguity. Partial index keeps the constraint scoped to
-- webhook-kind triggers only — schedule/api triggers leave webhook_token NULL
-- and don't participate in the unique key.
CREATE UNIQUE INDEX IF NOT EXISTS idx_autopilot_trigger_webhook_token
ON autopilot_trigger(webhook_token)
WHERE kind = 'webhook' AND webhook_token IS NOT NULL;
