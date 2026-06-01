-- name: CreateGovernanceApproval :one
INSERT INTO governance_approval (
    workspace_id, action_id, target_type, target_id, issue_id,
    approval_source_type, approval_source_id, approved_by_type, approved_by_id,
    reason, expires_at
) VALUES (
    $1, $2, $3, $4, sqlc.narg('issue_id'),
    $5, sqlc.narg('approval_source_id'), $6, $7,
    $8, sqlc.narg('expires_at')
) RETURNING *;

-- name: FindActiveGovernanceApproval :one
SELECT * FROM governance_approval
WHERE workspace_id = $1
  AND action_id = $2
  AND target_type = $3
  AND target_id = $4
  AND consumed_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at DESC
LIMIT 1;

-- name: ClaimActiveGovernanceApproval :one
UPDATE governance_approval
SET consumed_at = now()
WHERE id = (
    SELECT id FROM governance_approval
    WHERE workspace_id = $1
      AND action_id = $2
      AND target_type = $3
      AND target_id = $4
      AND consumed_at IS NULL
      AND (expires_at IS NULL OR expires_at > now())
    ORDER BY created_at DESC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ListGovernanceApprovals :many
SELECT * FROM governance_approval
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CreateGovernanceAudit :one
INSERT INTO governance_audit (
    workspace_id, action_id, target_type, target_id, actor_type, actor_id,
    before_summary, after_summary, issue_id, approval_id,
    approval_source_type, approval_source_id
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, sqlc.narg('issue_id'), sqlc.narg('approval_id'),
    sqlc.narg('approval_source_type'), sqlc.narg('approval_source_id')
) RETURNING *;

-- name: ListGovernanceAudits :many
SELECT * FROM governance_audit
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
