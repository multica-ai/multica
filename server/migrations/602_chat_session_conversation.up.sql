-- A chat_session that carries one agent's context inside a Team conversation
-- (MUL-8357): the agent's replies in a channel or channel thread run through
-- the existing chat task pipeline on this hidden session and are mirrored into
-- conversation_message. NULL for every existing and every user-facing session;
-- set sessions are never listed. No foreign key by project rule.
ALTER TABLE chat_session
    ADD COLUMN IF NOT EXISTS conversation_id UUID;
