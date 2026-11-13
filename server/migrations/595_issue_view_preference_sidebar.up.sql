-- The sidebar's starred views become one more per-user preference
-- container: `prefs.order` lists the starred items ("builtin:<key>" /
-- "view:<uuid>") in sidebar order. scope_id is backfilled with the user id,
-- like the my scope. A separate scope keeps older clients, which write the
-- workspace doc wholesale, from ever clobbering it.
ALTER TABLE issue_view_preference DROP CONSTRAINT issue_view_preference_scope_type_check;
ALTER TABLE issue_view_preference ADD CONSTRAINT issue_view_preference_scope_type_check
    CHECK (scope_type IN ('workspace', 'my', 'project', 'sidebar'));
