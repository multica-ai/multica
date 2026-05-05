-- Fork-specific: per-device Web Push subscriptions for browser/PWA notifications.
-- One row per (user_id, device) — endpoint is unique because the browser's
-- PushManager guarantees a stable URL per Service Worker registration. When
-- the browser revokes a subscription (user toggles off, browser cleared, etc),
-- the next push send will return 404/410 and the row is deleted.

CREATE TABLE IF NOT EXISTS push_subscription (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_push_subscription_user
    ON push_subscription(user_id);
