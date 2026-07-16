-- name: CreateChatSession :one
INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, is_agent_intro)
VALUES ($1, $2, $3, $4, (SELECT runtime_id FROM agent WHERE id = $2), $5)
RETURNING *;

-- name: GetChatSession :one
SELECT * FROM chat_session
WHERE id = $1;

-- name: GetChatSessionInWorkspace :one
SELECT * FROM chat_session
WHERE id = $1 AND workspace_id = $2;

-- name: ListChatSessionsByCreator :many
-- IM-style list: each active session with its unread *count* (assistant
-- messages after the read cursor), a preview of the latest message, and
-- ordered by most-recent activity so a new reply bumps a session to the top.
SELECT cs.*,
       (SELECT count(*) FROM chat_message m
          WHERE m.chat_session_id = cs.id
            AND m.role = 'assistant'
            AND m.created_at > cs.last_read_at)::int AS unread_count,
       COALESCE(lm.content, '') AS last_message_content,
       COALESCE(lm.role, '') AS last_message_role,
       lm.created_at AS last_message_at,
       lm.failure_reason AS last_message_failure_reason,
       COALESCE(lm.message_kind, '') AS last_message_kind
FROM chat_session cs
LEFT JOIN LATERAL (
  SELECT content, role, created_at, failure_reason, message_kind
    FROM chat_message m
   WHERE m.chat_session_id = cs.id
   ORDER BY m.created_at DESC
   LIMIT 1
) lm ON true
WHERE cs.workspace_id = $1 AND cs.creator_id = $2 AND cs.status = 'active'
ORDER BY (cs.pinned_at IS NOT NULL) DESC, cs.pinned_at DESC, COALESCE(lm.created_at, cs.updated_at) DESC;

-- name: ListAllChatSessionsByCreator :many
-- Unlike ListChatSessionsByCreator this returns archived sessions too (for the
-- "Archived" view), so unread must be forced to 0 for archived rows: archiving
-- deliberately does NOT advance last_read_at (so unarchive can restore the true
-- unread state), but an archived session is read-only and hidden from history,
-- so any residual unread is uncleanable and must not light up any badge. Gating
-- on status here is the single source of truth for all unread surfaces (FAB,
-- sidebar Chat tab, chat-window header) — see MUL-4360.
SELECT cs.*,
       CASE WHEN cs.status = 'archived' THEN 0
            ELSE (SELECT count(*) FROM chat_message m
                    WHERE m.chat_session_id = cs.id
                      AND m.role = 'assistant'
                      AND m.created_at > cs.last_read_at)
       END::int AS unread_count,
       COALESCE(lm.content, '') AS last_message_content,
       COALESCE(lm.role, '') AS last_message_role,
       lm.created_at AS last_message_at,
       lm.failure_reason AS last_message_failure_reason,
       COALESCE(lm.message_kind, '') AS last_message_kind
FROM chat_session cs
LEFT JOIN LATERAL (
  SELECT content, role, created_at, failure_reason, message_kind
    FROM chat_message m
   WHERE m.chat_session_id = cs.id
   ORDER BY m.created_at DESC
   LIMIT 1
) lm ON true
WHERE cs.workspace_id = $1 AND cs.creator_id = $2
ORDER BY (cs.pinned_at IS NOT NULL) DESC, cs.pinned_at DESC, COALESCE(lm.created_at, cs.updated_at) DESC;

-- name: UpdateChatSessionTitle :one
UPDATE chat_session SET title = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateChatSessionTitleIfCurrent :one
-- Compare-and-swap the title: only overwrite it when it still equals the
-- value the caller observed (@expected_title). This is the idempotency /
-- no-clobber guard behind LLM auto-titling (MUL-4295): the async generator
-- captures the session's current (default/original) title before calling the
-- model, and this write lands only if a manual rename or a competing writer
-- has not changed the title in the meantime. A mismatch returns pgx.ErrNoRows
-- (zero rows updated), which the caller treats as "someone renamed it — leave
-- it alone", NOT as an error.
UPDATE chat_session SET title = @new_title, updated_at = now()
WHERE id = @id AND title = @expected_title
RETURNING *;

-- name: SetChatSessionPinned :one
-- Pin/unpin a chat. Deliberately does NOT touch updated_at: pinning is a
-- list-ordering preference, not activity, so it must not bump the session's
-- last-activity sort key (which would make an unpinned chat jump the list).
-- pinned = true stamps pinned_at only when it was NULL, so re-pinning keeps
-- the original pin order; pinned = false clears it.
UPDATE chat_session
SET pinned_at = CASE WHEN @pinned::bool THEN COALESCE(pinned_at, now()) ELSE NULL END
WHERE id = $1
RETURNING *;

-- name: SetChatSessionArchived :one
-- Archive/unarchive a chat session by flipping status between 'active' and
-- 'archived'. Bumps updated_at so the row re-sorts on the receiving list. The
-- send-message path refuses archived sessions (see SendChatMessage), so the
-- conversation is effectively read-only until it is unarchived.
UPDATE chat_session
SET status = CASE WHEN @archived::bool THEN 'archived' ELSE 'active' END,
    updated_at = now()
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
-- land. workspace_id in the WHERE clause is a SQL-layer tenant guard; see
-- DeleteIssue.
DELETE FROM chat_session WHERE id = $1 AND workspace_id = $2;

-- name: TouchChatSession :exec
UPDATE chat_session SET updated_at = now()
WHERE id = $1;

-- name: CreateChatMessage :one
-- message_kind defaults to 'message' via COALESCE so every existing caller
-- (which omits it) keeps writing ordinary messages; the empty-reply path passes
-- 'no_response' to mark a visible turn with no text output (MUL-4351).
INSERT INTO chat_message (chat_session_id, role, content, task_id, failure_reason, elapsed_ms, message_kind)
VALUES ($1, $2, $3, sqlc.narg(task_id), sqlc.narg(failure_reason), sqlc.narg(elapsed_ms), COALESCE(sqlc.narg(message_kind)::text, 'message'))
RETURNING *;

-- name: LinkChatMessageToTask :exec
UPDATE chat_message
SET task_id = $2
WHERE id = $1 AND role = 'user';

-- name: DeleteUserChatMessageByTask :one
DELETE FROM chat_message
WHERE task_id = $1 AND role = 'user'
RETURNING *;

-- name: ListChatMessages :many
SELECT * FROM chat_message
WHERE chat_session_id = $1
ORDER BY created_at ASC;

-- name: ListChatInputMessages :many
-- Loads the immutable user-message input batch owned by a direct-chat task.
-- The caller passes the task's chat_input_task_id (itself for an original send,
-- the root task for an auto-retry child), so a claim reads exactly the messages
-- the user sent for this turn — and never absorbs a message that arrived after
-- the batch was sealed, no matter what the assistant wrote or when. Only used
-- for new task-owned direct-chat tasks; legacy/channel (chat_input_task_id
-- NULL) tasks keep using ListChatMessages + trailingUserMessages.
SELECT * FROM chat_message
WHERE task_id = $1 AND role = 'user'
ORDER BY created_at ASC, id ASC;

-- name: ListChatMessagesPage :many
SELECT * FROM chat_message
WHERE chat_session_id = $1
  AND (
    sqlc.narg('before_created_at')::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: GetChatMessage :one
SELECT * FROM chat_message
WHERE id = $1;

-- name: CreateChatTask :one
-- The chat sender (initiator) is a direct_human originator and accountable;
-- attribution provenance is stamped so this path is not a NULL-source enqueue
-- bypass (MUL-4302 §2).
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, chat_session_id,
    initiator_user_id, originator_user_id, accountable_user_id, force_fresh_session, runtime_mcp_overlay,
    runtime_connected_apps, originator_source, trigger_evidence_kind, trigger_evidence_ref_id
)
VALUES (
    $1, $2, NULL, 'queued', $3, $4, $5,
    sqlc.narg(originator_user_id),
    sqlc.narg(accountable_user_id),
    COALESCE(sqlc.narg('force_fresh_session')::boolean, FALSE),
    sqlc.narg(runtime_mcp_overlay),
    sqlc.narg(runtime_connected_apps),
    sqlc.narg(originator_source),
    sqlc.narg(trigger_evidence_kind),
    sqlc.narg(trigger_evidence_ref_id)
)
RETURNING *;

-- name: SetChatTaskInputOwnerSelf :one
-- Stamps a freshly-created direct-chat task as the owner of its own input batch
-- (chat_input_task_id = id), so a later claim loads exactly the user messages
-- tagged with this task id (ListChatInputMessages) rather than scanning trailing
-- history. Runs in the same transaction as CreateChatTask + the user message
-- insert on the direct-send path. Channel and legacy tasks skip this call and
-- keep chat_input_task_id NULL, so a rolling deploy never replays their history.
UPDATE agent_task_queue
SET chat_input_task_id = id
WHERE id = $1
RETURNING *;

-- name: GetLastChatTaskSession :one
-- Returns the most recent task in this chat session that managed to record a
-- session_id. Includes both completed and failed tasks: even a failed task
-- may have established a real agent session before failing, and we'd rather
-- resume there than start over and lose conversation memory. Used as a
-- fallback when chat_session.session_id is NULL. Resume-unsafe failures are
-- excluded because replaying those sessions deterministically reproduces the
-- same terminal state. Keep this list in sync with resumeUnsafeFailureReason
-- and GetLastTaskSession.
SELECT session_id, work_dir, runtime_id FROM agent_task_queue
WHERE chat_session_id = $1
  AND (
    status = 'completed'
    OR (
      status = 'failed'
      AND COALESCE(failure_reason, '') NOT IN ('iteration_limit', 'agent_fallback_message', 'api_invalid_request', 'codex_semantic_inactivity', 'agent_error.context_overflow')
      AND NOT (COALESCE(error, '') ILIKE '%400%' AND COALESCE(error, '') ILIKE '%invalid_request_error%')
    )
  )
  AND session_id IS NOT NULL
ORDER BY completed_at DESC
LIMIT 1;

-- name: GetPendingChatTask :one
-- Returns the most recent in-flight task for a chat session, if any.
-- Used by the frontend to recover pending state after refresh / reopen.
-- created_at is the anchor for the chat StatusPill timer (it computes
-- elapsed = now - task.created_at), so the pill survives refresh / reopen
-- without "resetting to 0s".
SELECT id, status, created_at FROM agent_task_queue
WHERE chat_session_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPendingChatTasksByCreator :many
-- Aggregate view of all in-flight chat tasks owned by a given creator in a
-- workspace. Drives the FAB's "running" indicator when the chat window is
-- closed and no single session's query is active.
--
-- Returns cs.agent_id so the handler can filter tasks belonging to private
-- agents the caller has lost access to using the already-loaded `allowed`
-- set — no second ListAllChatSessionsByCreator scan on the hot path.
--
-- atq.chat_session_id IS NOT NULL is redundant given the JOIN, but stated
-- explicitly so the planner can prove the query predicate is a subset of the
-- idx_agent_task_queue_chat_pending_v2 partial-index predicate and use it.
SELECT atq.id AS task_id, atq.status, atq.chat_session_id, cs.agent_id
FROM agent_task_queue atq
JOIN chat_session cs ON cs.id = atq.chat_session_id
WHERE atq.chat_session_id IS NOT NULL
  AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
  AND cs.workspace_id = $1
  AND cs.creator_id = $2
ORDER BY atq.created_at DESC;

-- name: HasPendingChatTasksByCreator :one
-- Boolean fast-path for the FAB's "running" indicator. Returns a single
-- EXISTS row instead of the full task list, so the planner can stop at the
-- first matching in-flight task (LIMIT 1 semantics via EXISTS).
--
-- Permission filtering is baked into the query: agent_id = ANY($3) restricts
-- the result to the agents the caller may currently see, so a member who lost
-- access to a private agent never gets a true from a task they can no longer
-- reach. The handler must pass its resolved accessible-agent id set as $3;
-- an empty array yields false.
SELECT EXISTS (
  SELECT 1
  FROM agent_task_queue atq
  JOIN chat_session cs ON cs.id = atq.chat_session_id
  WHERE atq.chat_session_id IS NOT NULL
    AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
    AND cs.workspace_id = sqlc.arg(workspace_id)
    AND cs.creator_id = sqlc.arg(creator_id)
    AND cs.agent_id = ANY(sqlc.arg(agent_ids)::uuid[])
) AS has_pending;

-- name: MarkChatSessionRead :exec
-- Advances the read cursor to now, dropping the session's unread_count to 0.
UPDATE chat_session SET last_read_at = now()
WHERE id = $1;

-- name: GetMostRecentUserChatMessage :one
-- Returns the most recent role='user' message in a session. Used by the
-- Lark `/issue` command parser: when the user types `/issue` with no
-- title, the spec falls back to "use the previous user message as the
-- title". Bot replies (role='assistant') are excluded — only human
-- input qualifies as a fallback title source.
SELECT * FROM chat_message
WHERE chat_session_id = $1 AND role = 'user'
ORDER BY created_at DESC
LIMIT 1;

-- name: ChatSessionHasUserMessage :one
-- Reports whether a session has any human (role='user') message yet. Used to
-- scope the is_agent_intro self-introduction prompt to the very first,
-- server-driven turn: an intro session starts with zero user messages, so the
-- opening run gets the "introduce yourself" prompt. Once the creator replies,
-- later turns in the same session must fall back to the normal reply prompt
-- instead of repeating the introduction every turn (MUL-4259).
SELECT EXISTS (
    SELECT 1 FROM chat_message
    WHERE chat_session_id = $1 AND role = 'user'
) AS has_user_message;

-- name: CreateChatDraftRestore :one
-- Persists the deferred-cancellation draft restore (#5219) in the same tx
-- that deletes the triggering user message: the chat:cancel_finalized
-- broadcast is best-effort, so an offline client recovers the draft from
-- this row instead. id is the deleted message's id.
INSERT INTO chat_draft_restore (id, chat_session_id, task_id, content, attachment_ids)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListChatDraftRestoresBySession :many
SELECT * FROM chat_draft_restore
WHERE chat_session_id = $1
ORDER BY created_at ASC;

-- name: DeleteChatDraftRestore :execrows
-- Idempotent consume: deleting an already-consumed restore matches no row.
DELETE FROM chat_draft_restore
WHERE id = $1 AND chat_session_id = $2;

-- name: DeleteChatDraftRestoresBySession :exec
-- chat_draft_restore carries no chat_session FK (MUL-3515), so DeleteChatSession
-- prunes its pending restores in the same tx that deletes the session.
DELETE FROM chat_draft_restore
WHERE chat_session_id = $1;

-- The chat_session row lock is the mutual-exclusion protocol between the
-- draft-restore writer (FinalizeDeferredCancelledChat) and every deleter of a
-- chat_session or one of its cascade parents. Without an FK, an INSERT into
-- chat_draft_restore takes no lock on its session, so a prune-then-delete would
-- otherwise miss a restore committed after its snapshot and strand it — with the
-- user's prompt in it — forever (#5219).
--
-- The contract, held by all five paths below:
--   deleter:   lock the sessions FOR UPDATE -> prune restores -> delete the parent
--   finalizer: lock the session FOR UPDATE  -> insert the restore
-- Whoever locks first wins: a finalizer that got there first commits its row
-- before the deleter's prune statement takes its snapshot, so the prune sweeps
-- it; a deleter that got there first leaves no session for the finalizer to
-- lock, so it never inserts.
--
-- Lock order is chat_session -> agent_task_queue everywhere (the finalizer locks
-- the session before claiming its task) so the deleters' cascade into
-- agent_task_queue cannot deadlock against it.
--
-- The single-session delete path needs no new query: it already holds
-- LockChatSessionForDelete (same FOR UPDATE row lock) across its prune.

-- name: LockChatSessionForTask :one
-- The finalizer's half of the protocol. No rows means the session is already
-- gone (its cascade NULLs agent_task_queue.chat_session_id), so there is nothing
-- to lock and nothing to restore into.
SELECT cs.id
FROM agent_task_queue t
JOIN chat_session cs ON cs.id = t.chat_session_id
WHERE t.id = $1
FOR UPDATE OF cs;

-- name: LockChatSessionsByWorkspace :many
-- ORDER BY id: a stable lock order keeps two concurrent deleters from
-- deadlocking against each other.
SELECT id FROM chat_session
WHERE workspace_id = $1
ORDER BY id
FOR UPDATE;

-- name: LockChatSessionsByArchivedRuntimeAgents :many
SELECT cs.id FROM chat_session cs
JOIN agent a ON a.id = cs.agent_id
WHERE a.runtime_id = $1 AND a.archived_at IS NOT NULL
ORDER BY cs.id
FOR UPDATE OF cs;

-- name: LockChatSessionsBySystemRuntimeAgents :many
SELECT cs.id FROM chat_session cs
JOIN agent a ON a.id = cs.agent_id
WHERE a.runtime_id = $1 AND a.kind = 'system'
ORDER BY cs.id
FOR UPDATE OF cs;

-- name: DeleteChatDraftRestoresByArchivedRuntimeAgents :exec
-- chat_session cascades from agent, so hard-deleting a runtime's archived agents
-- silently drops their sessions — and, without an FK, would strand the pending
-- restores (which still hold the user's prompt text) forever. Prune them in the
-- same tx, BEFORE the agent rows go: the join below needs them. Mirrors
-- DeleteChannelInstallationsByArchivedRuntimeAgents.
DELETE FROM chat_draft_restore
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs
    JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.archived_at IS NOT NULL
);

-- name: DeleteChatDraftRestoresBySystemRuntimeAgents :exec
-- Same cascade, for the system agents a runtime teardown also hard-deletes
-- (DeleteSystemAgentsByRuntime). Split from the archived-agent prune because the
-- runtime-profile teardown deletes only archived agents: pruning system-agent
-- sessions there would destroy restores whose session survives.
DELETE FROM chat_draft_restore
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs
    JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.kind = 'system'
);
