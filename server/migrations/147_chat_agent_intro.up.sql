-- Flags a chat_session auto-created when its agent was created, so its first
-- turn is a proactive self-introduction from the agent. Such a session carries
-- NO visible user message: the intro run is driven server-side (see the chat
-- prompt in server/internal/daemon/prompt.go) so the thread reads as the agent
-- messaging its creator first, not the creator prompting the agent.
ALTER TABLE chat_session ADD COLUMN is_agent_intro BOOLEAN NOT NULL DEFAULT FALSE;
