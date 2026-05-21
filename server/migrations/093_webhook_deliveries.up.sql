-- Webhook delivery layer: separates "we received an inbound HTTP webhook" from
-- "we created an autopilot run". Splitting the two lets us record duplicate
-- attempts, signature outcomes, ignored/skipped deliveries, and offers a
-- replay path — all of which the previous shape (writing straight into
-- autopilot_run.trigger_payload) could not express.
--
-- Scope of this migration:
--   1. Add `provider` + `signing_secret` to autopilot_trigger so a webhook
--      trigger can optionally carry per-provider configuration for signature
--      verification. signing_secret is plaintext at rest, mirroring how
--      webhook_token already lives — HMAC verification needs the cleartext
--      and there is no general secrets-at-rest infrastructure to layer on
--      yet (see issue MUL-2334 for the design rationale).
--   2. Create webhook_delivery, one row per inbound HTTP request the public
--      ingress endpoint accepted (including rejected / ignored outcomes).
--      Duplicate requests don't get their own row — they bump attempt_count
--      on the existing dedupe target. The autopilot_run table is kept intact
--      and continues to represent the downstream execution — both views are
--      needed.

ALTER TABLE autopilot_trigger
    ADD COLUMN provider TEXT NOT NULL DEFAULT 'generic'
        CHECK (provider IN ('generic', 'github')),
    ADD COLUMN signing_secret TEXT;

CREATE TABLE IF NOT EXISTS webhook_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    autopilot_id UUID NOT NULL REFERENCES autopilot(id) ON DELETE CASCADE,
    trigger_id UUID NOT NULL REFERENCES autopilot_trigger(id) ON DELETE CASCADE,
    provider TEXT NOT NULL
        CHECK (provider IN ('generic', 'github')),
    event TEXT NOT NULL DEFAULT 'webhook.received',
    -- dedupe_key is extracted from request headers per-provider:
    --   github  -> X-GitHub-Delivery
    --   generic -> Idempotency-Key
    -- NULL means "no provider-supplied dedupe identifier"; the partial unique
    -- index below skips these rows so each NULL-keyed delivery is treated as
    -- distinct (correct behavior for clients that don't implement idempotency
    -- headers — the alternative would collapse all of them onto one row).
    dedupe_key TEXT,
    dedupe_source TEXT,
    signature_status TEXT NOT NULL DEFAULT 'not_required'
        CHECK (signature_status IN ('not_required', 'valid', 'invalid', 'missing')),
    -- Delivery status tracks the *ingress*, not the autopilot run:
    --   queued     — INSERTed, dispatch not yet attempted
    --   dispatched — handed off to AutopilotService; autopilot_run_id is set.
    --                A run that was admission-skipped (e.g. runtime offline)
    --                still lives here — the skipped-ness is recorded on
    --                autopilot_run.status, not on the delivery, so the
    --                Deliveries enum stays unambiguous.
    --   rejected   — signature verification failed (invalid or missing)
    --   ignored    — trigger disabled / autopilot paused / archived
    --   failed     — dispatch attempted and errored
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'dispatched', 'rejected', 'ignored', 'failed')),
    attempt_count INTEGER NOT NULL DEFAULT 1,
    -- Selected headers we want to keep for debugging (user-agent, event,
    -- delivery id, idempotency key, signature presence). NOT the raw header
    -- map — never store auth tokens or signature values plaintext.
    selected_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    content_type TEXT,
    -- raw_body holds the exact bytes we received, capped at the ingress body
    -- limit (256 KiB). Required for replay and to debug normalization issues.
    -- No TTL/retention enforced here; if delivery volume becomes a storage
    -- concern, add a follow-up migration with a retention job rather than
    -- baking the policy into the schema.
    raw_body BYTEA,
    response_status INTEGER,
    response_body TEXT,
    autopilot_run_id UUID REFERENCES autopilot_run(id) ON DELETE SET NULL,
    replayed_from_delivery_id UUID REFERENCES webhook_delivery(id) ON DELETE SET NULL,
    error TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Listing deliveries per autopilot, newest first.
CREATE INDEX IF NOT EXISTS idx_webhook_delivery_autopilot
    ON webhook_delivery(autopilot_id, created_at DESC);

-- Provider-supplied dedupe identifiers must be unique per trigger. Partial so
-- that NULL keys (no Idempotency-Key / X-GitHub-Delivery header) never
-- collide. Terminal-but-not-successful outcomes (`rejected`, `failed`) are
-- excluded so a transient ingress failure or a misconfigured secret does
-- not strand the operator: providers like GitHub keep X-GitHub-Delivery
-- stable across retries, and a permanently-blocking row would prevent the
-- next retry from ever being dispatched.
CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_delivery_dedupe
    ON webhook_delivery(trigger_id, dedupe_key)
    WHERE dedupe_key IS NOT NULL AND status NOT IN ('rejected', 'failed');

-- Lookup by linked run (sync flows, gc check, run-detail "what triggered me").
CREATE INDEX IF NOT EXISTS idx_webhook_delivery_run
    ON webhook_delivery(autopilot_run_id)
    WHERE autopilot_run_id IS NOT NULL;
