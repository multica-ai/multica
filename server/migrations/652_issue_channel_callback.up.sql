-- A chat thread that asked to hear back about an issue. /issue registers one
-- for the chat message that created the issue; later issue events are
-- delivered there through the channel adapter, so agents never hold channel
-- credentials. The columns are adapter-neutral: chat_id / thread_id /
-- anchor_message_id are the platform's own ids. revoked_at stops future sends
-- while keeping the delivery history readable.
--
-- issue_channel_callback_delivery is the ledger: one row per callback and
-- event, keyed by a stable dedup_key so a redelivered event never posts twice.
--
-- No foreign keys (repo rule): issue deletion and workspace teardown remove
-- both tables in application code. A callback whose installation is gone is
-- skipped at send time and swept with its issue.
CREATE TABLE IF NOT EXISTS issue_channel_callback (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    issue_id          UUID NOT NULL,
    installation_id   UUID NOT NULL,
    channel_type      TEXT NOT NULL,
    chat_id           TEXT NOT NULL,
    chat_type         TEXT NOT NULL,
    thread_id         TEXT NOT NULL DEFAULT '',
    anchor_message_id TEXT NOT NULL,
    registered_by     UUID NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at        TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS issue_channel_callback_delivery (
    callback_id         UUID NOT NULL,
    dedup_key           TEXT NOT NULL,
    event               TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('pending', 'delivered', 'failed', 'skipped')),
    provider_message_id TEXT NOT NULL DEFAULT '',
    error               TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (callback_id, dedup_key)
);
