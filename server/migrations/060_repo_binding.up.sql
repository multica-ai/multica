-- Refactor repos into a first-class resource with a polymorphic binding table.
--
-- Background
-- ----------
-- Until now `workspace.repos` was a JSONB array of `{url, description}` objects
-- that lived inline on the workspace row. That shape only supported one scope
-- (the whole workspace) and forced every agent in the workspace to see every
-- repo, regardless of which project or issue it was actually working on.
--
-- This migration is Step 1 of YOU-14: the schema split. Behavior is preserved
-- (every existing workspace-level entry migrates as a `workspace`-scoped
-- binding), but the data model is now ready for project- and issue-scoped
-- bindings to be added in subsequent migrations without further table churn.
--
-- The companion down migration in `060_repo_binding.down.sql` reconstructs the
-- JSONB column from the workspace bindings, so self-hosted users can roll back
-- without losing data.

CREATE TABLE repo (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    url TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Polymorphic binding by (scope_type, scope_id). scope_id intentionally has
-- no FK so the same table can point at workspace / project / issue rows
-- without introducing three near-identical join tables. Application code is
-- responsible for cleaning up bindings when the scope row is deleted.
--
-- `description` lives on the binding rather than on `repo` because the prior
-- JSONB shape stored a per-workspace description: two workspaces could bind
-- the same git URL with different descriptions ("Backend API" vs. "Shared
-- SDK"), and storing it globally on the catalog row would make one
-- workspace's edit silently overwrite what the other sees.
CREATE TABLE repo_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id UUID NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'project', 'issue')),
    scope_id UUID NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo_id, scope_type, scope_id)
);

CREATE INDEX repo_binding_scope_idx ON repo_binding (scope_type, scope_id);
CREATE INDEX repo_binding_repo_idx ON repo_binding (repo_id);

-- Backfill: extract every JSONB entry, insert each unique URL into `repo`,
-- then create a workspace-scoped binding for every (workspace, url) pair
-- carrying the original per-workspace description.
--
-- The CASE-inside-LATERAL guard is load-bearing: `jsonb_array_elements` is
-- evaluated as the table expression is built and a row whose `repos` is not a
-- JSON array (object, string, NULL — all of which the legacy PATCH path
-- could store unchecked when the column was typed `any`) would abort the
-- whole migration. Coercing non-arrays to `'[]'::jsonb` before expansion
-- makes the migration safe against any garbage that landed in the column.
WITH entries AS (
    SELECT
        w.id                                  AS workspace_id,
        TRIM(BOTH FROM elem->>'url')          AS url,
        COALESCE(TRIM(BOTH FROM elem->>'description'), '') AS description
    FROM workspace w,
         LATERAL jsonb_array_elements(
             CASE WHEN jsonb_typeof(w.repos) = 'array' THEN w.repos ELSE '[]'::jsonb END
         ) AS elem
),
filtered AS (
    SELECT workspace_id, url, description
    FROM entries
    WHERE url <> ''
),
inserted AS (
    INSERT INTO repo (url)
    SELECT DISTINCT url FROM filtered
    ON CONFLICT (url) DO NOTHING
    RETURNING id, url
)
INSERT INTO repo_binding (repo_id, scope_type, scope_id, description)
SELECT DISTINCT ON (r.id, f.workspace_id) r.id, 'workspace', f.workspace_id, f.description
FROM filtered f
JOIN repo r ON r.url = f.url
ON CONFLICT (repo_id, scope_type, scope_id) DO NOTHING;

ALTER TABLE workspace DROP COLUMN repos;
