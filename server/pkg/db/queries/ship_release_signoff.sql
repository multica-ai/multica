-- Phase 7d follow-up — two-approver signoff tracking. For workspaces
-- using the "two" approval rule, each release tracks first + second
-- approver signoffs as separate rows; both are required before the
-- stage transitions to verifying.

-- name: UpsertReleaseSignoff :one
-- Idempotent: inserting the same (release_id, approver_slot) pair
-- updates the signed_by + note. The audit trail (release_event) is
-- the durable record of who signed when.
INSERT INTO ship_release_signoff (release_id, approver_slot, signed_by, note)
VALUES ($1, $2, $3, sqlc.narg('note'))
ON CONFLICT (release_id, approver_slot) DO UPDATE SET
    signed_by = EXCLUDED.signed_by,
    signed_at = NOW(),
    note      = EXCLUDED.note
RETURNING *;

-- name: ListReleaseSignoffs :many
SELECT * FROM ship_release_signoff
WHERE release_id = $1
ORDER BY approver_slot ASC;

-- name: DeleteReleaseSignoffs :exec
-- Used on Unverify when the rule is "two" — the next verify cycle
-- starts fresh.
DELETE FROM ship_release_signoff WHERE release_id = $1;
