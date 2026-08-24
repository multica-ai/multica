-- name: CreateChatSession :one
INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, is_agent_intro, project_id, id)
VALUES ($1, $2, $3, $4, (SELECT runtime_id FROM agent WHERE id = $2), $5, sqlc.narg('project_id'), COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()))
RETURNING *;

-- name: ClearChatSessionProjectByProject :exec
-- Project references are intentionally soft (no database FK). Keep chat
-- history while removing the context selection when a project is deleted.
-- Do not touch updated_at: context cleanup is not chat activity.
UPDATE chat_session
SET project_id = NULL
WHERE project_id = $1 AND workspace_id = $2;

-- name: GetChatSession :one
SELECT * FROM chat_session
WHERE id = $1;

-- name: GetChatSessionInWorkspace :one
SELECT * FROM chat_session
WHERE id = $1 AND workspace_id = $2;

-- name: GetPublicChatSessionInWorkspace :one
-- A channel command is a durable control-plane record, not a public chat turn.
-- Channel-created sessions therefore become public only after they contain a
-- non-command message. Empty first-party sessions stay public so the member can
-- open a newly-created Web Chat and send its first message. channel_ingested is
-- the immutable fallback when a channel binding has since been removed.
SELECT cs.* FROM chat_session AS cs
WHERE cs.id = $1
  AND cs.workspace_id = $2
  AND (
    EXISTS (
      SELECT 1 FROM chat_message AS public_message
      WHERE public_message.chat_session_id = cs.id
        AND public_message.message_kind != 'channel_command'
    )
    OR (
      NOT EXISTS (
        SELECT 1 FROM channel_chat_session_binding AS binding
        WHERE binding.chat_session_id = cs.id
      )
      AND NOT EXISTS (
        SELECT 1 FROM chat_message AS channel_message
        WHERE channel_message.chat_session_id = cs.id
          AND channel_message.channel_ingested
      )
    )
  );

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
     AND m.message_kind != 'channel_command'
   ORDER BY m.created_at DESC
   LIMIT 1
) lm ON true
WHERE cs.workspace_id = $1 AND cs.creator_id = $2 AND cs.status = 'active'
  AND (
    lm.created_at IS NOT NULL
    OR (
      NOT EXISTS (
        SELECT 1 FROM channel_chat_session_binding AS binding
        WHERE binding.chat_session_id = cs.id
      )
      AND NOT EXISTS (
        SELECT 1 FROM chat_message AS channel_message
        WHERE channel_message.chat_session_id = cs.id
          AND channel_message.channel_ingested
      )
    )
  )
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
     AND m.message_kind != 'channel_command'
   ORDER BY m.created_at DESC
   LIMIT 1
) lm ON true
WHERE cs.workspace_id = $1 AND cs.creator_id = $2
  AND (
    lm.created_at IS NOT NULL
    OR (
      NOT EXISTS (
        SELECT 1 FROM channel_chat_session_binding AS binding
        WHERE binding.chat_session_id = cs.id
      )
      AND NOT EXISTS (
        SELECT 1 FROM chat_message AS channel_message
        WHERE channel_message.chat_session_id = cs.id
          AND channel_message.channel_ingested
      )
    )
  )
ORDER BY (cs.pinned_at IS NOT NULL) DESC, cs.pinned_at DESC, COALESCE(lm.created_at, cs.updated_at) DESC;

-- name: ListAgentBuilderSessionsByCreator :many
-- The caller's unfinished agent-creation conversations.
--
-- These never appear in ListChatSessionsByCreator: that list is filtered
-- against ListAllAgents, which is `kind = 'user'` only, so a builder session —
-- whose agent is the hidden `kind = 'system'` carrier — is invisible to every
-- chat surface by construction. This statement is the only way back to one,
-- which is why the studio may stop deleting them on navigation.
--
-- `a.runtime_id` is the whole point of the join. The carrier is what
-- SendDirectChatMessage reads to stamp a chat task's runtime, so it is the only
-- truthful answer to "where does this conversation run". Deliberately NOT
-- cs.runtime_id: that is the daemon's resume pointer, left stale on purpose
-- after a runtime switch (see RebindAgentBuilderRuntime), so resuming from it
-- would put the picker on a runtime that no longer executes anything — the
-- exact split MUL-5163 removed.
--
-- A conversation qualifies once it holds something the user would miss: a
-- message, or a saved configuration. Requiring a message alone was wrong — the
-- form on the right is editable from the moment the session exists and
-- autosaves, so someone can open the builder, type a name, and leave before the
-- first turn. That session has real work in it and was unreachable. Requiring
-- neither is also wrong: a session opened and abandoned untouched is not a
-- draft, and would put an empty row in front of the user on every accidental
-- entry into the flow.
--
-- The stored draft rides along instead of needing its own fetch: the studio
-- renders this list beside the conversation it is switching between, so the
-- configuration for the row the user picks has to be in hand at click time.
-- LEFT JOIN because a conversation that has only ever been driven by the AI has
-- no saved draft — the client replays the last <agent_draft> block in that case.
SELECT cs.id,
       cs.title,
       cs.created_at,
       cs.updated_at,
       a.runtime_id,
       COALESCE(lm.content, '') AS last_message_content,
       COALESCE(lm.role, '') AS last_message_role,
       lm.created_at AS last_message_at,
       d.draft AS stored_draft
FROM chat_session cs
JOIN agent a ON a.id = cs.agent_id
LEFT JOIN agent_builder_draft d ON d.chat_session_id = cs.id
LEFT JOIN LATERAL (
  SELECT content, role, created_at
    FROM chat_message m
   WHERE m.chat_session_id = cs.id
   ORDER BY m.created_at DESC
   LIMIT 1
) lm ON true
WHERE cs.workspace_id = $1
  AND cs.creator_id = $2
  AND cs.status = 'active'
  AND a.kind = 'system'
  AND a.system_key LIKE 'agent_builder:%'
  AND (lm.created_at IS NOT NULL OR d.chat_session_id IS NOT NULL)
-- A draft-only session has no message to sort by; fall back to when its
-- configuration was last written so it still lands in activity order.
ORDER BY COALESCE(lm.created_at, d.updated_at, cs.updated_at) DESC;

-- name: UpdateChatSessionTitle :one
UPDATE chat_session SET title = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateChatSessionProject :one
-- Project context is user-editable session metadata. Do not touch updated_at:
-- changing context is not conversation activity and must not reorder history.
UPDATE chat_session
SET project_id = sqlc.narg('project_id')
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
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

-- name: ClearChatSessionSessionIfMatches :exec
-- Drops the chat session's resume pointer, but only while it still points at
-- the exact session the caller proved unresumable.
--
-- The claim handler reads chat_session.session_id FIRST and only falls back to
-- GetLastChatTaskSession when it is empty, so a poisoned pointer here bypasses
-- every filter that query applies. Declining to OVERWRITE the pointer on a
-- resume-unsafe failure — which is all the fail path used to do — leaves the
-- dead session in place and the next turn resumes it (GH #6066).
--
-- The session_id + runtime_id predicate is what makes this safe to run in the
-- fail transaction: a concurrent turn that has already written a NEW pointer
-- does not match, so its healthy session survives instead of being cleared by
-- a slower sibling's failure. work_dir is deliberately left alone — the
-- directory is still reusable, only the conversation is not.
UPDATE chat_session
SET session_id = NULL,
    runtime_id = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND session_id = sqlc.arg('session_id')
  AND runtime_id = sqlc.arg('runtime_id');

-- name: AdvanceCancelledChatSessionPointer :exec
-- Moves a chat's resume pointer onto the session a CANCELLED task recorded
-- (GH #6340).
--
-- Cancellation is the one terminal state that never reports back: the daemon
-- discards its result and only sends a cancel-ack, so neither CompleteTask nor
-- FailTask — the only other writers of chat_session.session_id — ever runs. The
-- claim handler reads this pointer BEFORE falling back to
-- GetLastChatTaskSession, so on a chat that already has history a pointer left
-- on the previous turn shadows the cancelled turn's session no matter what the
-- fallback would have found.
--
-- Two callers, one statement, because both are races the other cannot cover:
-- the cancel path runs it inside the status-flip transaction (so no follow-up
-- can observe `cancelled` while the pointer still names the older session), and
-- the pin path runs it after a mid-flight pin lands on an already-cancelled row
-- (Codex waits for its rollout, so the pin routinely arrives after the cancel —
-- at which point the cancel path saw no session to publish).
--
-- Everything it decides on is read from the task row inside the statement, so
-- neither caller can act on a stale in-memory copy. The NOT EXISTS guard is what
-- makes the late pin safe: a NEWER task on this chat that already recorded a
-- session owns the pointer, and a straggler must not drag the conversation
-- backwards onto the turn the user interrupted.
UPDATE chat_session cs
SET session_id = t.session_id,
    runtime_id = t.runtime_id,
    work_dir   = COALESCE(t.work_dir, cs.work_dir),
    updated_at = now()
FROM agent_task_queue t
WHERE t.id = sqlc.arg('task_id')
  AND t.chat_session_id = cs.id
  AND t.status = 'cancelled'
  AND t.session_id IS NOT NULL
  AND t.runtime_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM agent_task_queue newer
      WHERE newer.chat_session_id = t.chat_session_id
        AND newer.id <> t.id
        AND newer.session_id IS NOT NULL
        AND newer.created_at > t.created_at
  );

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

-- name: LockChatSessionForRuntimeBind :one
-- Acquires an exclusive (FOR UPDATE) row lock on chat_session(id), serialising
-- "which runtime does this session execute on" against "enqueue the next task".
--
-- Both SendDirectChatMessage and the agent-builder runtime switch take this lock
-- for their whole transaction. Without it the two are a read-then-write race: a
-- send reads the carrier agent's runtime_id, the switch then passes its
-- pending-task check and rebinds the carrier, and the send finally inserts a task
-- still stamped with the pre-switch runtime — so the user is told the switch
-- succeeded while their message runs on the old runtime (MUL-5163).
--
-- The lock alone is not sufficient: the send path must also re-read the agent
-- INSIDE the locked transaction, because a send blocked at INSERT would otherwise
-- resume and write the runtime_id it read before blocking.
--
-- Same row and same lock mode as LockChatSessionForDelete, and both take it as
-- their first statement, so the delete path and this one cannot deadlock.
SELECT id FROM chat_session
WHERE id = $1
FOR UPDATE;

-- name: LockChatSessionForEnqueue :one
-- The chat-task enqueue's answer to archiving, and the one lock on this row
-- that a concurrent inbound message must NOT wait behind.
--
-- The channel run trigger is debounced, so the message is persisted the moment
-- it arrives and the task row is created a window later. An archive committing
-- inside that window cancels the tasks it can see — there are none yet — and
-- deletes the channel binding, and the flush then enqueues onto a conversation
-- the user closed. ClaimAgentTask does not read chat_session.status, so the
-- daemon runs it. Taking this lock as the enqueue transaction's first
-- statement and re-reading status under it makes both interleavings safe:
-- enqueue-then-archive is caught by the archive's cancel, archive-then-enqueue
-- is refused here.
--
-- FOR NO KEY UPDATE, not the FOR UPDATE the delete / runtime-bind / draft locks
-- take, and the difference is the point. Those three want to block concurrent
-- INSERTs that reference this row: FOR UPDATE conflicts with the FOR KEY SHARE
-- an FK insert takes on its parent, which is exactly how LockChatSessionForDelete
-- stops a send from slipping a task in between its cancel and its delete. This
-- lock wants the opposite. Every inbound channel message is an INSERT into
-- chat_message, FK'd to this same row, and the flush it eventually triggers
-- holds this lock for the whole enqueue — under FOR UPDATE the room's next
-- message would block behind the previous message's enqueue. FOR NO KEY UPDATE
-- does not conflict with FOR KEY SHARE, so appends keep flowing, while it still
-- conflicts with FOR UPDATE and with FOR NO KEY UPDATE — which is what
-- SetChatSessionArchived's UPDATE (status is not a key column) and the delete
-- path's lock take. So the archive and the delete still serialise against this
-- enqueue in both directions, which is all this guard needs.
--
-- Returns the whole row, not just the id, for the same reason
-- LockChatSessionForDraftWrite does: the caller must re-check status INSIDE the
-- transaction, because an enqueue blocked here resumes holding the row it read
-- before blocking — and the archive is what it was blocked on.
--
-- Same row and same position (first statement) as the other three, so the
-- repo-wide chat_session -> agent_task_queue order is unchanged and no new
-- deadlock edge is introduced.
SELECT * FROM chat_session
WHERE id = $1
FOR NO KEY UPDATE;

-- name: LockChatSessionForDraftWrite :one
-- The autosave half of the agent_builder_draft protocol, and the writer's
-- answer to LockChatSessionForDelete.
--
-- agent_builder_draft carries no chat_session FK (repo rule), so an INSERT into
-- it takes no lock on the parent row and nothing stops a draft from landing
-- after its conversation is gone: the save path read the session, the delete
-- transaction then committed, and the upsert still succeeded — leaving a row
-- the user explicitly discarded, invisible to the UI and unreachable by every
-- prune except the workspace teardown. The FK that would have rejected that
-- INSERT is the one we are not allowed to have, so the lock replaces it.
--
-- Returns the whole row, not just the id: the caller must re-check creator and
-- status INSIDE the transaction, because a save blocked here resumes holding
-- values it read before blocking (the same reason the runtime-bind path re-reads
-- the agent under its lock).
--
-- Same row and same lock mode as LockChatSessionForDelete and
-- LockChatSessionForRuntimeBind, taken as the transaction's first statement, so
-- no ordering against the repo-wide chat_session -> agent_task_queue sequence is
-- introduced and none of the three can deadlock against each other.
SELECT * FROM chat_session
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
-- message_kind and quick_actions default via COALESCE so every existing caller
-- (which omits it) keeps writing ordinary messages; the empty-reply path passes
-- 'no_response' to mark a visible turn with no text output (MUL-4351).
INSERT INTO chat_message (
    chat_session_id, role, content, task_id, failure_reason, elapsed_ms,
    message_kind, quick_actions, channel_media_pending_until, channel_ingested, id
)
VALUES (
    $1, $2, $3, sqlc.narg(task_id), sqlc.narg(failure_reason), sqlc.narg(elapsed_ms),
    COALESCE(sqlc.narg(message_kind)::text, 'message'),
    COALESCE(sqlc.narg(quick_actions)::jsonb, '[]'::jsonb),
    -- The media deadline is DB-clock time: every consumer compares it against
    -- SQL now() (GetChannelMediaPendingUntil, the deferred promote, the
    -- trailing-message guard), so the writer must use the same clock. The
    -- caller passes a relative budget in seconds; an application-clock
    -- timestamp here would let a skewed app node shrink or stretch the
    -- fallback window.
    CASE WHEN sqlc.narg(channel_media_pending_secs)::float8 IS NULL THEN NULL
         ELSE now() + make_interval(secs => sqlc.narg(channel_media_pending_secs)::float8) END,
    COALESCE(sqlc.narg(channel_ingested)::boolean, FALSE),
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())
)
RETURNING *;

-- name: TaskHasChannelIngestedMessages :one
-- Immutable channel provenance for a task's user-message input batch:
-- channel_ingested is stamped inside the channel append transaction and never
-- mutated afterwards, so it survives session archiving and installation
-- rebinds that delete the channel_chat_session_binding row. Callers pass the
-- batch OWNER id (chat_input_task_id, which auto-retry clones inherit), not
-- necessarily the task's own id. The cancel restore-delete and the
-- empty-completion silent-drop both gate on this — a channel sender has no
-- Multica composer for a restored draft, and the no_response fallback body
-- must never be pushed to an external channel.
SELECT EXISTS (
    SELECT 1 FROM chat_message
    WHERE task_id = $1
      AND role = 'user'
      AND channel_ingested
) AS channel_ingested;

-- name: GetChannelMediaPendingUntil :one
-- The latest unexpired media deadline gates a channel task. Using a durable
-- task fire_at means a process restart still produces the placeholder fallback.
-- Only a turn that can join a task's input batch may gate one: channel_command
-- turns are excluded from the seal below, so an unrelated later message would
-- otherwise wait out a command's media (or its whole fallback budget, since a
-- command whose create failed never runs the binder that clears the marker).
SELECT channel_media_pending_until
FROM chat_message
WHERE chat_session_id = $1
  AND role = 'user'
  AND message_kind != 'channel_command'
  AND channel_media_pending_until > now()
ORDER BY channel_media_pending_until DESC
LIMIT 1;

-- name: ClearChatMessageChannelMediaPending :exec
UPDATE chat_message
SET channel_media_pending_until = NULL
WHERE id = $1 AND chat_session_id = $2;

-- name: UpdateChatMessageContentForChannelMedia :execrows
-- Channel messages are immutable user turns. Media resolution may finish after
-- the initial append, so materialize stable inline attachment references in the
-- same transaction that binds those attachments. The provenance and role guards
-- prevent this narrow post-append path from rewriting ordinary web/agent rows.
UPDATE chat_message
SET content = sqlc.arg(content)
WHERE id = sqlc.arg(id)
  AND chat_session_id = sqlc.arg(chat_session_id)
  AND role = 'user'
  AND channel_ingested;

-- name: LinkChatMessageToTask :exec
UPDATE chat_message
SET task_id = $2
WHERE id = $1 AND role = 'user';

-- name: LinkUnownedChannelChatMessagesToTask :exec
-- Seals the trailing channel-message batch to its task. The task row and these
-- links are committed together, so an older in-flight task cannot absorb a
-- newer media message and a later assistant row cannot hide that message.
-- channel_command turns were already handled synchronously by Router. They stay
-- durable for channel orchestration but remain unowned and absent from public
-- Chat projections, preventing both immediate and delayed re-execution.
--
-- The boundary is "already answered", not "something newer exists". A reply can
-- only be a reply TO this message if the turn that wrote it started AFTER the
-- message arrived; a turn already running when the message landed had sealed
-- its own input batch before the message existed, so its reply — however late
-- it lands — answers an earlier batch and must not become a boundary. Without
-- that qualifier, a predecessor completing inside the debounce window (message
-- at 18:31:17.7, predecessor reply at 18:31:20.2, seal at 18:31:20.7) sealed
-- ZERO rows: the new task owned an empty input batch and the claim path
-- cancelled it, losing the user's message with no reply (GH #6591).
--
-- The reply's turn is compared through COALESCE(chat_input_task_id, id): an
-- auto-retry clone gets a fresh id while inheriting the root's batch, and it is
-- the ROOT's creation time that says which messages that batch could contain.
-- A non-user row with no task (or whose task row is gone) still counts as a
-- boundary — unattributable output is treated as possibly answering, so the
-- pre-ownership rows of a legacy session are never swept into a new batch.
UPDATE chat_message AS message
SET task_id = @task_id
WHERE message.chat_session_id = @chat_session_id
  AND message.role = 'user'
  AND message.task_id IS NULL
  AND message.message_kind != 'channel_command'
  AND NOT EXISTS (
      SELECT 1
      FROM chat_message AS prior
      LEFT JOIN agent_task_queue AS prior_turn
        ON prior_turn.id = prior.task_id
      LEFT JOIN agent_task_queue AS prior_batch
        ON prior_batch.id = COALESCE(prior_turn.chat_input_task_id, prior_turn.id)
      WHERE prior.chat_session_id = @chat_session_id
        AND prior.role != 'user'
        AND (prior.created_at, prior.id) > (message.created_at, message.id)
        AND (prior_batch.id IS NULL OR prior_batch.created_at > message.created_at)
  );

-- name: DeferChatTaskForSealedPendingMedia :one
-- Closes the enqueue-vs-append race: under READ COMMITTED a media message can
-- commit between GetChannelMediaPendingUntil and the batch seal above, landing
-- an unexpired media marker inside a task the deadline read decided was
-- 'queued'. Re-derive the deferral from the sealed batch itself, in the same
-- transaction, so a task is never claimable while its own input still has an
-- unexpired marker. No row (ErrNoRows) means no correction was needed.
UPDATE agent_task_queue AS task
SET status = 'deferred', fire_at = pending.max_until
FROM (
    SELECT max(message.channel_media_pending_until) AS max_until
    FROM chat_message AS message
    WHERE message.task_id = @task_id
      AND message.role = 'user'
      AND message.channel_media_pending_until > now()
) AS pending
WHERE task.id = @task_id
  AND pending.max_until IS NOT NULL
  AND (task.fire_at IS NULL OR task.fire_at < pending.max_until)
RETURNING task.*;

-- name: DeleteUserChatMessageByTask :one
-- Deletes the MEMBER-TYPED input of a cancelled/edited turn.
--
-- The kickoff exclusion is load-bearing since MUL-5827: an onboarding session's
-- first real turn owns two user rows — the member's message and the adopted
-- kickoff — so an unqualified delete would take the kickoff with it. That row
-- is the only copy of the onboarding context and of "you have already greeted
-- them", so losing it makes Mika introduce herself a second time, and the
-- RETURNING row would be an arbitrary one of the two: cancel could hand the
-- member the product's internal prompt as their restored draft, and silently
-- drop what they actually typed. Callers release the kickoff separately
-- (ReleaseOnboardingKickoffFromTask) so the next send re-adopts it.
DELETE FROM chat_message
WHERE task_id = $1
  AND role = 'user'
  AND message_kind <> 'onboarding_kickoff'
RETURNING *;

-- name: ReleaseOnboardingKickoffFromTask :exec
-- Hands an adopted kickoff on to the session's next un-started turn when its
-- own turn dies (terminal failure, cancel, edit), falling back to unowned when
-- there is no such turn.
--
-- Handing off rather than simply clearing to NULL is what closes the queued
-- successor case. Adoption happens inside a send's transaction, so a message
-- queued WHILE the kickoff's turn was still running found nothing to adopt and
-- never gets another chance: clearing to NULL would leave that already-sealed
-- turn to execute with no onboarding skill, no profile block, and no record
-- that Mika had already greeted the member — exactly the double-introduction
-- this design exists to prevent — while a later message could pick the kickoff
-- up instead, delivering the context to the wrong turn.
--
-- Target restrictions, each load-bearing:
--   * status = 'queued' only. A dispatched/running turn has already had its
--     prompt built from its input batch, so joining it now would consume the
--     kickoff without ever delivering it.
--   * chat_input_task_id = id selects roots that own their own input batch. A
--     retry child names its root instead, and the kickoff is already reachable
--     through that root, so a retry must not be re-targeted.
--   * regenerate_quick_actions_for IS NULL skips background suggestion passes,
--     which carry no user input and are invisible in the transcript.
--
-- Ordering matches the shared visible-head selector above ListChatMessages, so
-- the kickoff lands on whichever turn the member will actually see run next.
UPDATE chat_message
SET task_id = (
    SELECT successor.id
    FROM agent_task_queue AS successor
    WHERE successor.chat_session_id = chat_message.chat_session_id
      AND successor.status = 'queued'
      AND successor.chat_input_task_id = successor.id
      AND successor.regenerate_quick_actions_for IS NULL
      AND successor.id <> $1
    ORDER BY successor.priority DESC, successor.created_at ASC, successor.id ASC
    LIMIT 1
)
WHERE task_id = $1
  AND role = 'user'
  AND message_kind = 'onboarding_kickoff';

-- name: ListChatMessages :many
-- IMPORTANT: the visible-head selector below is also used by
-- ListChatMessagesForLegacyTask, ListChatMessagesPage,
-- ReanchorClaimedDirectChatInput, ReanchorNextQueuedDirectChatInput,
-- ListPendingChatTasksForSession, and CancelQueuedAgentTasksForSession in
-- agent.sql. Keep the eligible statuses and ordering identical: a claimed task
-- is current, a deferred retry precedes still-queued work, and queued peers use
-- claim priority/FIFO order. Background quick-action regeneration is invisible.
-- This is a presentation/visibility order, not a scheduling guarantee:
-- deferred rows are not claimable before promotion, so a queued row may be
-- claimed during the backoff and then becomes the visible claimed head.
SELECT message.* FROM chat_message AS message
WHERE message.chat_session_id = $1
  AND message.message_kind != 'channel_command'
  AND NOT (
    message.role = 'user'
    AND EXISTS (
      SELECT 1
      FROM agent_task_queue AS task
      WHERE task.chat_session_id = message.chat_session_id
        AND task.status = 'queued'
        AND task.id = message.task_id
        -- "Queued follow-up" is positional, not the row's transient status:
        -- the first pending task is the current turn even before claim.
        AND task.id <> (
          SELECT head.id
          FROM agent_task_queue AS head
          WHERE head.chat_session_id = $1
            AND head.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
            AND head.regenerate_quick_actions_for IS NULL
          ORDER BY
            CASE
              WHEN head.status IN ('dispatched', 'running', 'waiting_local_directory') THEN 0
              WHEN head.status = 'deferred' THEN 1
              ELSE 2
            END,
            head.priority DESC,
            head.created_at ASC,
            head.id ASC
          LIMIT 1
        )
    )
  )
ORDER BY message.created_at ASC, message.id ASC;

-- name: ListChatMessagesForLegacyTask :many
-- Legacy/reclaimed daemon tasks use trailing history, but must not absorb a
-- newer queued successor that is already bound to its own user message.
SELECT message.* FROM chat_message AS message
WHERE message.chat_session_id = $1
  AND NOT (
    message.role = 'user'
    AND EXISTS (
      SELECT 1
      FROM agent_task_queue AS task
      WHERE task.chat_session_id = message.chat_session_id
        AND task.status = 'queued'
        AND task.id = message.task_id
        AND task.id <> (
          SELECT head.id
          FROM agent_task_queue AS head
          WHERE head.chat_session_id = $1
            AND head.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
            AND head.regenerate_quick_actions_for IS NULL
          ORDER BY
            CASE
              WHEN head.status IN ('dispatched', 'running', 'waiting_local_directory') THEN 0
              WHEN head.status = 'deferred' THEN 1
              ELSE 2
            END,
            head.priority DESC,
            head.created_at ASC,
            head.id ASC
          LIMIT 1
        )
    )
  )
ORDER BY message.created_at ASC, message.id ASC;

-- name: ListChatInputMessages :many
-- Loads the immutable user-message input batch owned by a direct-chat task.
-- The caller passes the task's chat_input_task_id (itself for an original send,
-- the root task for an auto-retry child), so a claim reads exactly the messages
-- the user sent for this turn — and never absorbs a message that arrived after
-- the batch was sealed, no matter what the assistant wrote or when. Only used
-- for new task-owned direct-chat tasks; legacy/channel (chat_input_task_id
-- NULL) tasks keep using ListChatMessagesForLegacyTask + trailingUserMessages.
SELECT * FROM chat_message
WHERE task_id = $1 AND role = 'user'
ORDER BY created_at ASC, id ASC;

-- name: ReanchorClaimedDirectChatInput :exec
-- An idle direct send is visible while it is the positional queue head. A
-- older/pre-deploy follow-up whose enqueue timestamp puts it before a newer
-- settled reply can still reach claim; this is the fallback that moves only
-- that out-of-order user row to the new turn boundary. In-order visible heads
-- keep their original timestamp, so an idle send already returned to a cursor
-- client never moves merely because a daemon claimed it.
--
-- The task's own created_at remains immutable and continues to drive queue
-- FIFO, wait duration, and elapsed time. Only the message timestamp changes,
-- preserving the existing public ordering/cursor contract for older clients.
-- Add one microsecond when the DB clock ties the latest visible row so UUID
-- ordering can never put the new user turn before the previous assistant row.
--
-- channel_ingested excludes sealed Slack/Lark batches, which can own multiple
-- user rows and must preserve their original provider order. A direct send
-- owns exactly one non-channel user row today; if direct batching is added,
-- this query must assign a stable per-row offset instead of one timestamp.
-- Retry children are excluded by the caller because their chat_input_task_id
-- names the root task rather than themselves. Stale dispatched reclaim bypasses
-- this query, so re-delivery does not move an already-visible turn.
--
-- Lock order is task row first (ClaimAgentTask), then this non-FK message-only
-- update. The visibility subqueries take no row locks, avoiding an inverse
-- message -> task lock edge with send/completion transactions.
WITH latest_visible AS (
    SELECT
        claimed_input.id AS claimed_input_id,
        claimed_input.created_at AS input_created_at,
        prior.created_at AS prior_created_at
    FROM chat_message AS claimed_input
    CROSS JOIN LATERAL (
        SELECT prior.created_at
        FROM chat_message AS prior
        WHERE prior.chat_session_id = claimed_input.chat_session_id
          AND prior.id != claimed_input.id
          AND NOT (
            prior.role = 'user'
            AND EXISTS (
              SELECT 1
              FROM agent_task_queue AS queued_task
              WHERE queued_task.chat_session_id = prior.chat_session_id
                AND queued_task.status = 'queued'
                AND queued_task.id = prior.task_id
                AND queued_task.id <> (
                  SELECT head.id
                  FROM agent_task_queue AS head
                  WHERE head.chat_session_id = claimed_input.chat_session_id
                    AND head.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
                    AND head.regenerate_quick_actions_for IS NULL
                  ORDER BY
                    CASE
                      WHEN head.status IN ('dispatched', 'running', 'waiting_local_directory') THEN 0
                      WHEN head.status = 'deferred' THEN 1
                      ELSE 2
                    END,
                    head.priority DESC,
                    head.created_at ASC,
                    head.id ASC
                  LIMIT 1
                )
            )
          )
        ORDER BY prior.created_at DESC, prior.id DESC
        LIMIT 1
    ) AS prior
    WHERE claimed_input.task_id = @task_id
      AND claimed_input.role = 'user'
      AND NOT claimed_input.channel_ingested
      -- The adopted onboarding kickoff is never a visible row, so it has no
      -- turn boundary to correct — and reanchoring it would actively break the
      -- one thing its position controls. It is deliberately older than the
      -- member's message so the runtime reads "context, then their words";
      -- moving it to dispatch time reverses that, and because the batch's two
      -- rows would then share one timestamp, their order falls to random UUIDs
      -- (MUL-5827).
      AND claimed_input.message_kind <> 'onboarding_kickoff'
)
UPDATE chat_message AS claimed_input
SET created_at = GREATEST(
    @dispatched_at::timestamptz,
    latest_visible.prior_created_at + interval '1 microsecond'
)
FROM latest_visible
WHERE claimed_input.id = latest_visible.claimed_input_id
  AND latest_visible.prior_created_at >= latest_visible.input_created_at;

-- name: ReanchorNextQueuedDirectChatInput :exec
-- An assistant outcome can make the next queued direct task the positional
-- head before a daemon claims it (MUL-5750). Its user row was persisted before
-- this reply, so move that still-hidden single-row input just after the reply.
-- Callers run this immediately after CreateChatMessage in the same transaction:
-- readers see either the old active head without the reply, or the settled
-- reply followed by the newly-visible head — never user B before assistant A.
-- Channel batches are excluded because their immutable provider ordering may
-- contain multiple user rows. Direct chat currently owns exactly one row; if
-- direct batching is added, assign stable per-row offsets here.
UPDATE chat_message AS queued_input
SET created_at = sqlc.arg('assistant_created_at')::timestamptz + interval '1 microsecond'
WHERE queued_input.chat_session_id = $1
  AND queued_input.role = 'user'
  AND NOT queued_input.channel_ingested
  -- Same exclusion, same reason as ReanchorClaimedDirectChatInput: the hidden
  -- kickoff has no visible position to fix, and moving it would put the
  -- product's context after the member's message inside one input batch.
  AND queued_input.message_kind <> 'onboarding_kickoff'
  AND queued_input.created_at <= sqlc.arg('assistant_created_at')::timestamptz
  AND EXISTS (
    SELECT 1
    FROM agent_task_queue AS queued_task
    WHERE queued_task.id = queued_input.task_id
      AND queued_task.chat_session_id = queued_input.chat_session_id
      AND queued_task.status = 'queued'
      AND queued_task.chat_input_task_id = queued_task.id
      AND queued_task.regenerate_quick_actions_for IS NULL
      AND queued_task.id = (
        SELECT head.id
        FROM agent_task_queue AS head
        WHERE head.chat_session_id = queued_input.chat_session_id
          AND head.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
          AND head.regenerate_quick_actions_for IS NULL
        ORDER BY
          CASE
            WHEN head.status IN ('dispatched', 'running', 'waiting_local_directory') THEN 0
            WHEN head.status = 'deferred' THEN 1
            ELSE 2
          END,
          head.priority DESC,
          head.created_at ASC,
          head.id ASC
        LIMIT 1
      )
  );

-- name: ListChatMessagesPage :many
SELECT message.* FROM chat_message AS message
WHERE message.chat_session_id = $1
  AND message.message_kind != 'channel_command'
  AND NOT (
    message.role = 'user'
    AND EXISTS (
      SELECT 1
      FROM agent_task_queue AS task
      WHERE task.chat_session_id = message.chat_session_id
        AND task.status = 'queued'
        AND task.id = message.task_id
        AND task.id <> (
          SELECT head.id
          FROM agent_task_queue AS head
          WHERE head.chat_session_id = $1
            AND head.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
            AND head.regenerate_quick_actions_for IS NULL
          ORDER BY
            CASE
              WHEN head.status IN ('dispatched', 'running', 'waiting_local_directory') THEN 0
              WHEN head.status = 'deferred' THEN 1
              ELSE 2
            END,
            head.priority DESC,
            head.created_at ASC,
            head.id ASC
          LIMIT 1
        )
    )
  )
  AND (
    sqlc.narg('before_created_at')::timestamptz IS NULL
    OR (message.created_at, message.id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid)
  )
ORDER BY message.created_at DESC, message.id DESC
LIMIT $2;

-- name: GetChatMessage :one
SELECT * FROM chat_message
WHERE id = $1;

-- name: CreateChatTask :one
-- Fenced against workspace teardown: lock_task_owner_rows (migration 284)
-- locks the owners' workspace rows in the writer's own transaction and returns
-- false once they are gone, so this statement writes no row instead of stranding
-- a task in a workspace that has just been deleted (MUL-5999).
-- The chat sender (initiator) is a direct_human originator and accountable;
-- attribution provenance is stamped so this path is not a NULL-source enqueue
-- bypass (MUL-4302 §2).
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, chat_session_id,
    initiator_user_id, originator_user_id, accountable_user_id, force_fresh_session, runtime_mcp_overlay,
    runtime_connected_apps, originator_source, trigger_evidence_kind, trigger_evidence_ref_id,
    fire_at, id
)
SELECT
    $1, $2, NULL,
    CASE WHEN sqlc.narg('fire_at')::timestamptz IS NULL THEN 'queued' ELSE 'deferred' END,
    $3, $4, $5,
    sqlc.narg(originator_user_id),
    sqlc.narg(accountable_user_id),
    COALESCE(sqlc.narg('force_fresh_session')::boolean, FALSE),
    sqlc.narg(runtime_mcp_overlay),
    sqlc.narg(runtime_connected_apps),
    sqlc.narg(originator_source),
    sqlc.narg(trigger_evidence_kind),
    sqlc.narg(trigger_evidence_ref_id),
    sqlc.narg('fire_at')::timestamptz,
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())
WHERE lock_task_owner_rows($1, NULL, $2)
RETURNING *;

-- name: PromoteChannelChatTasksIfMediaReady :many
-- Media completion may race with the 3s run batcher. Promote every original
-- channel task waiting for this session only after all unexpired media markers
-- are gone; retry/escalation/direct-chat deferred tasks are excluded. The
-- marker scan uses the same population as GetChannelMediaPendingUntil and the
-- seal: a channel_command turn belongs to no batch, so it must not hold a
-- deferred task back either.
UPDATE agent_task_queue AS task
SET status = 'queued', fire_at = NULL
WHERE task.chat_session_id = @chat_session_id
  AND task.status = 'deferred'
  AND task.issue_id IS NULL
  AND task.parent_task_id IS NULL
  AND task.escalation_for_task_id IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM chat_message AS message
      WHERE message.chat_session_id = @chat_session_id
        AND message.role = 'user'
        AND message.message_kind != 'channel_command'
        AND message.channel_media_pending_until > now()
  )
RETURNING task.*;

-- name: SetChatTaskInputOwnerSelf :one
-- Stamps a freshly-created direct-chat task as the owner of its own input batch
-- (chat_input_task_id = id), so a later claim loads exactly the user messages
-- tagged with this task id (ListChatInputMessages) rather than scanning trailing
-- history. Runs in the same transaction as CreateChatTask + message ownership:
-- direct-send inserts one owned message, while channel enqueue seals its
-- trailing unowned batch. Legacy tasks keep chat_input_task_id NULL and retain
-- the trailing-history fallback during rolling deploys.
UPDATE agent_task_queue
SET chat_input_task_id = id
WHERE id = $1
RETURNING *;

-- name: GetLastChatTaskSession :one
-- Returns the most recent task in this chat session that managed to record a
-- session_id. Includes completed, failed AND cancelled tasks: each of them may
-- have established a real agent session, and we'd rather resume there than
-- start over and lose conversation memory. Used as a fallback when
-- chat_session.session_id is NULL. Resume-unsafe failures are excluded because
-- replaying those sessions deterministically reproduces the same terminal
-- state. Keep this list in sync with resumeUnsafeFailureReason and
-- GetLastTaskSession.
--
-- The plan depends on idx_agent_task_queue_chat_terminal_resume for the
-- terminal/cutoff scans and idx_agent_task_queue_chat_retired_session for the
-- retired set. Keep their partial predicates broad enough for every CTE here,
-- especially the NULL-session resume_overflow_at rows.
--
-- 'cancelled' is resumable and its absence was GH #6340: the user stops a turn
-- the agent had already started answering, and the next message starts from
-- nothing. A cancelled row only carries a session_id because the daemon pinned
-- one mid-flight (UpdateAgentTaskSession), which means the provider really did
-- emit that session — either the resume loaded or it opened a fresh one. The
-- user interrupted it; the provider did not reject it, so it is no more
-- suspect than a completed one. Cancellation records no failure_reason/error,
-- so the poison filters below have nothing to match and cancelled rows pass
-- them the way completed rows do. The remaining risk — a transcript killed
-- mid-tool-call that the provider later refuses — is caught downstream by
-- taskfailure.UnresumableHistory and retires the session on the next turn.
--
-- The regex pair mirrors GetLastTaskSession's provider-agnostic guard for an
-- empty message baked into the conversation history: both must match, and
-- both track emptyContentRe / historyMessageLocatorRe in
-- pkg/taskfailure/resume.go (GH #6066).
--
-- Selection is per-session, not per-row, and retired sessions are excluded —
-- both mirroring GetLastTaskSession, which this query had drifted away from.
-- A plain row-level filter reopens the poisoning wormhole GH #5975 closed on
-- the issue side: it drops the newest poisoned row for a session and then
-- happily falls back to an OLDER completed row carrying the same dead
-- session_id. Judging each session by its LATEST terminal state means a newer
-- poisoned row invalidates the whole session, while a genuinely different
-- healthy session stays eligible.
WITH retired_sessions AS (
    SELECT DISTINCT r.retired_session_id AS session_id
    FROM agent_task_queue r
    WHERE r.chat_session_id = $1
      AND r.retired_session_id IS NOT NULL
), resume_overflow_at AS (
    -- completed_at alone, where the issue-side twin coalesces four columns:
    -- this query already selects and orders by bare completed_at throughout,
    -- so the cutoff has to be measured on the same clock as the values it is
    -- compared against. Change both halves together if that ever moves.
    SELECT MAX(t.completed_at) AS at
    FROM agent_task_queue t
    WHERE t.chat_session_id = $1
      AND t.status = 'failed'
      AND (
        COALESCE(t.failure_reason, '') = 'codex_resume_oversized'
        OR (COALESCE(t.error, '') ILIKE '%thread/resume failed%' AND COALESCE(t.error, '') ILIKE '%token too long%')
      )
), latest_per_session AS (
    SELECT DISTINCT ON (t.session_id)
        t.session_id, t.work_dir, t.runtime_id, t.status, t.failure_reason, t.error, t.completed_at
    FROM agent_task_queue t
    WHERE t.chat_session_id = $1
      AND t.session_id IS NOT NULL
      AND t.status IN ('completed', 'failed', 'cancelled')
    ORDER BY t.session_id, t.completed_at DESC
)
SELECT session_id, work_dir, runtime_id FROM latest_per_session
WHERE session_id NOT IN (SELECT session_id FROM retired_sessions)
  AND (
    status IN ('completed', 'cancelled')
    OR (
      status = 'failed'
      AND COALESCE(failure_reason, '') NOT IN ('iteration_limit', 'agent_fallback_message', 'api_invalid_request', 'codex_semantic_inactivity', 'agent_error.context_overflow', 'codex_resume_oversized')
      AND NOT (COALESCE(error, '') ILIKE '%400%' AND COALESCE(error, '') ILIKE '%invalid_request_error%')
      -- Mirrors the GetLastTaskSession auth-resolution guard: a provider that
      -- cannot resolve its auth method fails deterministically on resume, and
      -- the classification is agent_error.unknown (resume-safe), so only this
      -- text guard keeps the dead session from being replayed. This and
      -- GetLastTaskSession must move together.
      -- Keep in sync with ResumeUnsafeFailure and GetLastTaskSession.
      -- The phrase itself lives in taskfailure.AuthMethodUnresolved, which the
      -- daemon's in-turn fresh-session retry reads (GH #6777). This guard stays
      -- because it is the only protection for rows an older daemon wrote.
      AND NOT (COALESCE(error, '') ILIKE '%could not resolve authentication method%')
      AND NOT (COALESCE(error, '') ~* 'must not be empty|must be non-?empty|must have non-?empty|non-?empty content|cannot be empty|should not be empty'
               AND COALESCE(error, '') ~* 'role[^a-z0-9]{0,2}assistant|assistant message|message at position|messages\.[0-9]|messages\[[0-9]')
    )
  )
  -- MUL-5722, mirroring GetLastTaskSession: an overflowed resume records no
  -- session, so exclude by time instead of by matching the failed row. Note
  -- this only guards the FALLBACK — the claim handler reads
  -- chat_session.session_id first, so a pointer still naming the oversized
  -- thread has to be cleared at fail time (see FailTask) to be covered.
  AND (
    (SELECT at FROM resume_overflow_at) IS NULL
    OR completed_at > (SELECT at FROM resume_overflow_at)
  )
ORDER BY completed_at DESC
LIMIT 1;

-- name: HasActiveChatTaskForSession :one
-- True while ANY task — a normal user turn OR a background quick-actions
-- regenerate — is in flight for the session (contrast GetPendingChatTask, which
-- hides regenerate passes from the UI). A quick-actions refresh is refused when
-- this is true: a running turn is about to change the latest reply, so the
-- target we'd resume is already stale even before its assistant row lands; and a
-- second concurrent regenerate would double-spend quota on the same turn. Read
-- inside the same session lock as the enqueue so it cannot race a sibling insert
-- (MUL-5149 review §1/§2).
--
-- 'deferred' is included: an auto-retry armed with a backoff fire_at is inserted
-- deferred (CreateRetryTask), and provider_network's final chat attempt waits
-- ~5s that way. During that window the failed turn has written no assistant row,
-- so the latest-persisted check still points at the OLD turn — omitting deferred
-- would let a refresh resume a session the retry is about to advance and attach
-- the new turn's suggestions to the old one (MUL-5149 re-review §1).
SELECT EXISTS (
  SELECT 1 FROM agent_task_queue
  WHERE chat_session_id = $1
    AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
) AS has_active;

-- name: HasPendingChatTurnForSession :one
-- Position-only check for a direct send. Unlike GetPendingChatTask this is an
-- EXISTS query and includes deferred retries: a new user turn must remain a
-- follow-up while an older retry waits for its backoff, otherwise promotion of
-- that retry would make the new message disappear from the visible transcript.
-- Background quick-action regeneration owns no visible turn and is excluded.
SELECT EXISTS (
  SELECT 1 FROM agent_task_queue
  WHERE chat_session_id = $1
    AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
    AND regenerate_quick_actions_for IS NULL
) AS has_pending;

-- name: GetPendingChatTask :one
-- Returns the most recent in-flight task for a chat session, if any.
-- Used by the frontend to recover pending state after refresh / reopen.
-- created_at is the anchor for the chat StatusPill timer (it computes
-- elapsed = now - task.created_at), so the pill survives refresh / reopen
-- without "resetting to 0s".
SELECT id, status, created_at FROM agent_task_queue
WHERE chat_session_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
  -- Background quick-actions regeneration passes are invisible to the chat UI:
  -- they own no assistant turn and must not raise the StatusPill or disable the
  -- composer (MUL-5149 refresh follow-up).
  AND regenerate_quick_actions_for IS NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPendingChatTasksForSession :many
-- Returns a claimed task first, then a deferred retry, followed by prioritized
-- then FIFO queued work. See the shared visible-head invariant above
-- ListChatMessages; changing this order requires changing every selector named
-- there in the same patch.
-- The message lateral join reads only the immutable input owned by each task;
-- it avoids loading the session's complete message history just to render a
-- one-line queue preview. GetPendingChatTask remains for legacy callers that
-- only need an existence check.
SELECT
    task.id,
    task.status,
    task.created_at,
    -- Only meaningful while status is waiting_local_directory: the daemon
    -- stamps which directory this task is parked on and who holds it, so the
    -- StatusPill can say why it is not moving. Stale on any other status; the
    -- renderer reads it only for the waiting one.
    task.wait_reason,
    message.id AS message_id,
    COALESCE(message.content, '')::text AS content
FROM agent_task_queue AS task
LEFT JOIN LATERAL (
    SELECT input.id, input.content
    FROM chat_message AS input
    WHERE input.task_id = COALESCE(task.chat_input_task_id, task.id)
      AND input.role = 'user'
    ORDER BY input.created_at ASC, input.id ASC
    LIMIT 1
) AS message ON TRUE
WHERE task.chat_session_id = $1
  AND task.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  AND task.regenerate_quick_actions_for IS NULL
ORDER BY
    CASE
      WHEN task.status IN ('dispatched', 'running', 'waiting_local_directory') THEN 0
      WHEN task.status = 'deferred' THEN 1
      ELSE 2
    END,
    task.priority DESC,
    task.created_at ASC,
    task.id ASC;

-- name: PrioritizeQueuedChatTask :one
WITH target AS MATERIALIZED (
  SELECT candidate.id
  FROM agent_task_queue AS candidate
  WHERE candidate.id = sqlc.arg('id')
    AND candidate.chat_session_id = sqlc.arg('chat_session_id')
    AND candidate.status = 'queued'
    -- "Send now" is valid only while there is a visible claimed task for the
    -- client to cancel. If the visible head is still queued (or deferred), the
    -- selected row would otherwise replace it without any active_task_id.
    AND EXISTS (
      SELECT 1
      FROM agent_task_queue AS active
      WHERE active.chat_session_id = sqlc.arg('chat_session_id')
        AND active.status IN ('dispatched', 'running', 'waiting_local_directory')
        AND active.regenerate_quick_actions_for IS NULL
    )
  FOR UPDATE
), demoted AS (
  UPDATE agent_task_queue AS queued
  SET priority = 3
  WHERE queued.chat_session_id = sqlc.arg('chat_session_id')
    AND queued.id <> sqlc.arg('id')
    AND queued.status = 'queued'
    AND queued.priority >= 4
    AND EXISTS (SELECT 1 FROM target)
), prioritized AS (
  UPDATE agent_task_queue AS selected
  SET priority = 4
  FROM target
  WHERE selected.id = target.id
  RETURNING selected.id
)
SELECT
  prioritized.id AS task_id,
  (
    SELECT active.id
    FROM agent_task_queue AS active
    WHERE active.chat_session_id = sqlc.arg('chat_session_id')
      AND active.status IN ('dispatched', 'running', 'waiting_local_directory')
      AND active.regenerate_quick_actions_for IS NULL
    ORDER BY active.created_at ASC, active.id ASC
    LIMIT 1
  )::uuid AS active_task_id
FROM prioritized;

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
-- idx_agent_task_queue_chat_pending_v3 partial-index predicate and use it.
SELECT atq.id AS task_id, atq.status, atq.chat_session_id, cs.agent_id
FROM agent_task_queue atq
JOIN chat_session cs ON cs.id = atq.chat_session_id
WHERE atq.chat_session_id IS NOT NULL
  AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  -- Exclude background quick-actions regeneration passes: they own no assistant
  -- turn and must not surface as "running" chat work (MUL-5149 refresh follow-up).
  AND atq.regenerate_quick_actions_for IS NULL
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
    AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
    -- Background quick-actions regeneration passes own no visible turn and must
    -- never light the FAB "running" indicator (MUL-5149 refresh follow-up).
    AND atq.regenerate_quick_actions_for IS NULL
    AND cs.workspace_id = sqlc.arg(workspace_id)
    AND cs.creator_id = sqlc.arg(creator_id)
    AND cs.agent_id = ANY(sqlc.arg(agent_ids)::uuid[])
) AS has_pending;

-- name: MarkChatSessionRead :exec
-- Advances the read cursor to now, dropping the session's unread_count to 0.
UPDATE chat_session SET last_read_at = now()
WHERE id = $1;

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

-- name: LockChatSessionsBySystemRuntimeAgents :many
SELECT cs.id FROM chat_session cs
JOIN agent a ON a.id = cs.agent_id
WHERE a.runtime_id = $1 AND a.kind = 'system'
ORDER BY cs.id
FOR UPDATE OF cs;

-- name: DeleteChatDraftRestoresBySystemRuntimeAgents :exec
-- chat_session cascades from agent, so hard-deleting a runtime's system agents
-- silently drops their sessions — and, without an FK, would strand the pending
-- restores (which still hold the user's prompt text) forever. Prune them in the
-- same tx, BEFORE the agent rows go: the join below needs them. Mirrors
-- DeleteChannelInstallationsBySystemRuntimeAgents.
--
-- Only system agents are hard-deleted on runtime teardown since MUL-5559; user
-- agents (archived or not) are unbound and keep their sessions and restores.
DELETE FROM chat_draft_restore
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs
    JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.kind = 'system'
);

-- name: GetChatMessageByTaskAssistant :one
-- The completed turn's assistant outcome row, for the quick-actions
-- supplement path (daemon suggestion pass finishing after chat:done).
SELECT * FROM chat_message
WHERE task_id = $1 AND role = 'assistant'
ORDER BY created_at DESC
LIMIT 1;

-- name: TaskInputIsOnboardingKickoffOnly :one
-- Whether this input batch is a kickoff and NOTHING else — the shape only a
-- pre-MUL-5827 opening task has, where the kickoff was a turn of its own.
--
-- Reachable exclusively during a rolling deploy: a kickoff task enqueued by the
-- old server and claimed by the new one. The reply to it is still that member's
-- opening, so it still has to be stamped, or their session permanently renders
-- without the starter cards (the kind is persisted; nothing recomputes it).
--
-- The "and nothing else" half is what makes this safe to keep: an input batch
-- that pairs the kickoff with a real member message is the NEW shape, and
-- stamping that turn would render the starter cards a second time under a reply
-- that is not an opening.
--
-- Delete this once no pre-deploy kickoff tasks can still be in flight; nothing
-- creates this shape any more.
--
-- $1 is the INPUT-OWNING task id — COALESCE(task.chat_input_task_id, task.id),
-- i.e. chatInputOwnerID — never a retry clone's own id, since the whole retry
-- chain consumes the root's input batch (MUL-4351).
SELECT
    EXISTS (
        SELECT 1 FROM chat_message AS kickoff
        WHERE kickoff.task_id = sqlc.arg(task_id)
          AND kickoff.role = 'user'
          AND kickoff.message_kind = 'onboarding_kickoff'
    )
    AND NOT EXISTS (
        SELECT 1 FROM chat_message AS typed
        WHERE typed.task_id = sqlc.arg(task_id)
          AND typed.role = 'user'
          AND typed.message_kind <> 'onboarding_kickoff'
    ) AS kickoff_only;

-- name: CreateMikaOnboardingOpening :one
-- Mika's opening reply, written by the server rather than produced by an agent
-- run (MUL-5827). Paired with the hidden kickoff row in one transaction, which
-- is why created_at is derived instead of defaulted: now() is the TRANSACTION
-- timestamp, so both rows would land on the identical microsecond, and the
-- session-list LATERAL picks the last message with `ORDER BY created_at DESC
-- LIMIT 1` and no tiebreaker (ids are random UUIDs, not monotonic). A tie there
-- can select the kickoff, whose kind makes buildChatLastMessage return nil — so
-- a session that onboarded perfectly reports no last message and the "Start
-- with Mika" recovery card reappears. One microsecond makes the order total.
--
-- task_id stays NULL: no agent run produced this row, and nothing may treat it
-- as a turn to regenerate or resume.
INSERT INTO chat_message (chat_session_id, role, content, message_kind, created_at, id)
VALUES (
    sqlc.arg(chat_session_id),
    'assistant',
    sqlc.arg(content),
    'onboarding_opening',
    sqlc.arg(kickoff_created_at)::timestamptz + interval '1 microsecond',
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())
)
RETURNING *;

-- name: AdoptOrphanOnboardingKickoff :exec
-- Hands the session's unowned kickoff row to the member's first real turn.
--
-- The kickoff is written with no task_id (nothing runs for the opening any
-- more), so it would never reach a runtime on its own. Adopting it into the
-- first send's input batch is what carries the onboarding skill instruction and
-- the profile block into the run that does the first real work — and, because
-- the kickoff quotes the opening the member already read, what stops Mika from
-- introducing herself a second time.
--
-- Idempotent by construction: exactly one such row can exist per session, and
-- the task_id IS NULL predicate means later sends adopt nothing.
UPDATE chat_message
SET task_id = $2
WHERE chat_session_id = $1
  AND role = 'user'
  AND message_kind = 'onboarding_kickoff'
  AND task_id IS NULL;

-- name: GetLatestAssistantChatMessageForSession :one
-- The session's most recent assistant turn, used as the regeneration target
-- when the user clicks "refresh" on the quick-actions row (MUL-5149). Only rows
-- with a task_id qualify — the daemon suggest supplement keys off task_id and
-- a resume needs a real completed turn to resume from.
SELECT * FROM chat_message
WHERE chat_session_id = $1 AND role = 'assistant' AND task_id IS NOT NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: SetChatMessageQuickActionsByTask :one
UPDATE chat_message
SET quick_actions = $2
WHERE id = (
    SELECT inner_msg.id FROM chat_message AS inner_msg
    WHERE inner_msg.task_id = $1 AND inner_msg.role = 'assistant'
    ORDER BY inner_msg.created_at DESC
    LIMIT 1
)
RETURNING *;

-- name: GetOldestActiveChatSessionForCreatorAgent :one
-- Identity for "this member's conversation with this agent", independent of
-- the session title. Mika's onboarding session used to be matched on its
-- localized title from the client, which made the lookup both racy and
-- language-dependent. Oldest wins so the answer stays stable once a member
-- has opened more than one session with the same agent.
SELECT * FROM chat_session
WHERE workspace_id = $1
  AND creator_id = $2
  AND agent_id = $3
  AND status = 'active'
ORDER BY created_at ASC
LIMIT 1;
