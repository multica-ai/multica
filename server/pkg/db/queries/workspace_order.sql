-- name: LockWorkspaceOrderMembers :many
-- Stable lock order serializes overlapping whole-list replacements. Same
-- role filter as ListWorkspaces, so the set matches what the client sees.
SELECT workspace_id FROM member
WHERE user_id = $1
  AND role IN ('owner', 'admin', 'member', 'guest')
ORDER BY id
FOR UPDATE;

-- name: SetWorkspacePosition :exec
UPDATE member SET workspace_position = @position::integer
WHERE user_id = @user_id AND workspace_id = @workspace_id;
