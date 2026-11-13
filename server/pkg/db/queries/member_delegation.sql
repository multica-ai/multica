-- name: RecordMemberDelegationDay :exec
-- Idempotent: one row per member per UTC day. acted_at NULL means now.
INSERT INTO member_delegation_day (user_id, activity_date)
VALUES (
    sqlc.arg('user_id'),
    (COALESCE(sqlc.narg('acted_at')::timestamptz, CURRENT_TIMESTAMP) AT TIME ZONE 'UTC')::date
)
ON CONFLICT DO NOTHING;
