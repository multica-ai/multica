-- name: CreateShareLink :one
INSERT INTO workspace_share_link (workspace_id, code, created_by, role, expires_at, max_uses)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ClaimShareLinkByCode :one
-- Atomically consume one use of a share link. The conditional UPDATE both
-- revalidates validity (active, not expired, below max_uses) and increments
-- use_count in a single statement, so concurrent joins cannot exceed max_uses
-- and a join cannot slip in after the link was revoked or expired. Returns the
-- row only if the link is still usable.
UPDATE workspace_share_link
SET use_count = use_count + 1
WHERE code = $1
  AND is_active = true
  AND (expires_at IS NULL OR expires_at > now())
  AND (max_uses IS NULL OR use_count < max_uses)
RETURNING *;

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
