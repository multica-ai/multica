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
--
-- No index: every read here goes through an existing key (chat_session_id +
-- revision, or task_id).
ALTER TABLE channel_chat_context_generation
    ADD COLUMN IF NOT EXISTS last_message_id TEXT,
    ADD COLUMN IF NOT EXISTS last_thread_id  TEXT,
    ADD COLUMN IF NOT EXISTS last_sender_id  TEXT;

ALTER TABLE channel_task_delivery
    ADD COLUMN IF NOT EXISTS channel_sender_id TEXT;

-- Seed the generation that is current for each session from the binding's
-- cursor, so live sessions keep replying to the right message across the
-- deploy instead of degrading to a chat-level send until their next inbound
-- turn. Only the CURRENT revision is seeded: for any older generation the
-- binding cursor has already moved on and would be exactly the wrong value.
-- last_sender_id has no source to seed from and stays NULL — those sessions
-- reply without a mention until their next turn records one.
UPDATE channel_chat_context_generation AS generation
SET last_message_id = binding.last_message_id,
    last_thread_id  = binding.last_thread_id
FROM channel_chat_session_binding AS binding
WHERE binding.chat_session_id = generation.chat_session_id
  AND binding.context_revision = generation.revision
  AND generation.last_message_id IS NULL;
