-- One active budget per scope and period. The company scope has no scope_id,
-- so the expression maps NULL to the nil UUID to make it unique too.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS budget_active_scope_uidx
    ON budget (workspace_id, scope_type, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid), period)
    WHERE status = 'active';
