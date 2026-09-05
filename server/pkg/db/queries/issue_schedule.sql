-- One-time scheduled run bound to an issue (#5927). See migration 450 for
-- the table shape and server/internal/service/issue_schedule.go for the
-- validation and dispatch logic.

-- name: CreateIssueScheduledTrigger :one
INSERT INTO issue_scheduled_trigger (
    workspace_id, issue_id, run_at, created_by_user_id
) VALUES (
    $1, $2, $3, $4
) RETURNING *;

-- name: GetIssueScheduledTrigger :one
SELECT * FROM issue_scheduled_trigger
WHERE id = $1;

-- name: GetPendingIssueScheduledTriggerForIssue :one
SELECT * FROM issue_scheduled_trigger
WHERE issue_id = $1 AND status = 'pending';

-- name: CancelIssueScheduledTrigger :one
-- Restricted to status = 'pending' so cancelling a trigger that already
-- fired (or was already cancelled/missed) is a no-op (0 rows) rather than
-- silently rewriting a terminal row.
UPDATE issue_scheduled_trigger
SET status = 'cancelled'
WHERE issue_id = $1 AND status = 'pending'
RETURNING *;

-- name: ListDueIssueScheduledTriggers :many
-- The scheduler's per-tick scope query (server/internal/scheduler/jobs_issue_schedule.go).
-- Bounded by LIMIT so a large backlog of simultaneously-due schedules cannot
-- blow up a single tick; anything left over is picked up on the next tick.
SELECT * FROM issue_scheduled_trigger
WHERE status = 'pending' AND run_at <= sqlc.arg('now')
ORDER BY run_at ASC
LIMIT sqlc.arg('limit_count');

-- name: MarkIssueScheduledTriggerFired :one
-- Doubles as the exclusive claim on this trigger (see
-- IssueScheduleService.DispatchIssueSchedule): it is the ONLY query that
-- transitions a row away from 'pending' on the fire path, so the
-- status = 'pending' guard means at most one caller ever wins it — even
-- under the scheduler's stale-lease-theft retry, which can otherwise invoke
-- DispatchIssueSchedule concurrently for the same trigger. A caller that
-- loses the race (0 rows / pgx.ErrNoRows) must treat that as "already being
-- handled" and not retry the enqueue itself.
UPDATE issue_scheduled_trigger
SET status = 'fired', fired_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: MarkIssueScheduledTriggerMissed :one
-- Called only after MarkIssueScheduledTriggerFired has already claimed this
-- row and the dispatch attempt itself then failed (issue deleted, assignee
-- removed, enqueue error). Restricted to status = 'fired' — not 'pending' —
-- because the claim above already consumed the 'pending' state; scoping to
-- 'fired' means this can only ever be reached by the same caller that won
-- that claim, so it needs no separate race guard of its own.
UPDATE issue_scheduled_trigger
SET status = 'missed'
WHERE id = $1 AND status = 'fired'
RETURNING *;
