-- name: GetRecentAgentSelection :one
SELECT *
FROM recent_agent_selection
WHERE workspace_id = @workspace_id
  AND user_id = @user_id;

-- name: UpsertRecentAgentSelection :one
INSERT INTO recent_agent_selection (workspace_id, user_id, agent_ids)
VALUES (@workspace_id, @user_id, ARRAY[sqlc.arg('agent_id')::uuid])
ON CONFLICT (workspace_id, user_id) DO UPDATE SET
    agent_ids = (
        ARRAY[EXCLUDED.agent_ids[1]]
        || array_remove(recent_agent_selection.agent_ids, EXCLUDED.agent_ids[1])
    )[1:10],
    updated_at = now()
RETURNING *;

-- name: LockWorkspaceForRecentAgentSelection :one
-- Keep this FK-free write ordered behind workspace teardown. DeleteWorkspace
-- takes FOR UPDATE on the same row before pruning recent_agent_selection.
SELECT id
FROM workspace
WHERE id = @workspace_id
FOR KEY SHARE;

-- name: DeleteRecentAgentSelectionForMember :exec
DELETE FROM recent_agent_selection
WHERE workspace_id = @workspace_id
  AND user_id = @user_id;

-- name: DeleteRecentAgentSelectionsByWorkspace :exec
DELETE FROM recent_agent_selection
WHERE workspace_id = @workspace_id;
