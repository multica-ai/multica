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
-- The thread belongs here too, with the message and sender. It is tempting to
-- call the thread "route" and read it from the binding, but that only holds
-- for thread-ISOLATED sessions. Slack DMs are the counterexample the repo
-- already encodes: slackSessionRouting keeps one binding per DM channel while
-- carrying a per-message reply thread, so the binding's cursor names whichever
-- thread spoke last. Reading it there would hand a debounced revision-1 run
-- the thread of a later revision — the same cross-generation hazard this
-- snapshot exists to close for the message and sender.
--
-- The binding keeps its own last_message_id / last_thread_id for the
-- history-boundary bookkeeping (history_start_message_id,
-- history_end_message_id). That cursor is deliberately NOT interchangeable
-- with the trigger here: it advances for channel commands (/issue) too,
-- whereas the trigger is written only for messages that are actually agent
-- input.
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
-- correct value for that generation. Only a generation enqueued with NO
-- post-deploy append can read NULL here — the recovered-run path — and that is
-- exactly the case a backfill cannot serve: the binding cursor it would copy
-- from has already advanced past that generation, so seeding would supply a
-- confidently wrong message and sender rather than no answer.
--
-- What a NULL trigger costs depends on where the answer is going, and
-- CreateChannelTaskDeliveryFromSession is written around that:
--
--   * Ordinary chat, p2p, Slack DM — a chat-level send lands in the very
--     conversation the session belongs to. No quote, no mention, right place.
--
--   * Thread-isolated session (Lark topic, Slack channel thread, Telegram
--     forum topic) — the thread is recovered from the binding, which for these
--     bindings is a stable property of the session rather than a cursor. The
--     answer still reaches its thread.
--
--   * Lark topic specifically — Lark can only enter a topic by replying to a
--     message inside it, so a recovered thread is not enough. The Patcher
--     declines to send rather than posting into the parent group; see
--     topicSendWithoutTrigger.
