-- Standing approvals (spec 4.4). Every query filters by workspace_id. Policies
-- are created and revoked only through service.CreateApprovalPolicy and
-- service.RevokeApprovalPolicy, which refuse agent actors.

-- name: CreateApprovalPolicy :one
INSERT INTO approval_policy (id, workspace_id, name, kind, match, max_per_week, valid_until, created_by)
VALUES (
    sqlc.arg('id')::uuid,
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('name')::text,
    sqlc.arg('kind')::text,
    sqlc.arg('match')::jsonb,
    sqlc.arg('max_per_week')::integer,
    sqlc.narg('valid_until')::date,
    sqlc.arg('created_by')::uuid
)
RETURNING *;

-- name: GetApprovalPolicyInWorkspace :one
SELECT * FROM approval_policy WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: LockApprovalPolicyInWorkspace :one
SELECT * FROM approval_policy WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid FOR UPDATE;

-- name: ListApprovalPolicies :many
SELECT * FROM approval_policy WHERE workspace_id = sqlc.arg('workspace_id')::uuid ORDER BY created_at DESC, id DESC;

-- name: ListActiveApprovalPolicies :many
SELECT * FROM approval_policy
WHERE workspace_id = sqlc.arg('workspace_id')::uuid AND active
ORDER BY created_at ASC, id ASC;

-- name: RevokeApprovalPolicy :one
UPDATE approval_policy SET active = false
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: CountPolicyAutoApprovalsSince :one
-- The weekly cap: proposals this policy approved since the given time.
SELECT count(*)::bigint FROM proposal
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND decided_by_policy_id = sqlc.arg('policy_id')::uuid
  AND decided_at >= sqlc.arg('since')::timestamptz;

-- name: CountPolicyAutoApprovalsByPolicy :many
-- The weekly counts of every policy in the workspace, for the Policies list.
SELECT decided_by_policy_id AS policy_id, count(*)::bigint AS approvals
FROM proposal
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND decided_by_policy_id IS NOT NULL
  AND decided_at >= sqlc.arg('since')::timestamptz
GROUP BY decided_by_policy_id;

-- name: ListPolicyAutoApprovals :many
-- Recent auto-approvals of one policy, newest first, with the backing issue's
-- title and number for the history.
SELECT p.id, p.squad_id, p.kind, p.state, p.decided_at, p.reversed_at, p.reversal_note,
       i.title AS issue_title, i.number AS issue_number, i.workspace_id AS issue_workspace_id
FROM proposal p
JOIN issue i ON i.id = p.issue_id
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND p.decided_by_policy_id = sqlc.arg('policy_id')::uuid
ORDER BY p.decided_at DESC, p.id DESC
LIMIT sqlc.arg('row_limit')::integer;

-- name: ListPolicyReversals :many
-- Auto-approvals a review reversed, newest reversal first. The weekly review
-- reads it with since; the Policies list reads the policies it names.
SELECT p.id, p.decided_by_policy_id AS policy_id, p.reversed_at, p.reversal_note, p.decided_at,
       i.title AS issue_title, i.number AS issue_number
FROM proposal p
JOIN issue i ON i.id = p.issue_id
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND p.decided_by_policy_id IS NOT NULL
  AND p.reversed_at IS NOT NULL
  AND p.reversed_at >= sqlc.arg('since')::timestamptz
ORDER BY p.reversed_at DESC, p.id DESC;

-- name: SetProposalPolicyDecision :exec
-- Records that a policy approved the proposal, in the same transaction as the
-- approval itself.
UPDATE proposal SET decided_by_policy_id = sqlc.arg('policy_id')::uuid
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: SetProposalEstimatedCost :exec
UPDATE proposal SET estimated_cost_micros = sqlc.arg('estimated_cost_micros')::bigint
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: ReverseAutoApproval :one
-- A review reverses an auto-approval. Only an auto-approved proposal that is not
-- yet reversed can be reversed.
UPDATE proposal SET reversed_at = now(), reversal_note = sqlc.arg('reversal_note')::text
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND decided_by_policy_id IS NOT NULL
  AND reversed_at IS NULL
RETURNING *;

-- name: ListRecentPolicyApprovals :many
-- Auto-approvals decided since the given time, for the digest's FYI. A NULL
-- squad_ids reads the whole company; a floor digest passes its floor's squads.
SELECT p.id, p.decided_by_policy_id AS policy_id, p.decided_at, p.squad_id,
       i.title AS issue_title, i.number AS issue_number, pol.name AS policy_name
FROM proposal p
JOIN issue i ON i.id = p.issue_id
JOIN approval_policy pol ON pol.id = p.decided_by_policy_id
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND p.decided_by_policy_id IS NOT NULL
  AND p.decided_at >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('squad_ids')::uuid[] IS NULL OR p.squad_id = ANY(sqlc.narg('squad_ids')::uuid[]))
ORDER BY p.decided_at DESC, p.id DESC
LIMIT sqlc.arg('row_limit')::integer;

-- name: DeleteWorkspaceApprovalPolicies :exec
-- Workspace teardown only: policies go with their workspace.
DELETE FROM approval_policy WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: CountPolicyReviewEvents :one
-- Weekly reviews already recorded for a period end (YYYY-MM-DD), so a rerun does
-- not write the same review twice.
SELECT count(*)::int FROM governance_event
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND kind = 'review_published'
  AND details->>'review' = 'policy_reversals'
  AND details->>'period_end' = sqlc.arg('period_end')::text;
