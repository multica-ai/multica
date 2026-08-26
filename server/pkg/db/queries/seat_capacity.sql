-- name: UpsertSeatCapacityIntent :one
INSERT INTO seat_capacity_outbox (
    workspace_id, operation_token, action, subject_id, member_id,
    invitation_id, share_link_id, user_id, expires_at, next_attempt_at
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('operation_token'), sqlc.arg('action'),
    sqlc.narg('subject_id'), sqlc.narg('member_id'), sqlc.narg('invitation_id'),
    sqlc.narg('share_link_id'), sqlc.narg('user_id'), sqlc.narg('expires_at'),
    sqlc.arg('next_attempt_at')
)
ON CONFLICT (operation_token) DO UPDATE SET
    action = EXCLUDED.action,
    subject_id = EXCLUDED.subject_id,
    member_id = EXCLUDED.member_id,
    invitation_id = EXCLUDED.invitation_id,
    share_link_id = EXCLUDED.share_link_id,
    user_id = EXCLUDED.user_id,
    expires_at = EXCLUDED.expires_at,
    attempt_count = CASE
        WHEN seat_capacity_outbox.dead_lettered_at IS NOT NULL THEN 0
        WHEN seat_capacity_outbox.action <> EXCLUDED.action THEN 0
        ELSE seat_capacity_outbox.attempt_count
    END,
    delivered_at = CASE
        WHEN seat_capacity_outbox.action = EXCLUDED.action THEN seat_capacity_outbox.delivered_at
        ELSE NULL
    END,
    next_attempt_at = EXCLUDED.next_attempt_at,
    lease_token = NULL,
    last_error = NULL,
    dead_lettered_at = NULL,
    updated_at = now()
WHERE seat_capacity_outbox.action = EXCLUDED.action
   OR (seat_capacity_outbox.action = 'reserve_invitation' AND EXCLUDED.action = 'consume_invitation')
RETURNING *;

-- name: EnqueueSeatCapacityRelease :one
INSERT INTO seat_capacity_outbox (
    workspace_id, operation_token, action, subject_id, next_attempt_at
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('operation_token'), 'release',
    sqlc.narg('subject_id'), sqlc.arg('next_attempt_at')
)
ON CONFLICT (operation_token) DO UPDATE SET
    action = 'release',
    subject_id = EXCLUDED.subject_id,
    member_id = NULL,
    invitation_id = NULL,
    share_link_id = NULL,
    user_id = NULL,
    expires_at = NULL,
    delivered_at = NULL,
    attempt_count = 0,
    next_attempt_at = EXCLUDED.next_attempt_at,
    lease_token = NULL,
    last_error = NULL,
    dead_lettered_at = NULL,
    updated_at = now()
WHERE seat_capacity_outbox.action IN (
    'reserve_invitation', 'consume_invitation', 'claim_share_join', 'release'
)
RETURNING *;

-- name: GetSeatCapacityIntent :one
SELECT * FROM seat_capacity_outbox WHERE operation_token = $1;

-- name: GetClaimedSeatCapacityIntent :one
SELECT * FROM seat_capacity_outbox
WHERE operation_token = sqlc.arg('operation_token')
  AND action = sqlc.arg('action')
  AND lease_token = sqlc.arg('lease_token');

-- name: CreateOrReactivateShareJoinCapacityIntent :one
INSERT INTO seat_capacity_outbox (
    workspace_id, operation_token, action, subject_id, share_link_id, user_id,
    next_attempt_at
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('operation_token'), 'claim_share_join',
    sqlc.arg('operation_token'), sqlc.arg('share_link_id'), sqlc.arg('user_id'),
    sqlc.arg('next_attempt_at')
)
ON CONFLICT (workspace_id, share_link_id, user_id)
WHERE share_link_id IS NOT NULL AND user_id IS NOT NULL
DO UPDATE SET
    action = 'claim_share_join',
    subject_id = seat_capacity_outbox.operation_token,
    member_id = NULL,
    invitation_id = NULL,
    expires_at = NULL,
    delivered_at = NULL,
    attempt_count = 0,
    next_attempt_at = EXCLUDED.next_attempt_at,
    lease_token = NULL,
    last_error = NULL,
    dead_lettered_at = NULL,
    updated_at = now()
WHERE seat_capacity_outbox.action IN ('claim_share_join', 'release')
RETURNING *;

-- name: GetMemberReleaseCapacityIntent :one
SELECT * FROM seat_capacity_outbox
WHERE workspace_id = $1
  AND member_id = $2
  AND action = 'release_member'
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteSeatCapacityConfirmIntentsForWorkspaceDeletion :exec
-- Confirmed seats are released only through the member-scoped operation below.
-- Never rewrite them to token release: Cloud may accept token release for a
-- used operation, which would let a stale confirm bypass member ownership.
DELETE FROM seat_capacity_outbox
WHERE workspace_id = sqlc.arg('workspace_id')
  AND action = 'confirm';

-- name: PrepareSeatCapacityOperationReleasesForWorkspaceDeletion :exec
-- Workspace teardown commits these compensations atomically with removal of
-- the product rows. The FK-free outbox intentionally survives so Cloud can be
-- reconciled after the local workspace no longer exists.
UPDATE seat_capacity_outbox
SET action = 'release',
    member_id = NULL,
    delivered_at = NULL,
    attempt_count = 0,
    next_attempt_at = now(),
    lease_token = NULL,
    last_error = NULL,
    dead_lettered_at = NULL,
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND action NOT IN ('confirm', 'release_member');

-- name: PrepareSeatCapacityInvitationReleasesForWorkspaceDeletion :exec
INSERT INTO seat_capacity_outbox (
    workspace_id, operation_token, action, subject_id, invitation_id,
    next_attempt_at
)
SELECT invitation.workspace_id, invitation.id, 'release', invitation.id, invitation.id, now()
FROM workspace_invitation AS invitation
WHERE invitation.workspace_id = sqlc.arg('workspace_id')
  AND invitation.status = 'pending'
ON CONFLICT (operation_token) DO UPDATE SET
    action = 'release',
    member_id = NULL,
    delivered_at = NULL,
    attempt_count = 0,
    next_attempt_at = now(),
    lease_token = NULL,
    last_error = NULL,
    dead_lettered_at = NULL,
    updated_at = now()
WHERE seat_capacity_outbox.action IN (
    'reserve_invitation', 'consume_invitation', 'claim_share_join', 'release'
);

-- name: PrepareSeatCapacityMemberReleasesForWorkspaceDeletion :exec
INSERT INTO seat_capacity_outbox (
    workspace_id, operation_token, action, member_id, next_attempt_at
)
SELECT m.workspace_id, gen_random_uuid(), 'release_member', m.id, now()
FROM member AS m
WHERE m.workspace_id = sqlc.arg('workspace_id')
  AND NOT EXISTS (
      SELECT 1
      FROM seat_capacity_outbox AS existing
      WHERE existing.workspace_id = m.workspace_id
        AND existing.member_id = m.id
        AND existing.action = 'release_member'
  );

-- name: ClaimNextDueSeatCapacityIntent :one
WITH due AS (
    SELECT operation_token
    FROM seat_capacity_outbox
    WHERE dead_lettered_at IS NULL
      AND next_attempt_at <= now()
    ORDER BY next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE seat_capacity_outbox AS outbox
SET next_attempt_at = sqlc.arg('lease_until'),
    lease_token = gen_random_uuid(),
    updated_at = now()
FROM due
WHERE outbox.operation_token = due.operation_token
RETURNING outbox.*;

-- name: SeatCapacityOutboxStats :many
SELECT action,
       count(*) FILTER (WHERE dead_lettered_at IS NULL)::bigint AS pending_count,
       count(*) FILTER (WHERE dead_lettered_at IS NOT NULL)::bigint AS dead_lettered_count,
       COALESCE(
           EXTRACT(EPOCH FROM now() - min(created_at) FILTER (WHERE dead_lettered_at IS NULL)),
           0
       )::double precision AS oldest_pending_age_seconds
FROM seat_capacity_outbox
GROUP BY action;

-- name: MarkSeatCapacityIntentDelivered :exec
UPDATE seat_capacity_outbox
SET delivered_at = now(),
    attempt_count = attempt_count + 1,
    lease_token = NULL,
    last_error = NULL,
    updated_at = now()
WHERE operation_token = $1 AND action = $2;

-- name: MarkSeatCapacityIntentFailed :exec
UPDATE seat_capacity_outbox
SET attempt_count = attempt_count + 1,
    last_error = left(sqlc.arg('last_error'), 1000),
    next_attempt_at = sqlc.arg('next_attempt_at'),
    lease_token = NULL,
    updated_at = now()
WHERE operation_token = sqlc.arg('operation_token') AND action = sqlc.arg('action');

-- name: MarkClaimedSeatCapacityIntentDeadLettered :execrows
UPDATE seat_capacity_outbox
SET attempt_count = attempt_count + 1,
    last_error = left(sqlc.arg('last_error'), 1000),
    dead_lettered_at = now(),
    lease_token = NULL,
    updated_at = now()
WHERE operation_token = sqlc.arg('operation_token')
  AND action = sqlc.arg('action')
  AND lease_token = sqlc.arg('lease_token')
  AND dead_lettered_at IS NULL;

-- name: TransitionSeatCapacityIntent :execrows
UPDATE seat_capacity_outbox
SET action = sqlc.arg('next_action'),
    member_id = sqlc.narg('member_id'),
    delivered_at = NULL,
    attempt_count = 0,
    next_attempt_at = sqlc.arg('next_attempt_at'),
    lease_token = NULL,
    last_error = NULL,
    dead_lettered_at = NULL,
    updated_at = now()
WHERE operation_token = sqlc.arg('operation_token')
  AND action = sqlc.arg('current_action');

-- name: TransitionClaimedSeatCapacityIntent :execrows
UPDATE seat_capacity_outbox
SET action = sqlc.arg('next_action'),
    member_id = sqlc.narg('member_id'),
    delivered_at = NULL,
    attempt_count = 0,
    next_attempt_at = sqlc.arg('next_attempt_at'),
    lease_token = NULL,
    last_error = NULL,
    dead_lettered_at = NULL,
    updated_at = now()
WHERE operation_token = sqlc.arg('operation_token')
  AND action = sqlc.arg('current_action')
  AND lease_token = sqlc.arg('lease_token');

-- name: MarkClaimedSeatCapacityIntentFailed :execrows
UPDATE seat_capacity_outbox
SET attempt_count = attempt_count + 1,
    last_error = left(sqlc.arg('last_error'), 1000),
    next_attempt_at = sqlc.arg('next_attempt_at'),
    lease_token = NULL,
    updated_at = now()
WHERE operation_token = sqlc.arg('operation_token')
  AND action = sqlc.arg('action')
  AND lease_token = sqlc.arg('lease_token');

-- name: DeferClaimedSeatCapacityIntent :execrows
UPDATE seat_capacity_outbox
SET last_error = left(sqlc.arg('last_error'), 1000),
    next_attempt_at = sqlc.arg('next_attempt_at'),
    lease_token = NULL,
    updated_at = now()
WHERE operation_token = sqlc.arg('operation_token')
  AND action = sqlc.arg('action')
  AND lease_token = sqlc.arg('lease_token')
  AND dead_lettered_at IS NULL;

-- name: DeleteSeatCapacityIntentForAction :exec
DELETE FROM seat_capacity_outbox
WHERE operation_token = $1 AND action = $2;

-- name: DeleteClaimedSeatCapacityIntent :execrows
DELETE FROM seat_capacity_outbox
WHERE operation_token = sqlc.arg('operation_token')
  AND action = sqlc.arg('action')
  AND lease_token = sqlc.arg('lease_token');

-- name: ExpireInvitationForCapacityRecovery :exec
UPDATE workspace_invitation
SET status = 'expired', updated_at = now()
WHERE id = $1 AND status = 'pending';
