-- =====================
-- ADO Installation
-- =====================

-- name: ListADOInstallationsByWorkspace :many
SELECT * FROM ado_installation
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetADOInstallationByID :one
SELECT * FROM ado_installation
WHERE id = $1 AND workspace_id = $2;

-- name: GetADOInstallationByOrgURL :one
SELECT * FROM ado_installation
WHERE workspace_id = $1 AND org_url = $2;

-- name: GetADOInstallationByWebhookSecret :one
SELECT * FROM ado_installation
WHERE webhook_secret = $1;

-- name: CreateADOInstallation :one
INSERT INTO ado_installation (
    workspace_id, org_url, display_name, pat_encrypted, connected_by_id
) VALUES (
    $1, $2, $3, sqlc.narg('pat_encrypted'), sqlc.narg('connected_by_id')
)
ON CONFLICT (workspace_id, org_url) DO UPDATE SET
    display_name    = EXCLUDED.display_name,
    pat_encrypted   = COALESCE(EXCLUDED.pat_encrypted, ado_installation.pat_encrypted),
    connected_by_id = COALESCE(EXCLUDED.connected_by_id, ado_installation.connected_by_id),
    updated_at      = now()
RETURNING *;

-- name: DeleteADOInstallation :exec
DELETE FROM ado_installation WHERE id = $1 AND workspace_id = $2;

-- name: GetADOInstallationWebhookSecret :one
SELECT webhook_secret FROM ado_installation WHERE id = $1;

-- =====================
-- ADO Pull Request
-- =====================

-- name: UpsertADOPullRequest :one
INSERT INTO ado_pull_request (
    workspace_id, installation_id, org_url, project, repo_name, repo_id_ado, pr_id_ado,
    title, state, html_url, branch, author_login, author_avatar_url,
    merged_at, closed_at, pr_created_at, pr_updated_at,
    policy_status, merge_status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12, $13,
    sqlc.narg('merged_at'), sqlc.narg('closed_at'), $14, $15,
    sqlc.narg('policy_status'), sqlc.narg('merge_status')
)
ON CONFLICT (workspace_id, org_url, project, repo_name, pr_id_ado) DO UPDATE SET
    title           = EXCLUDED.title,
    state           = EXCLUDED.state,
    html_url        = EXCLUDED.html_url,
    branch          = COALESCE(EXCLUDED.branch, ado_pull_request.branch),
    author_login    = COALESCE(EXCLUDED.author_login, ado_pull_request.author_login),
    author_avatar_url = COALESCE(EXCLUDED.author_avatar_url, ado_pull_request.author_avatar_url),
    merged_at       = COALESCE(EXCLUDED.merged_at, ado_pull_request.merged_at),
    closed_at       = COALESCE(EXCLUDED.closed_at, ado_pull_request.closed_at),
    pr_updated_at   = EXCLUDED.pr_updated_at,
    policy_status   = CASE WHEN EXCLUDED.policy_status IS NOT NULL THEN EXCLUDED.policy_status ELSE ado_pull_request.policy_status END,
    merge_status    = COALESCE(EXCLUDED.merge_status, ado_pull_request.merge_status),
    updated_at      = now()
RETURNING *;

-- name: GetADOPullRequest :one
SELECT * FROM ado_pull_request
WHERE workspace_id = $1 AND org_url = $2 AND project = $3 AND repo_name = $4 AND pr_id_ado = $5;

-- name: GetADOPullRequestByID :one
SELECT * FROM ado_pull_request WHERE id = $1;

-- name: UpdateADOPullRequestPolicyStatus :one
UPDATE ado_pull_request SET
    policy_status = $2,
    merge_status  = sqlc.narg('merge_status'),
    updated_at    = now()
WHERE id = $1
RETURNING *;

-- name: ListADOPullRequestsByIssue :many
SELECT
    pr.id,
    pr.workspace_id,
    pr.org_url,
    pr.project,
    pr.repo_name,
    pr.pr_id_ado,
    pr.title,
    pr.state,
    pr.html_url,
    pr.branch,
    pr.author_login,
    pr.author_avatar_url,
    pr.merged_at,
    pr.closed_at,
    pr.pr_created_at,
    pr.pr_updated_at,
    pr.policy_status,
    pr.merge_status,
    COUNT(CASE WHEN bc.conclusion = 'failed' OR bc.conclusion = 'canceled' THEN 1 END)  AS checks_failed,
    COUNT(CASE WHEN bc.conclusion = 'succeeded' OR bc.conclusion = 'partiallySucceeded' THEN 1 END) AS checks_passed,
    COUNT(CASE WHEN bc.status <> 'completed' THEN 1 END) AS checks_pending,
    COUNT(bc.build_id) AS checks_total
FROM ado_pull_request pr
JOIN issue_ado_pull_request ipr ON ipr.pull_request_id = pr.id
LEFT JOIN ado_pull_request_build_check bc ON bc.pr_id = pr.id
WHERE ipr.issue_id = $1
GROUP BY pr.id
ORDER BY pr.pr_created_at DESC;

-- name: LinkIssueToADOPullRequest :exec
INSERT INTO issue_ado_pull_request (
    issue_id, pull_request_id, close_intent, linked_by_type, linked_by_id
) VALUES ($1, $2, $3, sqlc.narg('linked_by_type'), sqlc.narg('linked_by_id'))
ON CONFLICT (issue_id, pull_request_id) DO UPDATE SET
    close_intent = CASE WHEN $4 THEN EXCLUDED.close_intent ELSE issue_ado_pull_request.close_intent END;

-- name: GetADOIssuePullRequestCloseAggregate :one
SELECT
    COUNT(*) FILTER (WHERE pr.state IN ('open', 'draft')) AS open_count,
    COUNT(*) FILTER (WHERE pr.state = 'merged' AND ipr.close_intent = true) AS merged_with_close_intent_count
FROM issue_ado_pull_request ipr
JOIN ado_pull_request pr ON pr.id = ipr.pull_request_id
WHERE ipr.issue_id = $1;

-- name: ListIssueIDsForADOPullRequest :many
SELECT issue_id FROM issue_ado_pull_request WHERE pull_request_id = $1;

-- =====================
-- ADO Build Checks
-- =====================

-- name: UpsertADOBuildCheck :exec
INSERT INTO ado_pull_request_build_check (
    pr_id, build_id, definition_id, definition_name, conclusion, status, updated_at
) VALUES ($1, $2, $3, $4, sqlc.narg('conclusion'), $5, $6)
ON CONFLICT (pr_id, build_id) DO UPDATE SET
    conclusion      = EXCLUDED.conclusion,
    status          = EXCLUDED.status,
    definition_name = COALESCE(EXCLUDED.definition_name, ado_pull_request_build_check.definition_name),
    updated_at      = EXCLUDED.updated_at
WHERE EXCLUDED.updated_at >= ado_pull_request_build_check.updated_at;
