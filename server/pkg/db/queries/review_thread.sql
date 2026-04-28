-- name: UpsertReviewThread :one
INSERT INTO issue_review_thread (
    workspace_id, issue_id, pr_repo, pr_number,
    gh_comment_id, gh_thread_node_id,
    file_path, line, side, severity, title, body, url, author_login
) VALUES (
    $1, $2, $3, $4,
    $5, sqlc.narg('gh_thread_node_id')::text,
    $6, sqlc.narg('line')::int, sqlc.narg('side')::text, $7, $8, $9, $10, $11
)
ON CONFLICT (gh_comment_id) DO UPDATE SET
    body         = EXCLUDED.body,
    title        = EXCLUDED.title,
    severity     = EXCLUDED.severity,
    url          = EXCLUDED.url,
    file_path    = EXCLUDED.file_path,
    line         = EXCLUDED.line,
    side         = EXCLUDED.side,
    author_login = EXCLUDED.author_login,
    gh_thread_node_id = COALESCE(EXCLUDED.gh_thread_node_id, issue_review_thread.gh_thread_node_id),
    updated_at   = now()
RETURNING *;

-- name: GetReviewThreadByCommentID :one
SELECT * FROM issue_review_thread
WHERE gh_comment_id = $1;

-- name: ListReviewThreadsByIssue :many
SELECT * FROM issue_review_thread
WHERE issue_id = $1
ORDER BY created_at ASC;

-- name: ListUnresolvedReviewThreadsByIssue :many
SELECT * FROM issue_review_thread
WHERE issue_id = $1 AND state = 'unresolved'
ORDER BY created_at ASC;

-- name: CountUnresolvedReviewThreadsByIssue :one
SELECT COUNT(*) FROM issue_review_thread
WHERE issue_id = $1 AND state = 'unresolved';

-- name: SetReviewThreadStateByThreadNodeID :execrows
UPDATE issue_review_thread SET
    state             = $2,
    resolved_at       = CASE WHEN $2 = 'resolved' THEN now() ELSE NULL END,
    resolved_by_agent = CASE WHEN $2 = 'resolved' THEN sqlc.narg('agent_id')::uuid ELSE NULL END,
    updated_at        = now()
WHERE gh_thread_node_id = $1;

-- name: SetReviewThreadStateByPR :execrows
UPDATE issue_review_thread SET
    state      = $3,
    updated_at = now()
WHERE pr_repo = $1 AND pr_number = $2;

-- name: SetReviewThreadStateByCommentID :execrows
UPDATE issue_review_thread SET
    state             = $2,
    gh_thread_node_id = COALESCE(sqlc.narg('gh_thread_node_id')::text, gh_thread_node_id),
    resolved_at       = CASE WHEN $2 = 'resolved' THEN now() ELSE NULL END,
    resolved_by_agent = CASE WHEN $2 = 'resolved' THEN sqlc.narg('agent_id')::uuid ELSE NULL END,
    updated_at        = now()
WHERE gh_comment_id = $1;

-- name: BackfillReviewThreadNodeID :execrows
UPDATE issue_review_thread SET
    gh_thread_node_id = $2,
    updated_at        = now()
WHERE gh_comment_id = $1
  AND (gh_thread_node_id IS NULL OR gh_thread_node_id = '');
