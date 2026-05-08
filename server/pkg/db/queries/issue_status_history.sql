-- Queries for the issue_status_history audit table introduced in PUL-13 P0
-- (migration 070_issue_status_flow_p0.up.sql). Used by P1 to record auto-flips
-- triggered by comment.created hooks (source='hook_comment') alongside the
-- existing manual / skill_publish / skill_pickup / webhook_forge / backfill
-- sources.
--
-- Idempotency: UNIQUE (source, ref_id) on the table itself is the dedup
-- contract. Hook handlers MUST set ref_id to a stable identifier of the
-- triggering event (for hook_comment that's the comment id). When the same
-- hook fires twice (network retry, replay, etc.), the second insert fails
-- with unique_violation and the caller treats it as a successful no-op.

-- name: InsertStatusHistory :one
INSERT INTO issue_status_history (
    issue_id,
    from_status,
    to_status,
    source,
    actor_id,
    actor_type,
    ref_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: ListStatusHistoryByIssue :many
-- Newest-first audit timeline for a single issue.
SELECT * FROM issue_status_history
WHERE issue_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: GetStatusHistoryByRef :one
-- Reverse-lookup: given a (source, ref_id) pair, return the history row that
-- recorded the corresponding flip (if any). Useful for debugging
-- "why did this comment cause a status flip?" without scanning the table.
-- UNIQUE (source, ref_id) guarantees at most one row.
SELECT * FROM issue_status_history
WHERE source = $1 AND ref_id = $2;
