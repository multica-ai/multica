-- name: CreateGitHubInstallation :one
INSERT INTO github_installation (workspace_id, installation_id, account_login, account_type, app_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, installation_id) DO UPDATE
    SET account_login = EXCLUDED.account_login,
        account_type = EXCLUDED.account_type,
        updated_at = now()
RETURNING *;

-- name: GetGitHubInstallation :one
SELECT * FROM github_installation WHERE id = $1;

-- name: GetGitHubInstallationByInstallationID :one
SELECT * FROM github_installation WHERE installation_id = $1;

-- name: ListGitHubInstallations :many
SELECT * FROM github_installation WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: DeleteGitHubInstallation :exec
DELETE FROM github_installation WHERE id = $1;

-- name: DeleteGitHubInstallationByInstallationID :exec
DELETE FROM github_installation WHERE installation_id = $1;

-- name: UpsertIssuePullRequest :one
INSERT INTO issue_pull_request (issue_id, workspace_id, repo_owner, repo_name, pr_number, title, status, author, url, branch)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (issue_id, repo_owner, repo_name, pr_number) DO UPDATE
    SET title = EXCLUDED.title,
        status = EXCLUDED.status,
        author = EXCLUDED.author,
        url = EXCLUDED.url,
        branch = EXCLUDED.branch,
        updated_at = now()
RETURNING *;

-- name: UpdatePullRequestStatus :exec
UPDATE issue_pull_request
SET status = $1, updated_at = now()
WHERE repo_owner = $2 AND repo_name = $3 AND pr_number = $4;

-- name: ListPullRequestsByIssue :many
SELECT * FROM issue_pull_request WHERE issue_id = $1 ORDER BY created_at DESC;

-- name: ListPullRequestsByWorkspace :many
SELECT * FROM issue_pull_request WHERE workspace_id = $1 ORDER BY updated_at DESC;

-- name: FindIssuesByBranch :many
SELECT i.* FROM issue i
JOIN workspace w ON w.id = i.workspace_id
JOIN github_installation gi ON gi.workspace_id = w.id
WHERE gi.installation_id = $1
  AND i.status NOT IN ('done', 'cancelled')
ORDER BY i.updated_at DESC;

-- name: FindIssueByIdentifierNumber :one
SELECT * FROM issue
WHERE workspace_id = $1 AND number = $2;

-- name: DeleteIssuePullRequest :exec
DELETE FROM issue_pull_request WHERE id = $1;
