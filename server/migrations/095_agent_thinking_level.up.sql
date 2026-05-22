-- Per-agent thinking / reasoning effort setting. Stored as the
-- runtime-native string (e.g. Claude's "low|medium|high|xhigh|max",
-- Codex's "none|minimal|low|medium|high|xhigh") rather than a
-- cross-runtime abstraction, so the user-visible value matches what
-- each CLI's own UI advertises (see MUL-2339). NULL means "use the
-- runtime/model default" — every backend treats this as "do not
-- inject --effort / reasoning_effort" and lets the CLI pick.
ALTER TABLE agent ADD COLUMN thinking_level TEXT;
