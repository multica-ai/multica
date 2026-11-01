-- The workspace's Secretary: the one agent that reads the whole company and
-- writes the boss's briefs. A plain nullable column rather than a key in
-- workspace.settings, because clients replace settings as a whole object and a
-- stale client would silently unset it. Not a foreign key: the handler
-- validates that the agent is a live agent of this workspace, and a later
-- archive leaves a dangling id that the publish check treats as "no secretary".
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS secretary_agent_id UUID;
