-- The All Projects page's display settings (layout, grouping, sort, columns,
-- filters) follow the user across devices: one per-user document, keyed by
-- the user id like the sidebar scopes.
ALTER TABLE issue_view_preference DROP CONSTRAINT issue_view_preference_scope_type_check;
ALTER TABLE issue_view_preference ADD CONSTRAINT issue_view_preference_scope_type_check
    CHECK (scope_type IN ('workspace', 'my', 'project', 'sidebar', 'sidebar_projects', 'projects_page'));
