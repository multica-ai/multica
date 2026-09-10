-- Per-user view-bar preferences for one surface container: which items
-- (built-in tabs and saved views) are hidden and in what order they render.
-- `prefs` is a small client-owned doc: { "hidden": [...], "order": [...] }
-- with item ids like "builtin:all" / "view:<uuid>". Single-user data —
-- last-write-wins, no revision. scope_id is backfilled (workspace id for
-- workspace scope, user id for my scope, project id for project scope) so
-- the composite primary key never depends on NULL. No FKs by repository
-- policy; the member-revoke transaction deletes these rows explicitly.
CREATE TABLE issue_view_preference (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'my', 'project')),
    scope_id UUID NOT NULL,
    prefs JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(prefs) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id, scope_type, scope_id)
);
