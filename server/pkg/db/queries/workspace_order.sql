-- name: LockWorkspaceOrderMembers :many
-- Stable lock order serializes overlapping whole-list replacements.
SELECT workspace_id FROM member
WHERE user_id = $1
ORDER BY id
FOR UPDATE;

-- name: SetWorkspacePosition :exec
UPDATE member SET workspace_position = @position::integer
WHERE user_id = @user_id AND workspace_id = @workspace_id;
