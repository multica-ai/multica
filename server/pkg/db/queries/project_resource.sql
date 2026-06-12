-- name: ListProjectResources :many
SELECT * FROM project_resource
WHERE project_id = $1
ORDER BY position ASC, created_at ASC;

-- name: ListProjectResourcesForProjects :many
SELECT * FROM project_resource
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
ORDER BY project_id, position ASC, created_at ASC;

-- name: ListWorkspaceGithubRepoURLs :many
-- Returns the URLs of every github_repo project resource attached to any
-- project in the workspace. Used by the daemon repo endpoints to merge
-- project-attached repos into the workspace-level repo list so the
-- repocache picks them up automatically.
--
-- jsonb_typeof rejects non-string url values (e.g. {"url": 123} or
-- {"url": null}) at the SQL boundary so a malformed row can't leak a
-- non-URL string into the repocache. Empty / whitespace-only strings are
-- still possible from this query and are dropped by the Go normalizer
-- alongside the cross-project dedupe.
SELECT (resource_ref->>'url')::text AS url
FROM project_resource
WHERE workspace_id = $1
  AND resource_type = 'github_repo'
  AND jsonb_typeof(resource_ref->'url') = 'string';

-- name: GetProjectResource :one
SELECT * FROM project_resource
WHERE id = $1;

-- name: GetProjectResourceInWorkspace :one
SELECT * FROM project_resource
WHERE id = $1 AND workspace_id = $2;

-- name: CreateProjectResource :one
INSERT INTO project_resource (
    project_id, workspace_id, resource_type, resource_ref, label, position, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: UpdateProjectResource :one
UPDATE project_resource
SET resource_ref = $2,
    label        = $3,
    position     = $4
WHERE id = $1
RETURNING *;

-- name: DeleteProjectResource :exec
DELETE FROM project_resource WHERE id = $1;

-- name: CountProjectResources :one
SELECT count(*) FROM project_resource WHERE project_id = $1;

-- name: GetProjectResourceCounts :many
SELECT project_id, count(*)::bigint AS resource_count
FROM project_resource
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;
