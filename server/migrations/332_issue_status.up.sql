-- Custom Issue Status catalog (MUL-6243).
--
-- MODEL: the 7 categories map ONE-TO-ONE onto the 7 built-in statuses, and a
-- category's value IS its canonical status key. A custom status names a
-- category and inherits that canonical's platform behavior wholesale, so there
-- is no second "behaves_as" concept and no way to build a status that inherits
-- only half of a behavior.
--
-- `issue.status` stays the authoritative TEXT column and keeps holding a key
-- from this catalog, so no status_id, no backfill, and no double-write: every
-- existing `WHERE status = 'todo'` and `issue.Status == "backlog"` keeps
-- working verbatim because the 7 built-in keys never change.
--
-- No foreign keys (workspace_id is an application-layer relation, per the
-- project's database rules); cleanup is handled by the application.
--
-- id is NOT declared inline as PRIMARY KEY: the repo convention (see migrations
-- 327-329) is to build the table without a primary key, create the backing
-- unique index CONCURRENTLY in its own single-statement migration (333), then
-- attach it with PRIMARY KEY USING INDEX (334). An inline PRIMARY KEY would
-- build its index non-concurrently, which the project's migration rules forbid
-- even on a new table.
CREATE TABLE issue_status (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,

    -- Stable machine handle. Immutable after create: it is what `issue.status`
    -- stores, what the CLI accepts, and what agent instructions reference.
    key TEXT NOT NULL CHECK (key ~ '^[a-z0-9][a-z0-9_]{0,31}$'),

    -- Human-facing label. Editable for custom statuses; locked for built-ins.
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 256),

    -- Behavior equivalence class. Immutable after create — changing it would
    -- silently rewrite the machine semantics of every issue already sitting on
    -- this status, with no audit trail.
    category TEXT NOT NULL CHECK (
        category IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled')
    ),

    -- "#rrggbb". Built-in rows carry the seeded palette for API consumers, but
    -- clients that already know a built-in key keep rendering it from their own
    -- config, so the default experience stays pixel-identical.
    color TEXT NOT NULL CHECK (color ~ '^#[0-9a-f]{6}$'),

    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- A built-in row IS its category's canonical, so its key must equal its
    -- category. This is what makes Effective() an identity function on built-in
    -- keys, which is in turn what keeps every existing status check correct.
    CONSTRAINT issue_status_system_is_canonical
        CHECK (NOT is_system OR key = category),

    -- Archiving a built-in would delete its category's behavior definition.
    CONSTRAINT issue_status_system_not_archivable
        CHECK (NOT is_system OR archived_at IS NULL)
);
