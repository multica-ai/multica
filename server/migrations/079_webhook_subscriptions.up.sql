-- Outbound webhook subscriptions (RFC #1964).
--
-- Subscribers register an HTTPS URL + a per-subscription secret. The
-- dispatcher fans out events from `events.Bus` (workspace-scoped) into
-- HMAC-SHA256-signed POST requests against the URL. The raw secret is
-- stored (the dispatcher needs it to compute HMAC) but never returned by
-- the API after create/rotate — mirroring the agent custom_env redaction
-- pattern via a `secret_redacted: true` flag in the response shape.
--
-- Admin-only via membership middleware (mirrors autopilot semantics).

CREATE TABLE IF NOT EXISTS webhook_subscription (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    secret TEXT NOT NULL,
    -- Exact bus event-type strings (e.g. 'issue:updated', 'task:completed').
    -- The literal '*' opts in to all current AND future events; subscribers
    -- using '*' have an event_taxonomy_pinned_at timestamp recorded for
    -- auditability when wider events get added later.
    event_filter TEXT[] NOT NULL,
    state TEXT NOT NULL DEFAULT 'active'
        CHECK (state IN ('active', 'paused', 'auto_paused', 'disabled')),
    pause_threshold INT NOT NULL DEFAULT 5
        CHECK (pause_threshold >= 1 AND pause_threshold <= 100),
    consecutive_failures INT NOT NULL DEFAULT 0,
    allow_http BOOLEAN NOT NULL DEFAULT FALSE,
    per_attempt_timeout_seconds INT NOT NULL DEFAULT 10
        CHECK (per_attempt_timeout_seconds >= 1 AND per_attempt_timeout_seconds <= 30),
    event_taxonomy_pinned_at TIMESTAMPTZ,
    created_by UUID NOT NULL REFERENCES member(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_webhook_subscription_workspace
    ON webhook_subscription (workspace_id, state);
