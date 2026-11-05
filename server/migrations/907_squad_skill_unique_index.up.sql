CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_squad_skill_squad_skill
    ON squad_skill (squad_id, skill_id);
