-- name: ListApprovalsByIssue :many
SELECT * FROM approvals WHERE issue_id = $1 ORDER BY created_at DESC;

-- name: ListPendingApprovalsByApprover :many
SELECT a.*, i.title as issue_title, i.number as issue_number
FROM approvals a
JOIN issue i ON i.id = a.issue_id
WHERE a.workspace_id = $1
  AND a.approver_type = $2
  AND a.approver_id = $3
  AND a.status = 'pending'
ORDER BY a.created_at DESC;

-- name: CreateApproval :one
INSERT INTO approvals (workspace_id, issue_id, requester_type, requester_id, approver_type, approver_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ApproveApproval :one
UPDATE approvals SET status = 'approved', comment = $2, decided_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: RejectApproval :one
UPDATE approvals SET status = 'rejected', comment = $2, decided_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: GetApproval :one
SELECT * FROM approvals WHERE id = $1;

-- name: CountPendingApprovalsByApprover :one
SELECT COUNT(*) FROM approvals
WHERE workspace_id = $1
  AND approver_type = $2
  AND approver_id = $3
  AND status = 'pending';
