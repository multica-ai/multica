-- CEREBRO-PATCH(sqlc-chat): cerebro modification of upstream file
-- name: CreateChatSession :one
INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id)
VALUES ($1, $2, $3, $4, (SELECT runtime_id FROM agent WHERE id = $2))
RETURNING *;

-- name: GetChatSession :one
SELECT * FROM chat_session
WHERE id = $1;

-- name: GetChatSessionInWorkspace :one
SELECT * FROM chat_session
WHERE id = $1 AND workspace_id = $2;

-- name: ListChatSessionsByCreator :many
-- Returns active sessions with a boolean unread flag. Unread is strictly
-- per-session: either the user has uncleared assistant replies in this
-- session or they don't. Counting messages would be misleading.
SELECT cs.*,
       (cs.unread_since IS NOT NULL)::bool AS has_unread
FROM chat_session cs
WHERE cs.workspace_id = $1 AND cs.creator_id = $2 AND cs.status = 'active'
ORDER BY cs.updated_at DESC;

-- name: ListAllChatSessionsByCreator :many
SELECT cs.*,
       (cs.unread_since IS NOT NULL)::bool AS has_unread
FROM chat_session cs
WHERE cs.workspace_id = $1 AND cs.creator_id = $2
ORDER BY cs.updated_at DESC;

-- name: ListArchivedChatSessionsByCreator :many
-- Returns archived sessions with the unread flag, mirroring the active
-- variant. Backs the "show archived" view alongside the archived inbox feed.
SELECT cs.*,
       (cs.unread_since IS NOT NULL)::bool AS has_unread
FROM chat_session cs
WHERE cs.workspace_id = $1 AND cs.creator_id = $2 AND cs.status = 'archived'
ORDER BY cs.updated_at DESC;

-- name: UpdateChatSessionTitle :one
UPDATE chat_session SET title = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateChatSessionSession :exec
-- Updates the resume pointer for a chat session. Empty/NULL inputs are
-- ignored via COALESCE so a task that completes without a session_id (e.g.
-- the agent crashed before establishing one) cannot wipe out a previously
-- recorded resume pointer. This makes the chat memory robust against
-- intermittent agent failures.
UPDATE chat_session
SET session_id = COALESCE(sqlc.narg('session_id'), session_id),
    work_dir = COALESCE(sqlc.narg('work_dir'), work_dir),
    runtime_id = COALESCE(sqlc.narg('runtime_id'), runtime_id),
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: LockChatSessionForDelete :one
-- Acquires an exclusive (FOR UPDATE) row lock on chat_session(id). Used by
-- the delete path so that a concurrent SendChatMessage cannot enqueue a new
-- agent_task_queue row referencing this session between our cancel and
-- delete steps. The FK from agent_task_queue.chat_session_id takes a
-- KEY SHARE lock on the parent row during INSERT validation, which
-- conflicts with FOR UPDATE — concurrent inserts block here and then fail
-- their FK check after we commit the delete.
SELECT id FROM chat_session
WHERE id = $1
FOR UPDATE;

-- name: DeleteChatSession :exec
-- Hard delete. chat_message rows cascade via FK ON DELETE CASCADE; the
-- chat_session_id on agent_task_queue is set NULL by FK so completed/failed
-- task history survives the session being removed. Callers MUST run inside
-- the same transaction that holds LockChatSessionForDelete and that has
-- already cancelled any in-flight tasks (see CancelAgentTasksByChatSession)
-- so the daemon does not keep running work whose result has nowhere to
-- land.
DELETE FROM chat_session WHERE id = $1;

-- name: TouchChatSession :exec
UPDATE chat_session SET updated_at = now()
WHERE id = $1;

-- name: CreateChatMessage :one
INSERT INTO chat_message (chat_session_id, role, content, task_id, failure_reason, elapsed_ms)
VALUES ($1, $2, $3, sqlc.narg(task_id), sqlc.narg(failure_reason), sqlc.narg(elapsed_ms))
RETURNING *;

-- name: ListChatMessages :many
SELECT * FROM chat_message
WHERE chat_session_id = $1
ORDER BY created_at ASC;

-- name: ListUnrespondedUserMessages :many
-- Returns every user message in the session that has not yet been answered
-- by an assistant turn, in chronological order. The daemon claim path
-- bundles all of them into the next prompt so messages sent mid-stream
-- aren't dropped. Replaces the old time-based filter which lost messages
-- whose created_at was older than the prior turn's assistant reply (the
-- very window mid-stream sends land in).
SELECT cm.* FROM chat_message cm
WHERE cm.chat_session_id = $1
  AND cm.role = 'user'
  AND cm.responded_at IS NULL
ORDER BY cm.created_at ASC;

-- name: MarkUserMessagesRespondedBefore :exec
-- Marks every un-answered user message older than $cutoff as answered.
-- The cutoff is the claiming task's started_at: messages created before
-- the task started were the ones the daemon bundled into this task's
-- prompt. Messages created AFTER started_at were sent mid-stream and
-- are the responsibility of the next (queued) successor task — they
-- must NOT be marked here, otherwise the successor claims an empty
-- bundle and the agent answers "Tom besked".
--
-- Called inside the same transaction that inserts the assistant reply
-- (see CompleteTask + CancelTask in server/internal/service/task.go) so
-- the pair is atomic.
UPDATE chat_message
SET responded_at = now()
WHERE chat_session_id = $1
  AND role = 'user'
  AND responded_at IS NULL
  AND created_at < $2;

-- name: GetChatMessage :one
SELECT * FROM chat_message
WHERE id = $1;

-- name: CreateOrGetQueuedChatTask :one
-- Race-safe enqueue with coalescing. Inserts a queued chat task; if one is
-- already queued for this session, returns the existing row instead. Backed
-- by a partial unique index on (chat_session_id) WHERE status = 'queued'
-- (migration 057). The DO UPDATE branch is a no-op write — the assignment
-- to priority is what makes RETURNING surface the existing row instead of
-- producing zero rows.
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, chat_session_id)
VALUES ($1, $2, NULL, 'queued', $3, $4)
ON CONFLICT (chat_session_id) WHERE status = 'queued' AND chat_session_id IS NOT NULL
DO UPDATE SET priority = agent_task_queue.priority
RETURNING *;

-- name: GetLastChatTaskSession :one
-- Returns the most recent task in this chat session that managed to record a
-- session_id. Includes both completed and failed tasks: even a failed task
-- may have established a real agent session before failing, and we'd rather
-- resume there than start over and lose conversation memory. Used as a
-- fallback when chat_session.session_id is NULL.
SELECT session_id, work_dir, runtime_id FROM agent_task_queue
WHERE chat_session_id = $1
  AND status IN ('completed', 'failed')
  AND session_id IS NOT NULL
ORDER BY completed_at DESC
LIMIT 1;

-- name: GetPendingChatTask :one
-- Returns the in-flight task the UI should follow for this chat session.
-- Prefer the task that is actually mid-stream over a freshly-queued
-- successor: when the user sends msg2 while msg1's task is still 'running',
-- we want the frontend to keep watching task1's stream. Ordering by
-- created_at alone (newest first) made the queued successor win the moment
-- it was created — which flipped pendingTaskId mid-stream and hid the live
-- timeline. Within a single status bucket we still pick the oldest so
-- coalesced queued tasks return deterministically.
-- created_at is also the anchor for the chat StatusPill timer (it computes
-- elapsed = now - task.created_at), so the pill survives refresh / reopen
-- without "resetting to 0s".
SELECT id, status, created_at FROM agent_task_queue
WHERE chat_session_id = $1 AND status IN ('queued', 'dispatched', 'running')
ORDER BY
  CASE status
    WHEN 'running' THEN 0
    WHEN 'dispatched' THEN 1
    WHEN 'queued' THEN 2
  END,
  created_at ASC
LIMIT 1;

-- name: ListPendingChatTasksByCreator :many
-- Aggregate view of all in-flight chat tasks owned by a given creator in a
-- workspace. Drives the FAB's "running" indicator when the chat window is
-- closed and no single session's query is active.
SELECT atq.id AS task_id, atq.status, atq.chat_session_id
FROM agent_task_queue atq
JOIN chat_session cs ON cs.id = atq.chat_session_id
WHERE cs.workspace_id = $1
  AND cs.creator_id = $2
  AND atq.status IN ('queued', 'dispatched', 'running')
ORDER BY atq.created_at DESC;

-- name: MarkChatSessionRead :exec
-- Clears unread_since, dropping the session's unread count to 0.
UPDATE chat_session SET unread_since = NULL
WHERE id = $1;

-- name: SetUnreadSinceIfNull :exec
-- Atomically stamps the first unread assistant message's arrival time.
-- No-op if the session is already in "has unread" state — keeps the earliest
-- unread boundary stable across multiple incoming replies.
UPDATE chat_session SET unread_since = now()
WHERE id = $1 AND unread_since IS NULL;
