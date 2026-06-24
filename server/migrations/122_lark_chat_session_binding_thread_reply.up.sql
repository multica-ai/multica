-- Thread-aware outbound: remember the most recent inbound trigger
-- message (and the Lark topic / thread it belongs to, if any) per chat
-- binding so the decoupled outbound patcher can thread its reply back
-- into the originating 话题 (thread) instead of always posting a fresh
-- message at the chat level.
--
-- The outbound side is event-driven and disconnected from the inbound
-- message: it only knows the chat_session → lark_chat_session_binding
-- (i.e. the lark_chat_id). Persisting the latest trigger here is what
-- lets EventChatDone resolve a reply target without plumbing the
-- message id through the whole task lifecycle.
--
-- Both columns are nullable:
--   * last_lark_message_id is the message_id the reply quotes / replies
--     to (Lark's reply endpoint keys off it).
--   * last_lark_thread_id is the message's thread_id; Lark only sends a
--     thread_id for messages that are part of a topic, so a NULL/empty
--     value means "the last trigger was a normal chat message" and the
--     outbound keeps the existing chat-level send behavior. Only when a
--     thread_id is present does the patcher switch to a thread reply.
ALTER TABLE lark_chat_session_binding
    ADD COLUMN last_lark_message_id TEXT,
    ADD COLUMN last_lark_thread_id  TEXT;
