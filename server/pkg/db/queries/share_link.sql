-- name: CreateShareLink :one
INSERT INTO workspace_share_link (workspace_id, code, created_by, role, expires_at, max_uses)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ClaimShareLinkByID :one
-- The caller first resolves the opaque code and then atomically consumes the
-- non-secret row ID. The conditional UPDATE revalidates validity and the use
-- limit so a join cannot race past revocation or expiry.
UPDATE workspace_share_link
SET use_count = use_count + 1
WHERE id = $1
  AND is_active = true
  AND (expires_at IS NULL OR expires_at > now())
  AND (max_uses IS NULL OR use_count < max_uses)
RETURNING *;

-- name: GetActiveShareLinkByCode :one
SELECT * FROM workspace_share_link
WHERE code = $1
  AND is_active = true
  AND (expires_at IS NULL OR expires_at > now())
  AND (max_uses IS NULL OR use_count < max_uses);

-- name: GetShareLinkInfoByCode :one
SELECT wsl.role,
       w.name  AS workspace_name,
       w.slug  AS workspace_slug,
       u.name  AS creator_name
FROM workspace_share_link wsl
JOIN workspace w ON w.id = wsl.workspace_id
JOIN "user" u ON u.id = wsl.created_by
WHERE wsl.code = $1 AND wsl.is_active = true
  AND (wsl.expires_at IS NULL OR wsl.expires_at > now())
  AND (wsl.max_uses IS NULL OR wsl.use_count < wsl.max_uses);

-- name: ListShareLinksByWorkspace :many
SELECT wsl.*,
       u.name  AS creator_name,
       u.email AS creator_email
FROM workspace_share_link wsl
JOIN "user" u ON u.id = wsl.created_by
WHERE wsl.workspace_id = $1 AND wsl.is_active = true
ORDER BY wsl.created_at DESC;

-- name: DeactivateWorkspaceShareLinks :exec
UPDATE workspace_share_link
SET is_active = false
WHERE workspace_id = $1 AND is_active = true;

-- name: RevokeShareLink :exec
UPDATE workspace_share_link
SET is_active = false
WHERE id = $1 AND workspace_id = $2;
