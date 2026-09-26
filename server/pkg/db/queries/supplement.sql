-- name: StartAgentTaskWithSupplement :one
-- Starting the task and recording the exact daemon/server capability handshake
-- are one state transition. A missing row is the fail-closed value for old
-- daemons, old servers, unsupported providers and application rollback.
WITH candidate AS MATERIALIZED (
    SELECT t.id, t.issue_id, r.workspace_id, r.provider
    FROM agent_task_queue t
    JOIN agent_runtime r ON r.id = t.runtime_id
    WHERE t.id = @task_id
      AND t.status IN ('dispatched', 'waiting_local_directory')
    FOR UPDATE OF t
), capability AS (
    INSERT INTO task_supplement_capability (task_id, workspace_id, issue_id, capability)
    SELECT id, workspace_id, issue_id, 'task-supplement-v1'
    FROM candidate
    WHERE @enable_task_supplement::boolean
      AND provider IN ('codex', 'claude', 'grok')
      AND issue_id IS NOT NULL
    ON CONFLICT DO NOTHING
    RETURNING task_id
)
UPDATE agent_task_queue t
SET status = 'running',
    started_at = now(),
    wait_reason = NULL,
    prepare_lease_expires_at = NULL
FROM candidate
WHERE t.id = candidate.id
  -- Reference the data-modifying CTE explicitly: capability persistence and
  -- the returned running row are one indivisible statement.
  AND (SELECT count(*) FROM capability) >= 0
RETURNING t.*;

-- name: CreateTaskSupplement :one
-- Locking the exact task serializes against terminal transitions.
-- Comment creation, explicit task binding and run coverage then
-- commit as one statement: a terminal-race loser creates nothing.
WITH locked_task AS MATERIALIZED (
    SELECT t.id, t.issue_id, t.agent_id, t.trigger_comment_id
    FROM agent_task_queue t
    JOIN agent_runtime r ON r.id = t.runtime_id
    JOIN task_supplement_capability cap ON cap.task_id = t.id
    WHERE t.id = @task_id
      AND t.issue_id = @issue_id
      AND r.workspace_id = @workspace_id
      AND t.status = 'running'
      AND cap.capability = 'task-supplement-v1'
      AND r.provider IN ('codex', 'claude', 'grok')
    FOR UPDATE OF t
), touched_issue AS (
    UPDATE issue i SET
        updated_at = now(),
        revision = revision + 1,
        last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now())
    FROM locked_task t
    WHERE i.id = t.issue_id AND i.workspace_id = @workspace_id
    RETURNING i.id, i.workspace_id, i.revision
), inserted_comment AS (
    INSERT INTO comment (
        issue_id, workspace_id, author_type, author_id, content, type, parent_id
    )
    SELECT i.id, i.workspace_id, 'member', @author_id, @content, 'comment', t.trigger_comment_id
    FROM touched_issue i
    JOIN locked_task t ON t.issue_id = i.id
    RETURNING *
), inserted_supplement AS (
    INSERT INTO task_supplement (
        task_id, workspace_id, issue_id, comment_id, author_id,
        client_request_id, status
    )
    SELECT t.id, i.workspace_id, i.id, c.id, @author_id,
           @client_request_id,
           'pending'
    FROM locked_task t
    JOIN touched_issue i ON i.id = t.issue_id
    JOIN inserted_comment c ON c.issue_id = i.id
    RETURNING *
)
SELECT c.*, i.revision AS issue_revision,
       s.task_id AS supplement_task_id, s.status AS supplement_status,
       s.failure_reason AS supplement_failure_reason,
       s.delivered_at AS supplement_delivered_at
FROM inserted_comment c
JOIN touched_issue i ON i.id = c.issue_id
JOIN inserted_supplement s ON s.comment_id = c.id;

-- name: GetTaskSupplementByRequest :one
SELECT s.*
FROM task_supplement s
WHERE s.task_id = @task_id
  AND s.workspace_id = @workspace_id
  AND s.author_id = @author_id
  AND s.client_request_id = @client_request_id;

-- name: BindCommentTaskSupplement :one
-- Binds an ordinary member comment, already created through the normal comment
-- path, to the exact running turn its author chose. Locking the task
-- serializes against terminal transitions: when that turn ended first nothing
-- is bound — never a later turn of the same agent — and the caller keeps the
-- comment's normal trigger instead.
WITH locked_task AS MATERIALIZED (
    SELECT t.id, t.issue_id, t.runtime_id
    FROM agent_task_queue t
    JOIN agent_runtime r ON r.id = t.runtime_id
    JOIN task_supplement_capability cap ON cap.task_id = t.id
    WHERE t.id = @task_id
      AND t.issue_id = @issue_id
      AND t.agent_id = @agent_id
      AND r.workspace_id = @workspace_id
      AND t.status = 'running'
      AND cap.capability = 'task-supplement-v1'
      AND r.provider IN ('codex', 'claude')
    FOR UPDATE OF t
), inserted AS (
    INSERT INTO task_supplement (
        task_id, workspace_id, issue_id, comment_id, author_id,
        client_request_id, status
    )
    SELECT t.id, @workspace_id, t.issue_id, @comment_id, @author_id,
           @client_request_id, 'pending'
    FROM locked_task t
    RETURNING *
)
SELECT inserted.*, locked_task.runtime_id
FROM inserted
JOIN locked_task ON locked_task.id = inserted.task_id;

-- name: GetTaskSupplementForRun :one
SELECT * FROM task_supplement
WHERE comment_id = @comment_id AND task_id = @task_id AND workspace_id = @workspace_id;

-- name: CommentHasTaskSupplement :one
SELECT EXISTS (
    SELECT 1 FROM task_supplement
    WHERE comment_id = @comment_id AND workspace_id = @workspace_id
) AS bound;

-- name: GetTaskSupplementCapability :one
SELECT * FROM task_supplement_capability WHERE task_id = @task_id;

-- name: ListTaskSupplementsByCommentIDs :many
-- One comment may steer several runs; receipts carry the run's agent so a
-- client can say whose turn received it.
SELECT s.*, t.agent_id
FROM task_supplement s
LEFT JOIN agent_task_queue t ON t.id = s.task_id
WHERE s.workspace_id = @workspace_id
  AND s.comment_id = ANY(@comment_ids::uuid[])
ORDER BY s.created_at, s.task_id;

-- name: ListTaskSupplementMetadata :many
SELECT cap.task_id, cap.capability,
       COALESCE(array_agg(s.comment_id ORDER BY s.created_at, s.comment_id)
                FILTER (WHERE s.comment_id IS NOT NULL), '{}'::uuid[])::uuid[] AS comment_ids
FROM task_supplement_capability cap
LEFT JOIN task_supplement s ON s.task_id = cap.task_id
WHERE cap.workspace_id = @workspace_id
  AND cap.task_id = ANY(@task_ids::uuid[])
GROUP BY cap.task_id, cap.capability;

-- name: SettleTerminalTaskSupplements :execrows
-- Application terminal transitions call this in the same transaction as the
-- agent_task_queue update. Re-checking the persisted task status makes an
-- accidental early call a no-op while preserving the task-row -> supplement
-- lock order used by creation and delivery acknowledgement.
UPDATE task_supplement AS supplement
SET status = 'failed',
    failure_reason = 'turn_ended',
    updated_at = now()
FROM agent_task_queue AS task
WHERE supplement.task_id = task.id
  AND task.id = ANY(@task_ids::uuid[])
  AND task.status IN ('completed', 'failed', 'cancelled')
  AND supplement.status IN ('pending', 'delivering');

-- name: ClaimNextTaskSupplement :one
WITH next AS MATERIALIZED (
    SELECT s.comment_id, s.task_id
    FROM task_supplement s
    JOIN agent_task_queue t ON t.id = s.task_id
    JOIN task_supplement_capability cap ON cap.task_id = t.id
    WHERE s.task_id = @task_id
      AND s.status = 'pending'
      AND t.status = 'running'
      AND cap.capability = 'task-supplement-v1'
    ORDER BY s.created_at, s.comment_id
    FOR UPDATE OF s SKIP LOCKED
    LIMIT 1
), claimed AS (
    UPDATE task_supplement s
    SET status = 'delivering',
        attempt_count = attempt_count + 1,
        failure_reason = NULL,
        updated_at = now()
    FROM next
    WHERE s.comment_id = next.comment_id
      AND s.task_id = next.task_id
    RETURNING s.*
)
SELECT claimed.comment_id, claimed.attempt_count, c.content,
       COALESCE(NULLIF(btrim(u.name), ''), 'a user')::text AS author_name
FROM claimed
JOIN comment c ON c.id = claimed.comment_id
LEFT JOIN "user" u ON u.id = claimed.author_id;

-- name: AckTaskSupplementDelivered :one
-- The provider can accept input before completion while its HTTP acknowledgement
-- arrives afterward. Serialize with settlement and preserve that successful write.
WITH task AS MATERIALIZED (
    SELECT id FROM agent_task_queue WHERE id = @task_id FOR UPDATE
)
UPDATE task_supplement s
SET status = 'delivered',
    delivered_at = COALESCE(delivered_at, now()),
    failure_reason = NULL,
    updated_at = now()
FROM task t
WHERE s.task_id = t.id
  AND s.comment_id = @comment_id
  AND (s.status IN ('delivering', 'delivered')
       OR (s.status = 'failed' AND s.failure_reason = 'turn_ended' AND s.attempt_count > 0))
RETURNING s.*;

-- name: AckTaskSupplementFailed :one
UPDATE task_supplement
SET status = 'failed',
    failure_reason = @failure_reason,
    updated_at = now()
WHERE task_id = @task_id
  AND comment_id = @comment_id
  AND status = 'delivering'
RETURNING *;

-- name: RetryTaskSupplement :one
WITH comment AS MATERIALIZED (
    SELECT id FROM comment
    WHERE id = @comment_id AND workspace_id = @workspace_id AND deleted_at IS NULL
    FOR UPDATE
), active_task AS MATERIALIZED (
    SELECT agent_task_queue.id FROM agent_task_queue
    WHERE agent_task_queue.id = @task_id
      AND agent_task_queue.issue_id = @issue_id
      AND agent_task_queue.status = 'running'
      AND EXISTS (SELECT 1 FROM comment)
      AND EXISTS (
          SELECT 1 FROM task_supplement_capability cap
          WHERE cap.task_id = agent_task_queue.id
            AND cap.capability = 'task-supplement-v1'
      )
    FOR UPDATE OF agent_task_queue
)
UPDATE task_supplement s
SET status = 'pending',
    failure_reason = NULL,
    updated_at = now()
FROM active_task t
WHERE s.task_id = t.id
  AND s.comment_id = @comment_id
  AND s.workspace_id = @workspace_id
  AND s.status = 'failed'
RETURNING s.*;

-- name: LockTaskSupplementsByComment :many
SELECT * FROM task_supplement
WHERE comment_id = @comment_id AND workspace_id = @workspace_id
FOR UPDATE;

-- name: DeleteTaskSupplementByComment :exec
DELETE FROM task_supplement
WHERE comment_id = @comment_id AND workspace_id = @workspace_id;
