-- name: CreateChatWakeup :one
INSERT INTO chat_wakeup (
    id, workspace_id, chat_session_id, agent_id, requester_id,
    source_task_id, source_message_id, title, instruction, kind,
    condition, interval_seconds, fire_at, next_eval_at, expires_at
) VALUES (
    @id, @workspace_id, @chat_session_id, @agent_id, @requester_id,
    sqlc.narg(source_task_id), sqlc.narg(source_message_id), @title, @instruction, @kind,
    sqlc.narg(condition), sqlc.narg(interval_seconds), sqlc.narg(fire_at), @next_eval_at, @expires_at
)
RETURNING *;

-- name: GetChatWakeup :one
SELECT * FROM chat_wakeup WHERE id = $1;

-- name: GetChatWakeupInSession :one
SELECT * FROM chat_wakeup
WHERE id = @id AND chat_session_id = @chat_session_id;

-- name: LockChatWakeupInSession :one
SELECT * FROM chat_wakeup
WHERE id = @id AND chat_session_id = @chat_session_id
FOR UPDATE;

-- name: LockChatWakeup :one
SELECT * FROM chat_wakeup
WHERE id = $1
FOR UPDATE;

-- name: CountActiveChatWakeupsForSession :one
SELECT count(*) FROM chat_wakeup
WHERE chat_session_id = $1 AND status = 'active';

-- name: CountActiveChatWakeupsForRequester :one
SELECT count(*) FROM chat_wakeup
WHERE workspace_id = @workspace_id AND requester_id = @requester_id AND status = 'active';

-- name: ListChatWakeupsForSession :many
-- Active wakeups plus the ones that ended in the last week, newest first. The
-- last turn's status (after any auto-retry) lets a client tell a report that
-- is waiting, running, delivered or failed apart.
SELECT
    w.*,
    COALESCE(t.status, '')::text AS last_task_status
FROM chat_wakeup AS w
LEFT JOIN LATERAL (
    SELECT status FROM agent_task_queue
    WHERE chat_session_id = w.chat_session_id
      AND (id = w.last_task_id OR chat_input_task_id = w.last_task_id)
    ORDER BY created_at DESC
    LIMIT 1
) AS t ON w.last_task_id IS NOT NULL
WHERE w.chat_session_id = @chat_session_id
  AND (w.status = 'active' OR w.ended_at > now() - interval '7 days')
ORDER BY (w.status = 'active') DESC, w.created_at DESC
LIMIT 50;

-- name: ListChatWakeupsForSessionOwner :many
-- The wakeups of every conversation one person owns in one workspace: the
-- active ones and those that ended in the last week. Archived conversations
-- are skipped; their wakeups ended when they were archived.
SELECT
    w.*,
    COALESCE(t.status, '')::text AS last_task_status,
    s.title AS chat_session_title
FROM chat_wakeup AS w
JOIN chat_session AS s ON s.id = w.chat_session_id
LEFT JOIN LATERAL (
    SELECT status FROM agent_task_queue
    WHERE chat_session_id = w.chat_session_id
      AND (id = w.last_task_id OR chat_input_task_id = w.last_task_id)
    ORDER BY created_at DESC
    LIMIT 1
) AS t ON w.last_task_id IS NOT NULL
WHERE w.workspace_id = @workspace_id
  AND s.creator_id = @owner_id
  AND s.status = 'active'
  AND (w.status = 'active' OR w.ended_at > now() - interval '7 days')
ORDER BY (w.status = 'active') DESC, w.created_at DESC
LIMIT 100;

-- name: ListDueChatWakeupIDs :many
SELECT id FROM chat_wakeup
WHERE status = 'active' AND next_eval_at <= now()
ORDER BY next_eval_at ASC
LIMIT @row_limit;

-- name: SetChatWakeupEvaluation :exec
UPDATE chat_wakeup
SET condition_state = @condition_state,
    next_eval_at = @next_eval_at,
    last_error = sqlc.narg(last_error),
    updated_at = now()
WHERE id = @id;

-- name: DeferChatWakeup :exec
UPDATE chat_wakeup
SET next_eval_at = @next_eval_at,
    last_error = sqlc.narg(last_error),
    updated_at = now()
WHERE id = @id AND status = 'active';

-- name: RecordChatWakeupFire :one
-- One turn was started for this wakeup. A one-shot wakeup ends here; a check
-- stays active and is next evaluated at next_eval_at.
UPDATE chat_wakeup
SET last_task_id = @task_id,
    last_fired_at = now(),
    check_count = check_count + CASE WHEN kind = 'check' AND @end_status::text = '' THEN 1 ELSE 0 END,
    status = CASE WHEN @end_status::text = '' THEN status ELSE @end_status::text END,
    end_reason = CASE WHEN @end_status::text = '' THEN end_reason ELSE @end_reason::text END,
    ended_at = CASE WHEN @end_status::text = '' THEN ended_at ELSE now() END,
    next_eval_at = @next_eval_at,
    last_check_note = NULL,
    last_error = NULL,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: RecordChatWakeupRetry :one
-- The person re-ran an ended wakeup's failed turn. Status and end stay.
UPDATE chat_wakeup
SET last_task_id = @task_id,
    last_fired_at = now(),
    last_error = NULL,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: EndChatWakeup :one
UPDATE chat_wakeup
SET status = @status,
    end_reason = @end_reason,
    ended_at = now(),
    last_error = sqlc.narg(last_error),
    updated_at = now()
WHERE id = @id AND status = 'active'
RETURNING *;

-- name: UpdateChatWakeupConfig :one
UPDATE chat_wakeup
SET title = @title,
    instruction = @instruction,
    condition = sqlc.narg(condition),
    condition_state = @condition_state,
    interval_seconds = sqlc.narg(interval_seconds),
    fire_at = sqlc.narg(fire_at),
    next_eval_at = @next_eval_at,
    expires_at = @expires_at,
    last_error = NULL,
    updated_at = now()
WHERE id = @id AND status = 'active'
RETURNING *;

-- name: CheckInChatWakeup :one
UPDATE chat_wakeup
SET last_check_note = @note,
    last_checked_at = now(),
    last_checkin_task_id = @task_id,
    updated_at = now()
WHERE id = @id AND kind = 'check'
RETURNING *;

-- name: EndActiveChatWakeupsForSession :many
UPDATE chat_wakeup
SET status = 'cancelled',
    end_reason = @end_reason,
    ended_at = now(),
    updated_at = now()
WHERE chat_session_id = @chat_session_id AND status = 'active'
RETURNING *;

-- name: GetChatWakeupInputForTask :one
-- The hidden wakeup row a turn was started for, if any. $1 is the task's input
-- owner (chat_input_task_id), so an auto-retry finds its root's row.
SELECT * FROM chat_message
WHERE task_id = $1 AND role = 'user' AND message_kind = 'wakeup'
LIMIT 1;

-- name: DeleteChatWakeupInput :exec
DELETE FROM chat_message
WHERE id = $1 AND message_kind = 'wakeup';

-- name: GetChatWakeupTaskStatus :one
-- The status of a wakeup turn, following its auto-retries: they share the
-- turn's input owner, and the newest one is the turn's current state.
SELECT status FROM agent_task_queue
WHERE chat_session_id = @chat_session_id
  AND (id = @task_id OR chat_input_task_id = @task_id)
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteChatWakeupsBySession :exec
-- chat_wakeup has no chat_session FK; DeleteChatSession removes them in its
-- transaction.
DELETE FROM chat_wakeup WHERE chat_session_id = $1;

-- name: DeleteChatWakeup :exec
-- Drops a wakeup whose conversation no longer exists.
DELETE FROM chat_wakeup WHERE id = $1;

-- name: DeleteChatWakeupsBySystemRuntimeAgents :exec
-- System agents' sessions cascade away with the agents on runtime teardown;
-- their wakeups go first, while the join can still find them.
DELETE FROM chat_wakeup
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.kind = 'system'
);
