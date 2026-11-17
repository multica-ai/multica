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
-- last run's status lets a client tell a report that failed from one that is
-- still running.
SELECT
    w.*,
    COALESCE(t.status, '')::text AS last_task_status
FROM chat_wakeup AS w
LEFT JOIN agent_task_queue AS t ON t.id = w.last_task_id
WHERE w.chat_session_id = @chat_session_id
  AND (w.status = 'active' OR w.ended_at > now() - interval '7 days')
ORDER BY (w.status = 'active') DESC, w.created_at DESC
LIMIT 50;

-- name: ListChatWakeupsForRequester :many
-- Every conversation's wakeups for one person in one workspace: the active
-- ones and those that ended in the last week. Archived conversations are
-- skipped; their wakeups ended when they were archived.
SELECT
    w.*,
    COALESCE(t.status, '')::text AS last_task_status,
    s.title AS chat_session_title
FROM chat_wakeup AS w
JOIN chat_session AS s ON s.id = w.chat_session_id
LEFT JOIN agent_task_queue AS t ON t.id = w.last_task_id
WHERE w.workspace_id = @workspace_id
  AND w.requester_id = @requester_id
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
SELECT status FROM agent_task_queue WHERE id = $1;
