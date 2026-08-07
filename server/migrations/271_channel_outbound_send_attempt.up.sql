-- Per-target send-attempt ledger backing the outbound rate gate.
--
-- Why a ledger and not an in-memory counter: the platform's quota is per
-- target chat, counted at the platform. An in-process token bucket only knows
-- what this replica sent, so after a lease flip the new holder starts from zero
-- and walks straight back into the quota. The row is the shared count.
--
-- attempted_at is stamped when the frame is handed to the socket, not when the
-- platform acknowledges it: an ambiguous write may well have arrived, and the
-- quota is the platform's view, so an unacknowledged attempt must still be
-- counted. Over-counting costs a short deferral; under-counting costs a
-- rate-limit rejection on a user-visible reply.
--
-- Rows are the input to a sliding-window COUNT, not durable history, so the
-- reconciler purges them on a short retention.
--
-- No foreign keys (repo rule): the installation and session teardown paths in
-- channel.sql and channel_outbound_queue.sql delete these rows explicitly.
CREATE TABLE channel_outbound_send_attempt (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    queue_id           UUID NOT NULL,
    installation_id    UUID NOT NULL,
    workspace_id       UUID NOT NULL,
    chat_session_id    UUID,
    target_chat_id     TEXT NOT NULL,
    target_chat_type   SMALLINT NOT NULL,
    attempted_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
