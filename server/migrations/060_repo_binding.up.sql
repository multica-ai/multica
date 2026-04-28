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
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Polymorphic binding by (scope_type, scope_id). scope_id intentionally has
-- no FK so the same table can point at workspace / project / issue rows
-- without introducing three near-identical join tables. Application code is
-- responsible for cleaning up bindings when the scope row is deleted.
CREATE TABLE repo_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id UUID NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'project', 'issue')),
    scope_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo_id, scope_type, scope_id)
);

CREATE INDEX repo_binding_scope_idx ON repo_binding (scope_type, scope_id);
CREATE INDEX repo_binding_repo_idx ON repo_binding (repo_id);

-- Backfill: extract every JSONB entry, insert each unique URL into `repo`,
-- then create a workspace-scoped binding for every (workspace, url) pair.
-- Empty / whitespace-only URLs are dropped (matching the runtime
-- `normalizeWorkspaceRepos` behavior).
WITH entries AS (
    SELECT
        w.id                                  AS workspace_id,
        TRIM(BOTH FROM elem->>'url')          AS url,
        COALESCE(TRIM(BOTH FROM elem->>'description'), '') AS description
    FROM workspace w,
         LATERAL jsonb_array_elements(w.repos) AS elem
    WHERE w.repos IS NOT NULL
      AND jsonb_typeof(w.repos) = 'array'
      AND jsonb_array_length(w.repos) > 0
),
filtered AS (
    SELECT workspace_id, url, description
    FROM entries
    WHERE url <> ''
),
unique_urls AS (
    -- Pick a single description per URL — the lexicographically smallest one
    -- so the result is deterministic across re-runs.
    SELECT url, MIN(description) AS description
    FROM filtered
    GROUP BY url
),
inserted AS (
    INSERT INTO repo (url, description)
    SELECT url, description FROM unique_urls
    ON CONFLICT (url) DO NOTHING
    RETURNING id, url
)
INSERT INTO repo_binding (repo_id, scope_type, scope_id)
SELECT DISTINCT r.id, 'workspace', f.workspace_id
FROM filtered f
JOIN repo r ON r.url = f.url
ON CONFLICT (repo_id, scope_type, scope_id) DO NOTHING;

ALTER TABLE workspace DROP COLUMN repos;
