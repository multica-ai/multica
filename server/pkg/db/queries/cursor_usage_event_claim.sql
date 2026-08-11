-- name: GetCursorUsageEventClaimOwner :one
SELECT task_id
FROM cursor_usage_event_claim
WHERE account_key = $1
  AND occurrence_key = $2;

-- name: InsertCursorUsageEventClaim :execrows
INSERT INTO cursor_usage_event_claim (
    account_key,
    occurrence_key,
    task_id,
    workspace_id
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (account_key, occurrence_key) DO NOTHING;
