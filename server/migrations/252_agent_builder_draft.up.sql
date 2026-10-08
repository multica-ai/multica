-- The agent configuration a creation conversation has arrived at.
--
-- Half of this state was already durable: every reply from the builder ends in
-- an <agent_draft> block, so the client could always replay the last one out of
-- chat_message. What was not durable is the half the USER types — a rewritten
-- instruction, an unticked skill — which lived only in React state and died on
-- every reload, tab switch and navigation. Splitting one configuration across
-- "the AI's half on the server, yours in the browser" also forced the client to
-- arbitrate between the two on every restore. This table makes the whole thing
-- one server-owned object.
--
-- `draft` is opaque to the server. The shape is the studio's AgentDraft and is
-- validated client-side against a zod schema; teaching Postgres and the handler
-- about it would create a second definition to keep in sync for no gain, since
-- nothing server-side ever reads a field. The client also stores which
-- assistant message it has already applied, so a restore does not re-apply the
-- last reply over edits made after it.
--
-- No foreign key (repo rule), so deletion is explicit in application code —
-- DeleteChatSession, the runtime teardown agent cascade, and the workspace
-- teardown each prune this table, exactly as they already do for
-- chat_draft_restore.
--
-- chat_session_id is the primary key rather than a surrogate id: a conversation
-- has exactly one current draft, and making that a constraint means the write
-- path is a plain upsert with no room for duplicate rows to accumulate. It also
-- provides the lookup index, so no separate CREATE INDEX CONCURRENTLY is needed.
CREATE TABLE agent_builder_draft (
    chat_session_id UUID PRIMARY KEY,
    -- Denormalised so the workspace teardown can prune without joining through
    -- chat_session, which that statement deletes in the same CTE.
    workspace_id    UUID NOT NULL,
    draft           JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
