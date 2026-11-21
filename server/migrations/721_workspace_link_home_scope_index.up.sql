CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_link_home_scope_idx ON workspace_link (workspace_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE is_home;
