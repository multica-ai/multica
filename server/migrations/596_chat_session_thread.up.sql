-- Threads in a direct chat (MUL-8357). A thread is an ordinary chat_session
-- whose parent_chat_session_id names the conversation it branched from and
-- whose root_chat_message_id names the message it replies to, so sending,
-- streaming, queueing and read state reuse the existing chat machinery.
-- Both stay NULL for every existing and every top-level session. No foreign
-- keys by project rule; the handler validates the pair on create.
ALTER TABLE chat_session
    ADD COLUMN IF NOT EXISTS parent_chat_session_id UUID,
    ADD COLUMN IF NOT EXISTS root_chat_message_id UUID;
