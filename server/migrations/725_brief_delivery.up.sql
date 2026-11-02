-- One row per (brief, member) for a brief pushed to a chat app (spec 2.7, plan
-- 9.10). Written by the brief:created subscriber as pending; the retry job
-- sends it, defers it through quiet hours, or reschedules it with backoff.
--
-- state: pending (waiting for its next attempt), deferred (inside the member's
-- quiet hours; next_attempt_at is the window's end), sent, failed (permanent,
-- or out of attempts). attempts counts sends tried. last_error is the last
-- failure, with no credentials in it.
--
-- No foreign keys. Delivery failure never changes the brief. The primary key
-- is built concurrently in 726 and attached in 727; the (brief_id, member_id)
-- key that doubles as the brief lookup is 728, and the retry scan is 729.
CREATE TABLE IF NOT EXISTS brief_delivery (
    id              UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    brief_id        UUID NOT NULL,
    member_id       UUID NOT NULL,
    binding_id      UUID NOT NULL,
    state           TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'sent', 'failed', 'deferred')),
    attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 1000),
    delivered_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
