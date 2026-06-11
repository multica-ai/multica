-- Add scout_agent_id to workspace: a single optional agent that acts as the
-- workspace's "scout" (monitors incoming work and routes/summarises issues).
-- ON DELETE SET NULL means archiving or deleting the agent automatically
-- clears the pointer without requiring a separate cleanup step.
ALTER TABLE workspace ADD COLUMN scout_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL;
