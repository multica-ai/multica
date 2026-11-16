-- Chat wakeups (MUL-9439): an agent in a chat turn records when to come back to
-- this conversation and what to do then. The platform watches the condition or
-- the clock and starts one chat turn in the same session when it is due.
--
-- kind:
--   condition — the platform polls a stored fact (an issue's status, a run
--               ending, a pull request merging, a person replying) and fires
--               once when it holds.
--   at        — fires once at fire_at.
--   check     — the agent itself looks every interval_seconds; a check that
--               finds nothing checks in silently and the conversation stays
--               quiet. It ends when the agent finishes it or it expires.
--
-- Every wakeup has an end. expires_at is required; reaching it before the
-- condition holds starts one last turn so the agent can say where things
-- stand instead of going quiet.
--
-- requester_id is the person the turn answers to: the session creator.
CREATE TABLE chat_wakeup (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    chat_session_id UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    requester_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    source_task_id UUID,
    source_message_id UUID,
    title TEXT NOT NULL,
    instruction TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('condition', 'at', 'check')),
    condition JSONB,
    condition_state TEXT NOT NULL DEFAULT '',
    interval_seconds BIGINT,
    fire_at TIMESTAMPTZ,
    next_eval_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'fired', 'expired', 'cancelled', 'failed')),
    end_reason TEXT,
    ended_at TIMESTAMPTZ,
    check_count INTEGER NOT NULL DEFAULT 0,
    last_check_note TEXT,
    last_checked_at TIMESTAMPTZ,
    last_checkin_task_id UUID,
    last_task_id UUID,
    last_fired_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((kind = 'condition') = (condition IS NOT NULL)),
    CHECK ((kind = 'at') = (fire_at IS NOT NULL)),
    CHECK ((kind = 'check') = (interval_seconds IS NOT NULL))
);

CREATE INDEX idx_chat_wakeup_due ON chat_wakeup (next_eval_at) WHERE status = 'active';
CREATE INDEX idx_chat_wakeup_session ON chat_wakeup (chat_session_id, created_at DESC);
CREATE INDEX idx_chat_wakeup_requester ON chat_wakeup (workspace_id, requester_id, created_at DESC);

-- The hidden input row of a wakeup turn names its wakeup, so clients can head
-- the reply with what brought the agent back. NULL for every other row.
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS wakeup_id UUID;
