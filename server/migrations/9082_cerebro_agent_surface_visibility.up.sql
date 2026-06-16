-- TECH-3670: per-agent, per-surface discovery visibility.
--
-- Orthogonal to agent.visibility (workspace|private), which governs who may
-- ASSIGN/TRIGGER an agent. This column governs where a non-owner member can
-- DISCOVER the agent (lists, @-mention, chat picker, channel picker).
--
-- Shape: a JSON object whose keys are surfaces and whose values are booleans
-- meaning "visible to a non-owner member on this surface":
--   {"lists": false, "mention": false, "chat": true, "channels": false}
-- A missing key defaults to true (visible). A NULL column means "visible
-- everywhere" — the legacy behaviour, so existing agents are unchanged.
--
-- The "Skjult" preset (TECH-3670 simple mode) sets every surface to false;
-- the advanced mode lets the owner set each surface independently. Owner and
-- workspace owner/admin always see the agent regardless of this column.
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS surface_visibility JSONB;
