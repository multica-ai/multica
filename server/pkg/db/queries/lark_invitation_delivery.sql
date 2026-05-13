-- name: CreateLarkInvitationDelivery :exec
INSERT INTO lark_invitation_delivery (
    invitation_id,
    workspace_id,
    tenant_key,
    invitee_email,
    dedupe_key
) VALUES (
    $1,
    $2,
    $3,
    lower(trim($4)),
    $5
)
ON CONFLICT (dedupe_key) DO NOTHING;

-- name: ClaimPendingLarkInvitationDeliveries :many
UPDATE lark_invitation_delivery
SET status = 'sending', updated_at = now()
WHERE id IN (
    SELECT id
    FROM lark_invitation_delivery
    WHERE (
        (status = 'pending' AND next_attempt_at <= now())
        OR (status = 'sending' AND updated_at < now() - interval '5 minutes')
    )
      AND retry_count < sqlc.arg('max_attempts')::int
    ORDER BY next_attempt_at ASC, created_at ASC
    LIMIT sqlc.arg('limit')::int
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: GetLarkInvitationDeliveryMessage :one
SELECT
    d.id,
    d.invitation_id,
    d.workspace_id,
    d.tenant_key,
    d.invitee_email,
    d.lark_open_id,
    d.status,
    d.dedupe_key,
    d.retry_count,
    d.next_attempt_at,
    d.last_error,
    d.sent_message_id,
    d.sent_at,
    d.created_at,
    d.updated_at,
    wi.inviter_id,
    wi.invitee_user_id,
    wi.role,
    wi.status AS invitation_status,
    wi.expires_at,
    w.name AS workspace_name,
    inviter.name AS inviter_name,
    inviter.email AS inviter_email,
    identity.open_id AS identity_open_id
FROM lark_invitation_delivery d
JOIN workspace_invitation wi ON wi.id = d.invitation_id
JOIN workspace w ON w.id = d.workspace_id
JOIN "user" inviter ON inviter.id = wi.inviter_id
LEFT JOIN LATERAL (
    SELECT x.open_id
    FROM user_external_identity x
    WHERE x.provider = sqlc.arg('provider')::text
      AND x.tenant_key = d.tenant_key
      AND x.open_id IS NOT NULL
      AND x.open_id <> ''
      AND (
        lower(x.email) = d.invitee_email
        OR (wi.invitee_user_id IS NOT NULL AND x.user_id = wi.invitee_user_id)
      )
    ORDER BY x.last_synced_at DESC NULLS LAST, x.updated_at DESC
    LIMIT 1
) identity ON true
WHERE d.id = sqlc.arg('id')::uuid;

-- name: SetLarkInvitationDeliveryOpenID :exec
UPDATE lark_invitation_delivery
SET lark_open_id = sqlc.arg('lark_open_id')::text,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid;

-- name: MarkLarkInvitationDeliverySent :exec
UPDATE lark_invitation_delivery
SET status = 'sent',
    sent_message_id = sqlc.narg('sent_message_id'),
    sent_at = now(),
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid;

-- name: MarkLarkInvitationDeliveryPendingAfterFailure :exec
UPDATE lark_invitation_delivery
SET status = 'pending',
    retry_count = retry_count + 1,
    next_attempt_at = now() + (sqlc.arg('delay_seconds')::int * interval '1 second'),
    last_error = left(sqlc.arg('last_error')::text, 2000),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid;

-- name: MarkLarkInvitationDeliveryFailed :exec
UPDATE lark_invitation_delivery
SET status = 'failed',
    last_error = left(sqlc.arg('last_error')::text, 2000),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid;

-- name: MarkLarkInvitationDeliverySkipped :exec
UPDATE lark_invitation_delivery
SET status = 'skipped',
    last_error = left(sqlc.arg('last_error')::text, 2000),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid;
