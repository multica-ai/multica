-- Supersede-don't-move channel chat bindings on /new.
--
-- Previously the single (installation_id, channel_chat_id) binding row was
-- repointed in place when /new started a fresh session (RebindChannelChatSession
-- UPDATE-d chat_session_id). That orphaned any still-in-flight turn of the OLD
-- session: the decoupled outbound patcher reverse-looks-up the binding by
-- chat_session_id (GetChannelChatSessionBindingBySession), and after the move no
-- row referenced the old session, so its completion reply was silently dropped
-- (DingTalk, Feishu, Slack all share this reverse lookup).
--
-- Fix: keep one binding row per chat_session. /new marks the current row inactive
-- and inserts a fresh active row, so the old session stays reverse-resolvable and
-- its in-flight reply still reaches the chat. "The current session for a chat"
-- becomes a partial unique index over active rows (superseded rows may coexist).

ALTER TABLE channel_chat_session_binding
    ADD COLUMN active boolean NOT NULL DEFAULT true;

-- The plain UNIQUE(installation_id, channel_chat_id) allowed only one row per
-- chat; replace it with a partial unique index so only the ACTIVE binding is
-- unique per chat while superseded rows are retained.
ALTER TABLE channel_chat_session_binding
    DROP CONSTRAINT IF EXISTS channel_chat_session_binding_installation_id_channel_chat_i_key;

CREATE UNIQUE INDEX channel_chat_session_binding_active_chat_key
    ON channel_chat_session_binding (installation_id, channel_chat_id)
    WHERE active;
