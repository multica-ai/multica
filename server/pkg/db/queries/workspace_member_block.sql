-- name: BlockWorkspaceRejoin :exec
INSERT INTO workspace_member_block (workspace_id, email, blocked_by)
VALUES (@workspace_id, lower(@email::text), sqlc.narg('blocked_by'))
ON CONFLICT (workspace_id, email) DO NOTHING;

-- name: IsBlockedFromWorkspace :one
SELECT EXISTS (
    SELECT 1 FROM workspace_member_block
    WHERE workspace_id = @workspace_id AND email = lower(@email::text)
)::bool AS blocked;

-- name: ListWorkspaceMemberBlocks :many
SELECT * FROM workspace_member_block
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: DeleteWorkspaceMemberBlock :execrows
DELETE FROM workspace_member_block
WHERE id = @id AND workspace_id = @workspace_id;
