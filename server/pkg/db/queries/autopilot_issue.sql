-- name: UpsertAutopilotIssue :exec
-- Record that an autopilot run created (or is tracking) a specific GitLab
-- issue. Idempotent on the composite key. Callers discard the row, so :exec
-- avoids a spurious pgx.ErrNoRows on conflict.
INSERT INTO autopilot_issue (autopilot_run_id, workspace_id, gitlab_iid)
VALUES ($1, $2, $3)
ON CONFLICT (autopilot_run_id, workspace_id, gitlab_iid) DO NOTHING;

-- name: GetAutopilotIssueByIID :one
-- Given a workspace + gitlab_iid, return the autopilot run that owns this
-- issue, if any.
SELECT * FROM autopilot_issue
WHERE workspace_id = $1 AND gitlab_iid = $2
LIMIT 1;

-- name: ListAutopilotIssuesByRun :many
SELECT * FROM autopilot_issue WHERE autopilot_run_id = $1;
