-- Brief push to a member's chat app (spec 2.7, plan 9.10). The subscriber writes
-- the delivery rows, the retry job claims and settles them, and the preference
-- API reads and writes the member's choice.

-- name: GetBriefPushPreference :one
SELECT * FROM brief_push_preference
WHERE workspace_id = @workspace_id AND member_id = @member_id;

-- name: UpsertBriefPushPreference :one
-- The whole preference is replaced on each save: the API takes all three fields.
INSERT INTO brief_push_preference (
    id, workspace_id, member_id, brief_push, binding_id,
    quiet_hours_start, quiet_hours_end, updated_at
) VALUES (
    @id, @workspace_id, @member_id, @brief_push, sqlc.narg('binding_id'),
    sqlc.narg('quiet_hours_start'), sqlc.narg('quiet_hours_end'), now()
)
ON CONFLICT (workspace_id, member_id) DO UPDATE SET
    brief_push        = EXCLUDED.brief_push,
    binding_id        = EXCLUDED.binding_id,
    quiet_hours_start = EXCLUDED.quiet_hours_start,
    quiet_hours_end   = EXCLUDED.quiet_hours_end,
    updated_at        = now()
RETURNING *;

-- name: ListBriefPushRecipients :many
-- Members who get a brief pushed in this workspace: push on, a chat chosen, and
-- both the binding and the membership still present. A preference that points
-- at a binding of someone else, or at a removed member, is left out here.
SELECT p.workspace_id, p.member_id, p.binding_id
FROM brief_push_preference p
JOIN channel_user_binding b
  ON b.id = p.binding_id
 AND b.workspace_id = p.workspace_id
 AND b.multica_user_id = p.member_id
JOIN member m
  ON m.workspace_id = p.workspace_id
 AND m.user_id = p.member_id
WHERE p.workspace_id = @workspace_id
  AND p.brief_push = true
  AND p.binding_id IS NOT NULL
ORDER BY p.member_id;

-- name: CreateBriefDelivery :execrows
-- @now is the application clock, the same one the retry job claims against, so
-- the row's first attempt is due on the schedule the job reads. A subscriber
-- that runs twice for one brief inserts nothing new.
INSERT INTO brief_delivery (
    id, workspace_id, brief_id, member_id, binding_id, state, attempts, next_attempt_at, created_at
) VALUES (
    @id, @workspace_id, @brief_id, @member_id, @binding_id, 'pending', 0, @now, @now
)
ON CONFLICT (brief_id, member_id) DO NOTHING;

-- name: ClaimDueBriefDeliveries :many
-- Takes up to @row_limit due rows and moves their next attempt to @lease_until,
-- so an overlapping run skips them while this one sends. A run that dies leaves
-- the lease to expire, and the rows come back.
UPDATE brief_delivery SET next_attempt_at = @lease_until
WHERE id IN (
    SELECT d.id FROM brief_delivery d
    WHERE d.state IN ('pending', 'deferred')
      AND d.next_attempt_at <= @due_at
    ORDER BY d.next_attempt_at, d.id
    LIMIT @row_limit
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: RecordBriefDeliveryOutcome :execrows
-- One write for every outcome: sent, failed, deferred or pending with a backoff.
UPDATE brief_delivery
SET state = @state,
    attempts = @attempts,
    next_attempt_at = @next_attempt_at,
    last_error = @last_error,
    delivered_at = sqlc.narg('delivered_at')
WHERE id = @id;

-- name: ListBriefPendingDecisions :many
-- The pending proposals a brief raises, with their titles. A snoozed proposal
-- waits out its snooze and is left out, as the digest and the brief leave it.
-- @now is the application clock, as in CreateBriefDelivery. The caller puts the
-- rows in the order the brief lists them.
SELECT p.id, i.title
FROM proposal p
JOIN issue i ON i.id = p.issue_id AND i.workspace_id = p.workspace_id
WHERE p.workspace_id = @workspace_id
  AND p.id = ANY(@proposal_ids::uuid[])
  AND p.state = 'pending'
  AND (p.snoozed_until IS NULL OR p.snoozed_until <= @now);

-- name: GetChannelUserBindingInWorkspace :one
SELECT * FROM channel_user_binding
WHERE id = @id AND workspace_id = @workspace_id;

-- name: DeleteWorkspaceBriefDeliveries :exec
DELETE FROM brief_delivery WHERE workspace_id = $1;

-- name: DeleteWorkspaceBriefPushPreferences :exec
DELETE FROM brief_push_preference WHERE workspace_id = $1;
