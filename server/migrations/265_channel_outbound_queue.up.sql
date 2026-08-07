-- Durable outbound delivery queue for hold-connection channels.
--
-- Why a table and not an in-process handoff: a channel that reaches its
-- platform over a long-lived socket can only write from the replica that
-- holds that socket's lease, but the reply to send is produced wherever the
-- agent task happened to run. An in-process bus therefore drops every reply
-- produced on a replica that does not hold the lease. This table is the
-- handoff point: producers INSERT from any replica, and the lease holder
-- claims and drains.
--
-- channel_type is a column rather than a table-name prefix so Lark, Slack,
-- and WeCom share one queue, one claim path, and one set of retention
-- sweeps. Nothing here knows a platform's wire format; payload is an opaque
-- JSONB document the owning adapter renders at send time.
--
-- No foreign keys (repo rule): lifecycle cleanup is application-owned via
-- the Fail*/Delete* queries in pkg/db/queries/channel_outbound_queue.sql and
-- the channel installation teardown paths.
--
-- status is deliberately a 3-value set. There is no 'sending' state: a row
-- being worked on is still 'queued' and is fenced by lease_token +
-- lease_expires_at instead, so a worker that dies mid-send leaves a row that
-- another worker reclaims once the lease expires rather than one stranded in
-- a state nobody owns. 'failed' doubles as the dead letter — attempts and
-- last_error survive on the row, and only the retention sweep removes it.
CREATE TABLE channel_outbound_queue (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id   UUID NOT NULL,
    workspace_id      UUID NOT NULL,
    channel_type      TEXT NOT NULL,
    chat_session_id   UUID,
    -- (source_kind, source_id) is the business key that makes enqueue
    -- idempotent: the realtime producer and the reconciler both race to
    -- insert the same reply, and exactly one must win. See the unique index
    -- in migration 267.
    source_kind       TEXT NOT NULL,
    source_id         TEXT NOT NULL,
    target_chat_id    TEXT NOT NULL,
    target_chat_type  SMALLINT NOT NULL,
    msg_type          TEXT NOT NULL,
    payload_version   SMALLINT NOT NULL DEFAULT 1,
    payload           JSONB NOT NULL DEFAULT '{}'::jsonb,
    status            TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'sent', 'failed')),
    attempts          INT NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token       TEXT,
    lease_expires_at  TIMESTAMPTZ,
    sent_at           TIMESTAMPTZ,
    last_error        TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
