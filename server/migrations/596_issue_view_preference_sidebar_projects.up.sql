-- The sidebar's Projects section settings follow the user across devices:
-- one per-user document holding which projects are listed and in what
-- order, and how expanded rows list their issues. Its own scope, keyed by
-- the user id like the sidebar scope, so the starred-views list (which
-- writes its document wholesale) can never clobber it.
ALTER TABLE issue_view_preference DROP CONSTRAINT issue_view_preference_scope_type_check;
ALTER TABLE issue_view_preference ADD CONSTRAINT issue_view_preference_scope_type_check
    CHECK (scope_type IN ('workspace', 'my', 'project', 'sidebar', 'sidebar_projects'));
