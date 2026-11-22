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
-- requester_id is the person the turn answers to: whoever sent the message
-- that asked for it, which in a group chat need not be the session creator.
--
-- No foreign keys (repo rule): DeleteChatSession, workspace teardown and
-- system-agent deletion remove a session's wakeups in the same transaction,
-- and the dispatcher drops a wakeup whose session is gone.
CREATE TABLE IF NOT EXISTS chat_wakeup (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    chat_session_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    requester_id UUID NOT NULL,
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

-- Indexes are built concurrently in 676–678.

-- The hidden input row of a wakeup turn names its wakeup, so clients can head
-- the reply with what brought the agent back. NULL for every other row.
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS wakeup_id UUID;
