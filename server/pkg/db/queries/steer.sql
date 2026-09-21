-- name: CountSteerableTasksForComment :one
SELECT count(*)
FROM agent_task_queue t
JOIN agent_runtime r ON r.id = t.runtime_id
WHERE t.issue_id = @issue_id
  AND t.agent_id = @agent_id
  AND t.comment_thread_id IS NOT DISTINCT FROM comment_thread_root_id(@comment_id::uuid)
  AND t.status = 'running'
  AND (
      COALESCE(sqlc.narg('head_sha')::text, '') = ''
      OR t.context->>'head_sha' = sqlc.narg('head_sha')::text
  )
  AND r.provider IN ('codex', 'claude')
  AND COALESCE(r.metadata->'capabilities', '[]'::jsonb) ? 'task-steer-v1';

-- name: RegisterCommentSteer :one
WITH candidates AS MATERIALIZED (
    SELECT t.id, t.runtime_id
    FROM agent_task_queue t
    JOIN agent_runtime r ON r.id = t.runtime_id
    WHERE t.issue_id = @issue_id
      AND t.agent_id = @agent_id
      AND t.comment_thread_id IS NOT DISTINCT FROM comment_thread_root_id(@comment_id::uuid)
      AND t.status = 'running'
      AND (
          COALESCE(sqlc.narg('head_sha')::text, '') = ''
          OR t.context->>'head_sha' = sqlc.narg('head_sha')::text
      )
      AND r.provider IN ('codex', 'claude')
      AND COALESCE(r.metadata->'capabilities', '[]'::jsonb) ? 'task-steer-v1'
    FOR UPDATE OF t
), winner AS (
    SELECT id, runtime_id FROM candidates
    WHERE (SELECT count(*) FROM candidates) = 1
    LIMIT 1
), reserved AS (
    INSERT INTO comment_agent_delivery (comment_id, agent_id, task_id, runtime_id, status)
    SELECT @comment_id, @agent_id, id, runtime_id, 'pending' FROM winner
    ON CONFLICT (comment_id, agent_id) DO NOTHING
    RETURNING comment_id, agent_id, task_id, runtime_id, status, delivered_at
), attached AS (
    UPDATE agent_task_queue t
    SET coalesced_comment_ids = (
        SELECT COALESCE(array_agg(DISTINCT e), '{}')
        FROM unnest(array_append(t.coalesced_comment_ids, @comment_id::uuid)) AS e
        WHERE e IS NOT NULL
    )
    FROM reserved r
    WHERE t.id = r.task_id AND t.status = 'running'
    RETURNING t.id
)
SELECT r.comment_id, r.agent_id, r.task_id, r.runtime_id, r.status, r.delivered_at
FROM reserved r JOIN attached a ON a.id = r.task_id;

-- name: RecordCommentFollowUpDelivery :exec
-- Human comment recipients always get a durable per-agent receipt. This is
-- recorded after the existing enqueue/coalescing path succeeds when steering
-- is unsupported, ambiguous, or loses its registration race.
INSERT INTO comment_agent_delivery (
    comment_id, agent_id, task_id, runtime_id, status, failure_reason
)
VALUES (@comment_id, @agent_id, NULL, NULL, 'follow_up', @failure_reason)
ON CONFLICT (comment_id, agent_id) DO NOTHING;

-- name: GetCommentAgentDelivery :one
SELECT comment_id, agent_id, task_id, runtime_id, status, delivered_at
FROM comment_agent_delivery
WHERE comment_id = @comment_id AND agent_id = @agent_id;

-- name: ClaimNextCommentSteer :one
WITH next_delivery AS (
    SELECT d.comment_id, d.agent_id
    FROM comment_agent_delivery d
    JOIN comment c ON c.id = d.comment_id
    JOIN agent_task_queue t ON t.id = d.task_id
    WHERE d.task_id = @task_id
      AND d.status = 'pending'
      AND c.author_type = 'member'
      AND t.status = 'running'
    ORDER BY c.created_at, c.id
    FOR UPDATE OF d SKIP LOCKED
    LIMIT 1
), claimed AS (
    UPDATE comment_agent_delivery d
    SET status = 'steering', claimed_at = now(), updated_at = now()
    FROM next_delivery n
    WHERE d.comment_id = n.comment_id AND d.agent_id = n.agent_id
    RETURNING d.comment_id, d.agent_id, d.task_id
)
SELECT claimed.comment_id, claimed.agent_id, claimed.task_id, c.content,
       COALESCE(NULLIF(btrim(u.name), ''), 'a user')::text AS author_name
FROM claimed
JOIN comment c ON c.id = claimed.comment_id
LEFT JOIN "user" u ON u.id = c.author_id;

-- name: AckCommentSteerDelivered :one
WITH active_task AS (
    SELECT id FROM agent_task_queue
    WHERE id = @task_id AND status = 'running'
    FOR UPDATE
)
UPDATE comment_agent_delivery d
SET status = 'delivered', delivered_at = COALESCE(delivered_at, now()), updated_at = now(), failure_reason = NULL
FROM active_task t
WHERE d.task_id = @task_id
  AND d.comment_id = @comment_id
  AND d.status IN ('steering', 'delivered')
  AND t.id = d.task_id
RETURNING comment_id, agent_id, task_id, runtime_id, status, delivered_at;

-- name: MarkCommentSteerFollowUp :one
UPDATE comment_agent_delivery
SET status = 'follow_up', failure_reason = @failure_reason, updated_at = now()
WHERE task_id = @task_id
  AND comment_id = @comment_id
  AND status IN ('pending', 'steering')
RETURNING comment_id, agent_id, task_id, runtime_id, status, delivered_at;

-- name: GetCommentSteerDeliveryForTask :one
SELECT comment_id, agent_id, task_id, runtime_id, status, delivered_at
FROM comment_agent_delivery
WHERE task_id = @task_id AND comment_id = @comment_id;

-- name: FinalizeUndeliveredCommentSteers :many
UPDATE comment_agent_delivery
SET status = 'follow_up', failure_reason = 'turn_ended', updated_at = now()
WHERE task_id = @task_id AND status IN ('pending', 'steering')
RETURNING comment_id, agent_id;

-- name: ListDeliveredSteerCommentIDs :many
SELECT comment_id
FROM comment_agent_delivery
WHERE task_id = @task_id AND status = 'delivered';

-- name: ListCommentAgentDeliveries :many
SELECT d.comment_id, d.agent_id, a.name AS agent_name, d.status, d.delivered_at
FROM comment_agent_delivery d
JOIN agent a ON a.id = d.agent_id
WHERE d.comment_id = ANY(@comment_ids::uuid[])
ORDER BY d.created_at, d.agent_id;
