-- name: GetRepo :one
SELECT * FROM repo WHERE id = $1;

-- name: GetRepoByURL :one
SELECT * FROM repo WHERE url = $1;

-- name: UpsertRepoByURL :one
-- Description lives on `repo_binding`, not on the repo catalog row, so the
-- upsert here is keyed only on URL. The trivial DO UPDATE bumps updated_at
-- and forces RETURNING to fire on the conflict path.
INSERT INTO repo (url)
VALUES ($1)
ON CONFLICT (url) DO UPDATE
    SET updated_at = now()
RETURNING *;

-- name: ListReposByScope :many
-- description comes from the binding, not the repo, so two scopes that share
-- the same git URL can carry different descriptions without one overwriting
-- the other.
SELECT r.id, r.url, rb.description, r.created_at, r.updated_at
FROM repo r
JOIN repo_binding rb ON rb.repo_id = r.id
WHERE rb.scope_type = $1
  AND rb.scope_id   = $2
ORDER BY r.url ASC;

-- name: CreateRepoBinding :one
-- ON CONFLICT updates description (and bumps no other column) so calling this
-- twice with different descriptions is the way a caller mutates an existing
-- binding's description in place — including the no-change case where the
-- existing row is returned verbatim. Idempotent.
INSERT INTO repo_binding (repo_id, scope_type, scope_id, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (repo_id, scope_type, scope_id) DO UPDATE
    SET description = EXCLUDED.description
RETURNING *;

-- name: DeleteRepoBinding :exec
DELETE FROM repo_binding
WHERE repo_id    = $1
  AND scope_type = $2
  AND scope_id   = $3;

-- name: DeleteRepoBindingsForScope :exec
DELETE FROM repo_binding
WHERE scope_type = $1
  AND scope_id   = $2;

-- name: DeleteOrphanRepos :exec
-- Removes repo rows that no binding references anymore. Called after a "set
-- the workspace's repos to exactly this list" operation so the catalog doesn't
-- accumulate stale entries when users prune their settings. With a single
-- workspace scope this matches the pre-refactor behavior; once project- and
-- issue-scoped bindings are introduced (Step 2 / Step 3) the same predicate
-- still keeps shared repos alive as long as any scope references them.
DELETE FROM repo
WHERE NOT EXISTS (
    SELECT 1 FROM repo_binding rb WHERE rb.repo_id = repo.id
);
