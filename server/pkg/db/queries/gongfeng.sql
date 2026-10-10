-- name: ListGongfengRepositories :many
SELECT gr.* FROM gongfeng_repository gr
JOIN vcs_connection vc ON vc.id = gr.connection_id
WHERE gr.connection_id = $1 AND vc.workspace_id = $2
ORDER BY gr.path;

-- name: GetGongfengRepository :one
SELECT * FROM gongfeng_repository WHERE connection_id = $1 AND project_id = $2;

-- name: AddGongfengRepository :one
INSERT INTO gongfeng_repository (connection_id, project_id, path, web_url, clone_url, description, default_branch)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (connection_id, project_id) DO UPDATE SET
    path = EXCLUDED.path, web_url = EXCLUDED.web_url, clone_url = EXCLUDED.clone_url,
    description = EXCLUDED.description, default_branch = EXCLUDED.default_branch
RETURNING *;

-- name: DeleteGongfengRepository :exec
DELETE FROM gongfeng_repository WHERE connection_id = $1 AND project_id = $2;

-- name: ResetGongfengRepositorySync :one
UPDATE gongfeng_repository SET next_page = 1, scan_started_at = NULL, synced_at = NULL, attempted_at = NULL,
    hook_revision = NULL, sync_error = '', updated_at = now()
WHERE connection_id = $1 AND project_id = $2 RETURNING *;

-- name: ResetGongfengConnectionHooks :exec
UPDATE gongfeng_repository SET hook_revision = NULL, attempted_at = NULL, updated_at = now()
WHERE connection_id = $1;

-- name: ListDueGongfengRepositories :many
SELECT gr.* FROM gongfeng_repository gr JOIN vcs_connection vc ON vc.id = gr.connection_id
WHERE vc.provider = 'gongfeng' AND
    (gr.attempted_at IS NULL OR gr.attempted_at < now() - CASE WHEN gr.sync_error <> '' THEN interval '5 minutes' ELSE interval '1 minute' END)
-- Rotate across bounded batches even when an entire connection is paused.
ORDER BY (ROW(gr.connection_id, gr.project_id) > ROW(sqlc.arg('after_connection')::uuid, sqlc.arg('after_project')::bigint)) DESC,
    gr.connection_id, gr.project_id LIMIT 20;

-- name: BeginGongfengRepositorySync :one
UPDATE gongfeng_repository SET attempted_at = now(), scan_started_at = COALESCE(scan_started_at, now())
WHERE connection_id = $1 AND project_id = $2 AND updated_at = $3 RETURNING *;

-- name: SaveGongfengRepositorySync :execrows
UPDATE gongfeng_repository SET hook_id = $4, next_page = $5, sync_error = $6, hook_revision = sqlc.narg('hook_revision'),
    synced_at = CASE WHEN sqlc.arg('completed')::boolean THEN scan_started_at ELSE synced_at END,
    scan_started_at = CASE WHEN sqlc.arg('completed')::boolean THEN NULL ELSE scan_started_at END
WHERE connection_id = $1 AND project_id = $2 AND updated_at = $3;

-- name: ListDueGongfengPullRequests :many
SELECT pr.* FROM vcs_pull_request pr JOIN vcs_connection vc ON vc.id = pr.connection_id
WHERE pr.provider = 'gongfeng' AND pr.state IN ('open', 'draft')
    AND EXISTS (SELECT 1 FROM issue_vcs_pull_request ipr WHERE ipr.pull_request_id = pr.id)
    AND (pr.snapshot_attempted_at IS NULL OR (pr.snapshot_head_sha <> pr.head_sha AND pr.snapshot_error = '') OR pr.snapshot_attempted_at < now() - CASE WHEN pr.snapshot_error <> '' THEN interval '5 minutes' ELSE interval '2 minutes' END)
ORDER BY (pr.id > sqlc.arg('after_id')::uuid) DESC, pr.id LIMIT 40;

-- name: SaveGongfengSnapshot :execrows
UPDATE vcs_pull_request SET snapshot_head_sha = $2, snapshot_fetched_at = now(), snapshot_attempted_at = now(), snapshot = $3, snapshot_error = ''
WHERE vcs_pull_request.id = $1 AND head_sha = $2 AND pr_updated_at = $4
    AND EXISTS (SELECT 1 FROM vcs_connection vc WHERE vc.id = vcs_pull_request.connection_id AND vc.updated_at = $5);

-- name: FailGongfengSnapshot :exec
UPDATE vcs_pull_request SET snapshot_error = $2, snapshot_attempted_at = now()
WHERE id = $1 AND head_sha = $3 AND pr_updated_at = $4;
