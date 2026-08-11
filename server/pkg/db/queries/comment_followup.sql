-- name: UpsertCommentFollowupObligation :one
INSERT INTO agent_comment_followup_obligation (
    issue_id,
    agent_id,
    comment_id,
    comment_updated_at,
    head_sha
) VALUES (
    sqlc.arg(issue_id)::uuid,
    sqlc.arg(agent_id)::uuid,
    sqlc.arg(comment_id)::uuid,
    sqlc.arg(comment_updated_at)::timestamptz,
    sqlc.arg(head_sha)::text
)
ON CONFLICT (agent_id, comment_id) DO UPDATE
SET comment_updated_at = EXCLUDED.comment_updated_at,
    head_sha = EXCLUDED.head_sha,
    updated_at = now()
RETURNING *;

-- name: ListCommentFollowupObligations :many
SELECT *
FROM agent_comment_followup_obligation
WHERE sqlc.narg(after_updated_at)::timestamptz IS NULL
   OR (updated_at, id) > (
        sqlc.narg(after_updated_at)::timestamptz,
        sqlc.narg(after_id)::uuid
   )
ORDER BY updated_at ASC, id ASC
LIMIT sqlc.arg(scan_limit);

-- name: LockCommentForFollowup :one
SELECT *
FROM comment
WHERE id = sqlc.arg(comment_id)::uuid
FOR UPDATE;

-- name: LockCommentFollowupObligation :one
SELECT *
FROM agent_comment_followup_obligation
WHERE agent_id = sqlc.arg(agent_id)::uuid
  AND comment_id = sqlc.arg(comment_id)::uuid
FOR UPDATE;

-- name: CommentExists :one
SELECT EXISTS (
    SELECT 1
    FROM comment
    WHERE id = sqlc.arg(comment_id)::uuid
);

-- name: CommentFollowupCoveredByTask :one
SELECT EXISTS (
    SELECT 1
    FROM agent_task_queue
    WHERE issue_id = sqlc.arg(issue_id)::uuid
      AND agent_id = sqlc.arg(agent_id)::uuid
      AND (
          trigger_comment_id = sqlc.arg(comment_id)::uuid
          OR sqlc.arg(comment_id)::uuid = ANY(coalesced_comment_ids)
      )
);

-- name: RefreshCommentFollowupObligation :one
UPDATE agent_comment_followup_obligation
SET comment_updated_at = sqlc.arg(comment_updated_at)::timestamptz,
    head_sha = sqlc.arg(head_sha)::text,
    updated_at = now()
WHERE agent_id = sqlc.arg(agent_id)::uuid
  AND comment_id = sqlc.arg(comment_id)::uuid
RETURNING *;

-- name: DeleteCommentFollowupObligation :execrows
DELETE FROM agent_comment_followup_obligation
WHERE agent_id = sqlc.arg(agent_id)::uuid
  AND comment_id = sqlc.arg(comment_id)::uuid
  AND comment_updated_at = sqlc.arg(comment_updated_at)::timestamptz
  AND head_sha = sqlc.arg(head_sha)::text;

-- name: DeleteCommentFollowupObligationInvalid :execrows
DELETE FROM agent_comment_followup_obligation
WHERE agent_id = sqlc.arg(agent_id)::uuid
  AND comment_id = sqlc.arg(comment_id)::uuid;

-- name: LockMemberForCommentFollowup :one
SELECT *
FROM member
WHERE workspace_id = sqlc.arg(workspace_id)::uuid
  AND user_id = sqlc.arg(requester_user_id)::uuid
FOR UPDATE;

-- name: LockRuntimeForPoolCommentMerge :one
SELECT *
FROM agent_runtime
WHERE id = sqlc.arg(runtime_id)::uuid
FOR UPDATE;

-- name: LockChatSessionForCommentFollowup :one
SELECT *
FROM chat_session
WHERE id = sqlc.arg(chat_session_id)::uuid
FOR UPDATE;

-- name: LockAgentForCommentFollowup :one
SELECT *
FROM agent
WHERE id = sqlc.arg(agent_id)::uuid
FOR UPDATE;

-- name: LockPoolTaskForCommentMerge :one
SELECT *
FROM agent_task_queue
WHERE id = sqlc.arg(task_id)::uuid
FOR UPDATE NOWAIT;
