-- At most one hold in force per task. A refused task is recorded once; the
-- insert uses ON CONFLICT on this index, so concurrent claims cannot double it.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS budget_hold_active_task_uidx
    ON budget_hold (task_id) WHERE released_at IS NULL;
