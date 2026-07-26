-- FIR-3805: "always on" per agent-skill binding.
--
-- A bound skill normally reaches the agent as one line in the brief (name +
-- description) and is loaded on demand. When always_on is true the skill's full
-- text is pasted into the agent's instructions on every single run instead, so
-- rules that must ALWAYS apply (writing style, check-before-you-send gates)
-- cannot be skipped by the agent deciding not to open the skill.
--
-- Default false — every existing binding keeps today's behaviour.
ALTER TABLE agent_skill
    ADD COLUMN IF NOT EXISTS always_on BOOLEAN NOT NULL DEFAULT FALSE;

-- The brief builder reads "which of this agent's skills are always on" on every
-- task dispatch; a partial index keeps that lookup cheap without paying for the
-- overwhelmingly common always_on = false rows.
CREATE INDEX IF NOT EXISTS idx_agent_skill_always_on
    ON agent_skill (agent_id)
    WHERE always_on;
