-- name: GetRepo :one
SELECT * FROM repo WHERE id = $1;

-- name: GetRepoByURL :one
SELECT * FROM repo WHERE url = $1;

-- name: UpsertRepoByURL :one
INSERT INTO repo (url, description)
VALUES ($1, $2)
ON CONFLICT (url) DO UPDATE
    SET description = EXCLUDED.description,
        updated_at  = now()
RETURNING *;

-- name: ListReposByScope :many
SELECT r.*
FROM repo r
JOIN repo_binding rb ON rb.repo_id = r.id
WHERE rb.scope_type = $1
  AND rb.scope_id   = $2
ORDER BY r.url ASC;

-- name: CreateRepoBinding :one
-- ON CONFLICT updates a no-op column so the existing row is returned, which
-- makes the call idempotent and gives the caller a stable RepoBinding back
-- whether or not the binding existed before.
INSERT INTO repo_binding (repo_id, scope_type, scope_id)
VALUES ($1, $2, $3)
ON CONFLICT (repo_id, scope_type, scope_id) DO UPDATE
    SET repo_id = EXCLUDED.repo_id
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
