-- Routines of one squad: the list endpoint and the squad panel read by squad_id.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_squad_routine_squad ON squad_routine (squad_id);
