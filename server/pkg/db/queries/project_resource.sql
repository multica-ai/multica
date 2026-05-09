-- name: ListProjectResources :many
SELECT * FROM project_resource
WHERE project_id = $1
ORDER BY position ASC, created_at ASC;

-- name: ListProjectResourcesForProjects :many
SELECT * FROM project_resource
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
ORDER BY project_id, position ASC, created_at ASC;

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

-- name: DeleteProjectResource :exec
DELETE FROM project_resource WHERE id = $1;

-- name: CountProjectResources :one
SELECT count(*) FROM project_resource WHERE project_id = $1;

-- name: GetProjectResourceCounts :many
SELECT project_id, count(*)::bigint AS resource_count
FROM project_resource
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;

-- name: FindProjectByRepoURL :one
-- Webhook ingestion needs the inverse mapping: given a repo_url from a
-- GitHub event, which project (in this workspace) should we attach the
-- PR / deploy row to? The github_repo resource_ref carries the URL we
-- match against. LIMIT 1 because the same repo can in theory be
-- attached to multiple projects in the same workspace; we pick the
-- earliest by position so behavior is deterministic.
SELECT p.* FROM project p
JOIN project_resource pr ON pr.project_id = p.id
WHERE p.workspace_id = sqlc.arg('workspace_id')
  AND pr.resource_type = 'github_repo'
  AND pr.resource_ref->>'url' = sqlc.arg('repo_url')::text
ORDER BY pr.position ASC, pr.created_at ASC
LIMIT 1;
