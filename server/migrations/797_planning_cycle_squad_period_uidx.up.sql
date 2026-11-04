CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS planning_cycle_squad_period_uidx
    ON planning_cycle (workspace_id, period_start, squad_id);
