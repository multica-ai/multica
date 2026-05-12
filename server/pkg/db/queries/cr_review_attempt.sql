-- name: UpsertCRReviewAttempt :one
INSERT INTO cr_review_attempt (issue_id, workspace_id, cr_round, pr_url, head_sha)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (issue_id, cr_round) DO UPDATE SET
    pr_url = EXCLUDED.pr_url,
    head_sha = COALESCE(NULLIF(EXCLUDED.head_sha, ''), cr_review_attempt.head_sha)
RETURNING *;

-- name: RecordCRWrappingReview :one
UPDATE cr_review_attempt
SET review_submitted_at = now(),
    review_state        = $3,
    findings_count      = $4
WHERE issue_id = $1 AND cr_round = $2
RETURNING *;

-- name: CloseCRReviewAttempt :one
UPDATE cr_review_attempt
SET outcome        = $3,
    outcome_reason = $4,
    closed_at      = now()
WHERE issue_id = $1 AND cr_round = $2 AND closed_at IS NULL
RETURNING *;

-- name: CloseLatestOpenCRReviewAttemptForIssue :one
UPDATE cr_review_attempt
SET outcome        = $2,
    outcome_reason = $3,
    closed_at      = now()
WHERE id = (
    SELECT id FROM cr_review_attempt
    WHERE cr_review_attempt.issue_id = $1 AND cr_review_attempt.closed_at IS NULL
    ORDER BY cr_round DESC
    LIMIT 1
)
RETURNING *;

-- name: ListPendingCommentedApprovals :many
SELECT a.id AS attempt_id, a.issue_id, a.workspace_id, a.cr_round,
       a.pr_url, i.pr_repo::text AS pr_repo, i.pr_number::int AS pr_number
FROM cr_review_attempt a
JOIN issue i ON i.id = a.issue_id
WHERE a.review_state = 'commented'
  AND a.outcome IS NULL
  AND i.status = 'coderabbit'
  AND i.pr_repo IS NOT NULL
  AND i.pr_number IS NOT NULL
  AND a.review_submitted_at < now() - make_interval(secs => sqlc.arg(settle_seconds)::int)
ORDER BY a.review_submitted_at ASC;

-- name: GetCRReviewAttempt :one
SELECT * FROM cr_review_attempt
WHERE issue_id = $1 AND cr_round = $2;

-- name: GetCRReviewAttemptByID :one
SELECT * FROM cr_review_attempt
WHERE id = $1;

-- name: RecordCRFirstSignal :exec
UPDATE cr_review_attempt
SET first_signal_at   = COALESCE(first_signal_at, now()),
    first_signal_kind = COALESCE(first_signal_kind, $3)
WHERE issue_id = $1 AND cr_round = $2;

-- name: ListSilentCoderabbitIssues :many
SELECT
    i.id                              AS issue_id,
    i.workspace_id,
    i.pr_repo::text                   AS pr_repo,
    i.pr_number::int                  AS pr_number,
    i.updated_at,
    COALESCE(b.cr_required, true)     AS cr_required,
    a.id                              AS attempt_id,
    COALESCE(a.cr_round, 0)::int      AS cr_round
FROM issue i
LEFT JOIN cr_review_attempt a
       ON a.issue_id = i.id
      AND a.cr_round = (SELECT COALESCE(MAX(cr_round), 0) FROM cr_review_attempt WHERE issue_id = i.id)
LEFT JOIN workspace_repo_binding b ON b.repo_full_name = i.pr_repo
WHERE i.status = 'coderabbit'
  AND i.pr_repo IS NOT NULL
  AND i.pr_number IS NOT NULL
  AND (a.first_signal_at IS NULL OR a.id IS NULL)
  AND i.updated_at < now() - make_interval(secs => sqlc.arg(no_review_seconds)::int);

-- name: ListCRAttemptsForIssue :many
SELECT * FROM cr_review_attempt
WHERE issue_id = $1
ORDER BY cr_round DESC;
