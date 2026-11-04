CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS budget_period_key_uidx ON budget_period (budget_id, period_start);
