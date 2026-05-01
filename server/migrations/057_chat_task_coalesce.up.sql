-- Enforce at most one queued chat task per chat_session.
-- Backs the coalesce logic in EnqueueChatTask: when the user sends multiple
-- messages while an agent is already mid-turn, we keep one running task plus
-- at most one queued successor. The successor picks up every user message
-- newer than the last assistant reply at claim time.
-- Without this index, two concurrent SendChatMessage calls could both insert
-- a queued task before either notices the other.
CREATE UNIQUE INDEX agent_task_queue_chat_session_queued_unique
    ON agent_task_queue (chat_session_id)
    WHERE status = 'queued' AND chat_session_id IS NOT NULL;
