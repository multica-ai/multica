-- name: CreateTaskToken :one
INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at, id)
VALUES ($1, $2, $3, $4, $5, $6, COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()))
RETURNING *;

-- name: GetTaskTokenByHash :one
SELECT * FROM task_token
WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteTaskTokensByTask :exec
DELETE FROM task_token WHERE task_id = $1;

-- name: DeleteExpiredTaskTokens :exec
DELETE FROM task_token WHERE expires_at <= now();

-- name: GetTaskTokenChainRoot :one
-- Follow only same-human, same-workspace delegation edges. A broken edge,
-- cycle, or excessive depth leaves a hop label, which the signer refuses.
WITH RECURSIVE lineage AS (
    SELECT atq.id, atq.originator_user_id, atq.originator_source,
           atq.delegated_from_task_id, atq.autopilot_run_id, atq.issue_id, 0 AS depth
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE atq.id = sqlc.arg(task_id) AND a.workspace_id = sqlc.arg(workspace_id)

    UNION ALL

    SELECT parent.id, parent.originator_user_id, parent.originator_source,
           parent.delegated_from_task_id, parent.autopilot_run_id, parent.issue_id, child.depth + 1
    FROM lineage child
    JOIN agent_task_queue parent ON parent.id = child.delegated_from_task_id
    JOIN agent a ON a.id = parent.agent_id
    WHERE child.originator_source IN ('delegation', 'comment_source')
      AND parent.originator_user_id = child.originator_user_id
      AND a.workspace_id = sqlc.arg(workspace_id)
      AND child.depth < 32
)
SELECT id, originator_user_id, originator_source, autopilot_run_id, issue_id
FROM lineage ORDER BY depth DESC LIMIT 1;

-- name: GetTaskTokenAutopilotRunByIssue :one
-- Completed schedule roots still authorize their queued descendants. The
-- general dispatch lookup only returns active runs and cannot prove this.
SELECT * FROM autopilot_run WHERE issue_id = $1
ORDER BY triggered_at DESC, id DESC LIMIT 1;
