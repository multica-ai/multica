-- The goal tree of a scope, and the goals a squad or initiative owns.
CREATE INDEX CONCURRENTLY IF NOT EXISTS goal_scope_idx ON goal (workspace_id, scope_type, scope_id);
