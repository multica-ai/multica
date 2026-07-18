-- Durable draft restore for deferred cancellations (#5219). When a cancelled
-- chat task's transcript settles as empty, the triggering user message is
-- deleted and its content must return to the creator's composer. The
-- chat:cancel_finalized broadcast is best-effort, so the restore is persisted
-- here and served by a creator-authorized endpoint: a client that was offline
-- across the event fetches it on the next session open and consumes it
-- (DELETE) once applied.
--
-- No foreign keys: new tables enforce their relationships in the application
-- layer (MUL-3515). DeleteChatSession prunes this table inside its transaction,
-- alongside the channel_* tables it already prunes there.
--
-- id is the deleted user chat message's id: globally unique (it was a
-- chat_message PK), stable for client-side dedup, and one restore per task is
-- guaranteed by the atomic deferred-finalize claim.
--
-- The chat_session_id lookup index lives in migration 183: every production
-- index is built CONCURRENTLY in its own single-statement file.
CREATE TABLE chat_draft_restore (
    id              UUID PRIMARY KEY,
    chat_session_id UUID NOT NULL,
    task_id         UUID NOT NULL,
    content         TEXT NOT NULL,
    -- Detached attachment rows the restored draft re-binds; resolved to full
    -- attachment responses (URL policy included) at read time.
    attachment_ids  UUID[] NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
