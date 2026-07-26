DROP INDEX IF EXISTS idx_agent_skill_always_on;

ALTER TABLE agent_skill
    DROP COLUMN IF EXISTS always_on;
