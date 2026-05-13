-- Adds the persona-sandbox attachment for an agent. NULL means "no persona
-- gating" — the agent runs with daemon-default sandbox config (existing
-- behaviour). A non-null value names the persona sandbox by name (e.g.
-- "claude-developer") that the daemon resolves at spawn time.
--
-- Sandbox names are deliberately stored as strings, not foreign keys: persona
-- runs in its own database, so a hard FK is not possible. Validation happens
-- at write time (the API checks the name exists in persona) and at spawn time
-- (the daemon will fail-closed if persona doesn't recognise the sandbox).
ALTER TABLE agent ADD COLUMN IF NOT EXISTS persona_sandbox TEXT;
