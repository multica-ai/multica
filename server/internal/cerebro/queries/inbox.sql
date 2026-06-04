-- Cerebro-only inbox queries. Lives in cerebrodb so upstream merges of
-- pkg/db/queries/inbox.sql don't conflict.
--
-- Note: these queries operate on the upstream `inbox_item` table; cerebro
-- adds columns (route, project_id, muted_until) via 9NNN migrations.

-- name: SetInboxMutedUntil :one
-- Mute an inbox item until the given timestamp (caller computes "next 08:00
-- local" or whatever duration applies). Idempotent — re-muting just updates
-- the timestamp.
UPDATE inbox_item
SET muted_until = $2,
    read = false
WHERE id = $1
RETURNING *;

-- name: SetInboxMutedUntilByIssue :execrows
-- Mute every sibling inbox item for the same recipient + issue. The inbox UI
-- deduplicates rows by issue_id, so muting only the clicked notification can
-- make an unmuted sibling resurface after refetch.
UPDATE inbox_item
SET muted_until = $5,
    read = false
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = false;

-- name: ClearInboxMute :one
-- Un-mute an inbox item by clearing the muted_until column.
UPDATE inbox_item
SET muted_until = NULL
WHERE id = $1
RETURNING *;

-- name: ClearInboxMuteByIssue :execrows
-- Unmute every sibling inbox item for the same recipient + issue.
UPDATE inbox_item
SET muted_until = NULL
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = false;

-- name: SetInboxUnread :one
-- Force an inbox item back to unread state. Used by the "Marker ulæst" action;
-- the value persists until the user explicitly opens the item again.
UPDATE inbox_item
SET read = false
WHERE id = $1
RETURNING *;

-- name: UnarchiveInboxItem :one
-- Restore an archived inbox item to the active inbox. Mirror of upstream
-- ArchiveInboxItem; lives in cerebrodb so we can ship the cerebro
-- archived-view unarchive action without touching upstream SQL.
UPDATE inbox_item
SET archived = false
WHERE id = $1
RETURNING *;

-- name: UnarchiveInboxByIssue :execrows
-- Restore every archived inbox_item for (recipient, issue) — mirrors
-- ArchiveInboxByIssue so issue-level unarchive surfaces every notification
-- the user had for that issue, not only the one row they clicked.
UPDATE inbox_item
SET archived = false
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = true;

-- name: FindPendingReminder :one
-- Dedupe guard for user-created reminders. A matching future reminder is
-- rescheduled instead of duplicated, so agents/autopilots can safely retry.
SELECT *
FROM inbox_item
WHERE workspace_id = $1
  AND recipient_type = 'member'
  AND recipient_id = $2
  AND type = 'reminder'
  AND archived = false
  AND (($3::uuid IS NULL AND issue_id IS NULL) OR issue_id = $3)
  AND title = $4
  AND COALESCE(body, '') = COALESCE($5, '')
  AND COALESCE(details->>'comment_id', '') = $6::text
  AND muted_until IS NOT NULL
  AND muted_until > NOW()
ORDER BY created_at DESC
LIMIT 1;

-- name: CreateReminderInboxItem :one
-- A reminder is an inbox item muted until its planned time. It lives in the
-- same Muted/Snooze view as normal muted inbox rows and automatically
-- resurfaces when muted_until passes.
INSERT INTO inbox_item (
    workspace_id, recipient_type, recipient_id,
    type, severity, issue_id, title, body,
    actor_type, actor_id, details, route, muted_until
) VALUES ($1, 'member', $2, 'reminder', 'action_required', $3, $4, $5, $6, $7, $8, 'inbox', $9)
RETURNING *;

-- name: UpdateReminderInboxItem :one
-- Reschedule an existing pending reminder instead of creating another copy.
UPDATE inbox_item
SET muted_until = $2,
    read = false,
    details = $3
WHERE id = $1
RETURNING *;

-- name: FindPrivateAgentRunRequest :one
-- FIR-2385 dedup guard: a repeated tag of the same private agent on the same
-- comment must not spam the owner with duplicate run-requests. Matches on the
-- (owner recipient, agent, comment) triple carried in details.
SELECT *
FROM inbox_item
WHERE workspace_id = $1
  AND recipient_type = 'member'
  AND recipient_id = $2
  AND type = 'private_agent_run_request'
  AND archived = false
  AND details->>'agent_id' = $3::text
  AND details->>'comment_id' = $4::text
ORDER BY created_at DESC
LIMIT 1;

-- name: CreatePrivateAgentRunRequest :one
-- FIR-2385: a non-owner tagged a private agent. Instead of waking the agent we
-- drop a run-request in the owner's inbox; the owner runs it from there. The
-- recipient is the agent owner; actor is the requester who tagged.
INSERT INTO inbox_item (
    workspace_id, recipient_type, recipient_id,
    type, severity, issue_id, title, body,
    actor_type, actor_id, details, route
) VALUES ($1, 'member', $2, 'private_agent_run_request', 'action_required', $3, $4, $5, 'member', $6, $7, 'inbox')
RETURNING *;

-- name: FindRuntimePauseInboxCard :one
-- FIR-2611 dedup guard: collapse a runtime's auth/quota bounce-loop into ONE
-- card per (runtime, day) instead of N failed-run rows. Matches the open card
-- for this runtime on this UTC date carried in details.
SELECT *
FROM inbox_item
WHERE workspace_id = $1
  AND recipient_type = 'member'
  AND recipient_id = $2
  AND type = 'runtime_auto_paused'
  AND archived = false
  AND details->>'runtime_id' = $3::text
  AND details->>'pause_date' = $4::text
ORDER BY created_at DESC
LIMIT 1;

-- name: CreateRuntimePauseInboxCard :one
-- FIR-2611: first auth/quota auto-pause for this runtime today. Runtime-level
-- (issue_id NULL) so the card aggregates across every issue the runtime was
-- working. actor is the system.
INSERT INTO inbox_item (
    workspace_id, recipient_type, recipient_id,
    type, severity, issue_id, title, body,
    actor_type, actor_id, details, route
) VALUES ($1, 'member', $2, 'runtime_auto_paused', $3, NULL, $4, $5, 'system', NULL, $6, 'inbox')
RETURNING *;

-- name: BumpRuntimePauseInboxCard :one
-- FIR-2611: a later auto-pause for the same runtime on the same day. Refresh
-- the card in place — bump the failed-run counter, update severity/title/body
-- and the parsed reset time, and force it unread so it resurfaces — instead of
-- adding another row.
UPDATE inbox_item
SET read = false,
    severity = $2,
    title = $3,
    body = $4,
    details = jsonb_set(
        jsonb_set(
            jsonb_set(COALESCE(details, '{}'::jsonb),
                '{failed_runs}', to_jsonb(((COALESCE(details->>'failed_runs', '0'))::int + 1)::text)),
            '{resets_at}', to_jsonb($5::text)),
        '{circuit_open}', to_jsonb($6::text))
WHERE id = $1
RETURNING *;
