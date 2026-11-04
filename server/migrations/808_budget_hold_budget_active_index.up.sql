CREATE INDEX CONCURRENTLY IF NOT EXISTS budget_hold_budget_active_idx
    ON budget_hold (budget_id) WHERE released_at IS NULL;
