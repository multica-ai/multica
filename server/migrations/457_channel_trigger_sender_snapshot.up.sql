-- Freeze each trigger's reply target AND its channel-native sender per context
-- generation, so an outbound reply answers exactly the message that asked and
-- @-mentions exactly the account that sent it (#8234).
--
-- Two earlier shapes were wrong, both for the same underlying reason — the
-- identity was read from something coarser than "this trigger":
--
--   * Resolving the open_id at send time from the task's initiator_user_id.
--     channel_user_binding is unique on (installation_id, channel_user_id) and
--     NOT on (installation_id, multica_user_id), so a member who binds a
--     second Feishu account on one installation has two rows and the reverse
--     lookup could name either.
--
--   * Recording the trigger on channel_chat_session_binding, which holds one
--     row per chat_session and therefore only ever remembers the LATEST
--     trigger. A run debounced in revision 1 has its delivery row created
--     after a /clear opened revision 2, so it would freeze the newer
--     generation's message and sender: reply to A's question, quote and
--     mention B.
--
-- channel_chat_context_generation is keyed (chat_session_id, revision) and
-- already snapshots initiator_user_id for exactly this reason — see
-- SetChannelChatContextInitiator, whose comment names the same hazard for
-- crash recovery. The trigger belongs beside it. channel_task_delivery then
-- freezes the generation's values per task.
--
-- The binding keeps its own last_message_id / last_thread_id: those drive the
-- history-boundary bookkeeping (history_start_message_id, history_end_message_id)
-- which is genuinely a per-session latest-trigger cursor, not per-generation.
-- That cursor is deliberately NOT interchangeable with the values here: it
-- advances for channel commands (/issue) too, whereas the generation trigger
-- is written only for messages that are actually agent input.
--
-- No index: every read here goes through an existing key (chat_session_id +
-- revision, or task_id).
ALTER TABLE channel_chat_context_generation
    ADD COLUMN IF NOT EXISTS last_message_id TEXT,
    ADD COLUMN IF NOT EXISTS last_thread_id  TEXT,
    ADD COLUMN IF NOT EXISTS last_sender_id  TEXT;

ALTER TABLE channel_task_delivery
    ADD COLUMN IF NOT EXISTS channel_sender_id TEXT;

-- Existing generations intentionally keep a NULL trigger — no backfill, in
-- line with 451_agent_task_comment_thread's "pre-migration rows drain without
-- rewriting historical data".
--
-- Nothing needs one. The trigger is recorded during AppendUserMessage, which
-- commits before the debounced flush that enqueues the task and creates its
-- delivery row, so the first inbound turn after deploy already supplies a
-- correct value for that generation. Only a generation that is enqueued with
-- NO post-deploy append could read NULL here — the recovered-run path for an
-- older unowned generation — and that is exactly the case a backfill cannot
-- serve: the binding cursor it would have to copy from has already advanced
-- past that generation, so seeding would supply a confidently wrong message
-- and sender rather than no answer.
--
-- A NULL degrades to the chat-level send with no mention, which for those runs
-- is strictly better than what they got before this change: the session's
-- newest trigger, i.e. some other member's message.
