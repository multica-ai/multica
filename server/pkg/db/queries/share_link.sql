-- name: CreateShareLink :one
INSERT INTO workspace_share_link (workspace_id, code, created_by, role, expires_at, max_uses)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetShareLinkByCode :one
SELECT * FROM workspace_share_link
WHERE code = $1 AND is_active = true
  AND (expires_at IS NULL OR expires_at > now())
  AND (max_uses IS NULL OR use_count < max_uses);

-- name: ListShareLinksByWorkspace :many
SELECT wsl.*,
       u.name  AS creator_name,
       u.email AS creator_email
FROM workspace_share_link wsl
JOIN "user" u ON u.id = wsl.created_by
WHERE wsl.workspace_id = $1 AND wsl.is_active = true
ORDER BY wsl.created_at DESC;

-- name: RevokeShareLink :exec
UPDATE workspace_share_link
SET is_active = false
WHERE id = $1 AND workspace_id = $2;

-- name: IncrementShareLinkUseCount :exec
UPDATE workspace_share_link
SET use_count = use_count + 1
WHERE id = $1;
