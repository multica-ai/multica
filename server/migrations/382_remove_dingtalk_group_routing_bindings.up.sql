-- A channel chat session is permanently owned by the agent recorded on its
-- chat_session row. Remove only DingTalk group bindings that still point at an
-- agent other than the installation's default so the next @bot message creates
-- a new session for that default agent. Direct messages, other channels, and
-- groups already owned by the default agent keep their existing context.
WITH removed_bindings AS (
    DELETE FROM channel_chat_session_binding binding
    USING channel_installation installation, chat_session session
    WHERE binding.installation_id = installation.id
      AND binding.chat_session_id = session.id
      AND binding.channel_type = 'dingtalk'
      AND binding.chat_type = 'group'
      AND installation.channel_type = 'dingtalk'
      AND session.agent_id IS DISTINCT FROM installation.agent_id
    RETURNING binding.chat_session_id
)
DELETE FROM channel_outbound_card_message outbound
USING removed_bindings removed
WHERE outbound.chat_session_id = removed.chat_session_id;
