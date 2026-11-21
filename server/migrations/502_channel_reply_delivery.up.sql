-- One row per user turn: who is currently delivering that turn's reply into a
-- chat channel, what the provider has already accepted, and how far it got.
--
-- The streamed placeholder, the final answer and the failure notice are three
-- code paths, and across replicas three processes — the daemon's transcript
-- report and its completion callback are independent HTTP requests that land
-- wherever the load balancer sends them, and the event bus is in-process. A
-- process that cannot see the placeholder posts its own copy of the same
-- answer (GH #8049, #7750). This row is where they agree.
--
-- Keyed by turn, not by task: an automatic retry runs under a new task id and
-- must finish the reply its previous attempt started rather than answer beside
-- it. turn_id is the root of the agent_task_queue retry_of_task_id chain, so
-- the relationship is the real lineage and never a guess about which pending
-- reply looked closest.
CREATE TABLE IF NOT EXISTS channel_reply_delivery (
    turn_id UUID NOT NULL,
    -- The attempt holding the turn most recently; diagnostics, never identity.
    -- How far down the retry chain that attempt sits is added by migration 506,
    -- which is also where a database created by this file gets it.
    task_id UUID NOT NULL,
    binding_id UUID NOT NULL,
    installation_id UUID NOT NULL,
    channel_type TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    -- streaming: the placeholder is the live reply and may still be edited.
    -- terminal:  the final answer has taken over; no placeholder may reopen.
    -- settled:   delivery is over; nothing may send or edit for this turn.
    phase TEXT NOT NULL CHECK (phase IN ('streaming', 'terminal', 'settled')),
    -- The state of the one send that may be outstanding, placeholder or chunk.
    -- none:      nothing is out.
    -- in_flight: a send is out; the provider may already have accepted it.
    -- known:     accepted, and message_id identifies the editable message.
    -- unknown:   accepted or not — the response was lost. Telegram's
    --            sendMessage has no caller-supplied idempotency key, so
    --            re-sending here cannot be deduplicated by the provider.
    --            Delivery stops and this row keeps the evidence.
    send_state TEXT NOT NULL CHECK (send_state IN ('none', 'in_flight', 'known', 'unknown')),
    -- The message later paths edit. Independent of chunks_sent: a placeholder
    -- exists long before any part of the final answer has been delivered, and
    -- conflating the two silently truncates the reply.
    message_id TEXT NOT NULL DEFAULT '',
    -- Parts of the FINAL answer already in the chat. Never set by the
    -- placeholder.
    chunks_sent INTEGER NOT NULL DEFAULT 0 CHECK (chunks_sent >= 0),
    -- The delivery lease. Exactly one process may act on a turn at a time;
    -- every state write proves it still holds the token. A lease that expires
    -- is a process that died mid-delivery — the successor may take the turn
    -- over, but an outstanding send stays unknown rather than being retried.
    owner_token UUID,
    owner_expires_at TIMESTAMPTZ,
    settled_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
