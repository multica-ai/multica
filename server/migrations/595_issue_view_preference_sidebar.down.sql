DELETE FROM issue_view_preference WHERE scope_type = 'sidebar';
ALTER TABLE issue_view_preference DROP CONSTRAINT issue_view_preference_scope_type_check;
ALTER TABLE issue_view_preference ADD CONSTRAINT issue_view_preference_scope_type_check
    CHECK (scope_type IN ('workspace', 'my', 'project'));
