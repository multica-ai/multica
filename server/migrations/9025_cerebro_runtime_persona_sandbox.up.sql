-- Hard upper-bound persona sandbox per runtime. NULL = no upper bound, the
-- agent-level sandbox decides alone. A non-null value names the persona
-- sandbox (e.g. "claude-developer") that caps every agent on this runtime —
-- the daemon enforces "most restrictive of (agent, runtime)" at spawn.
ALTER TABLE agent_runtime ADD COLUMN IF NOT EXISTS persona_sandbox TEXT;

-- Capability snapshot reported by the daemon at registration time. JSON
-- shape is loose on purpose so different providers can report what they
-- have (Claude Code's tool list, MCP servers configured, etc.) without
-- a schema migration per provider. Persona's UI reads this to surface
-- "what your runtime can actually do" alongside the abstract sandbox.
ALTER TABLE agent_runtime ADD COLUMN IF NOT EXISTS capabilities JSONB NOT NULL DEFAULT '{}'::jsonb;
