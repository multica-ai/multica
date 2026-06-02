-- =====================
-- GitHub PR Review
-- =====================

-- name: UpsertGitHubPRReview :one
INSERT INTO github_pr_review (
    pr_id, review_id, reviewer_login, reviewer_avatar_url,
    state, body, submitted_at
) VALUES (
    $1, $2, $3, sqlc.narg('reviewer_avatar_url'),
    $4, sqlc.narg('body'), $5
)
ON CONFLICT (pr_id, review_id) DO UPDATE SET
    reviewer_login = EXCLUDED.reviewer_login,
    reviewer_avatar_url = EXCLUDED.reviewer_avatar_url,
    state = EXCLUDED.state,
    body = EXCLUDED.body,
    submitted_at = EXCLUDED.submitted_at,
    updated_at = now()
RETURNING *;

-- name: ListPRReviewsByPR :many
SELECT * FROM github_pr_review
WHERE pr_id = $1
ORDER BY submitted_at DESC;

-- name: ListPRReviewsByIssue :many
-- Aggregated reviews across all PRs linked to an issue, newest first.
-- Returns review rows joined with PR context so the frontend can group by PR.
SELECT
    r.id, r.pr_id, r.review_id, r.reviewer_login, r.reviewer_avatar_url,
    r.state, r.body, r.submitted_at, r.created_at, r.updated_at,
    pr.repo_owner, pr.repo_name, pr.pr_number, pr.html_url AS pr_html_url
FROM github_pr_review r
JOIN github_pull_request pr ON pr.id = r.pr_id
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE ipr.issue_id = $1
ORDER BY r.submitted_at DESC;

-- name: GetLatestReviewPerPRByIssue :many
-- For each PR linked to the issue, returns the most recent review per
-- reviewer so the UI can show an aggregate approval badge (N approved,
-- M changes requested). Uses DISTINCT ON for the latest-per-reviewer
-- pattern.
SELECT DISTINCT ON (r.pr_id, r.reviewer_login)
    r.pr_id, r.reviewer_login, r.reviewer_avatar_url, r.state,
    r.submitted_at,
    pr.repo_owner, pr.repo_name, pr.pr_number, pr.html_url AS pr_html_url
FROM github_pr_review r
JOIN github_pull_request pr ON pr.id = r.pr_id
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
WHERE ipr.issue_id = $1
ORDER BY r.pr_id, r.reviewer_login, r.submitted_at DESC;
