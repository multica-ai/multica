-- name: UpsertReviewThread :one
INSERT INTO issue_review_thread (
    workspace_id, issue_id, pr_repo, pr_number,
    gh_comment_id, gh_thread_node_id,
    file_path, line, side, severity, title, body, url, author_login,
    severity_badge, effort_badge, ai_prompt
) VALUES (
    $1, $2, $3, $4,
    $5, sqlc.narg('gh_thread_node_id')::text,
    $6, sqlc.narg('line')::int, sqlc.narg('side')::text, $7, $8, $9, $10, $11,
    $12, $13, $14
)
ON CONFLICT (gh_comment_id) DO UPDATE SET
    body           = EXCLUDED.body,
    title          = EXCLUDED.title,
    severity       = EXCLUDED.severity,
    severity_badge = EXCLUDED.severity_badge,
    effort_badge   = EXCLUDED.effort_badge,
    ai_prompt      = EXCLUDED.ai_prompt,
    url            = EXCLUDED.url,
    file_path      = EXCLUDED.file_path,
    line           = EXCLUDED.line,
    side           = EXCLUDED.side,
    author_login   = EXCLUDED.author_login,
    gh_thread_node_id = COALESCE(EXCLUDED.gh_thread_node_id, issue_review_thread.gh_thread_node_id),
    updated_at     = now()
RETURNING *;

-- name: GetReviewThreadByCommentID :one
SELECT * FROM issue_review_thread
WHERE gh_comment_id = $1;

-- name: GetReviewThreadInIssue :one
SELECT * FROM issue_review_thread
WHERE id = $1 AND issue_id = $2;

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

-- name: ListStuckCoderabbitIssues :many
-- Issues that have been parked in `coderabbit` long enough that we suspect
-- CodeRabbit streamed inline comments without ever submitting a wrapping
-- pull_request_review (rare, but observed on long PRs). The settle-window
-- sweeper runs this on a cadence and forces the coderabbit → resolving
-- transition for any matching issue.
--
-- Match conditions:
--   1. issue is currently in `coderabbit`
--   2. issue has at least one unresolved CR thread
--   3. the most recent CR thread is at least @settle_seconds old
--      (we hold off on issues whose CR comments are still landing — the
--      normal pull_request_review.submitted will fire shortly)
SELECT
    i.id          AS issue_id,
    i.workspace_id,
    i.title,
    i.pr_repo,
    i.pr_number,
    COUNT(t.*)::int AS unresolved_count,
    MAX(t.created_at) AS last_cr_comment_at
FROM issue i
JOIN issue_review_thread t ON t.issue_id = i.id
WHERE i.status = 'coderabbit'
  AND t.state = 'unresolved'
GROUP BY i.id
HAVING MAX(t.created_at) < now() - make_interval(secs => sqlc.arg(settle_seconds)::int)
ORDER BY i.id;

-- name: BackfillReviewThreadNodeID :execrows
UPDATE issue_review_thread SET
    gh_thread_node_id = $2,
    updated_at        = now()
WHERE gh_comment_id = $1
  AND (gh_thread_node_id IS NULL OR gh_thread_node_id = '');

-- name: ClaimNextUnprocessedThread :one
WITH next_thread AS (
    SELECT rt.id
    FROM issue_review_thread rt
    WHERE rt.issue_id = $1
      AND rt.state = 'unresolved'
      AND rt.processed_by_resolver_at IS NULL
      AND (rt.claim_expires_at IS NULL OR rt.claim_expires_at < now())
    ORDER BY
      CASE rt.severity
        WHEN 'issue'      THEN 0
        WHEN 'refactor'   THEN 1
        WHEN 'suggestion' THEN 2
        WHEN 'nitpick'    THEN 3
        ELSE 4
      END,
      rt.file_path ASC,
      rt.line ASC NULLS LAST
    LIMIT 1
    FOR UPDATE SKIP LOCKED
), claimed AS (
    UPDATE issue_review_thread t
    SET claimed_by_agent = $2,
        claim_expires_at = now() + make_interval(secs => sqlc.arg(claim_ttl_secs)::int),
        updated_at       = now()
    FROM next_thread
    WHERE t.id = next_thread.id
    RETURNING t.*
)
SELECT * FROM claimed;

-- name: ReleaseThreadClaim :exec
UPDATE issue_review_thread
SET claimed_by_agent = NULL,
    claim_expires_at = NULL,
    updated_at       = now()
WHERE id = $1 AND claimed_by_agent = $2;

-- name: SweepExpiredThreadClaims :execrows
UPDATE issue_review_thread
SET claimed_by_agent = NULL,
    claim_expires_at = NULL,
    updated_at       = now()
WHERE claim_expires_at IS NOT NULL
  AND claim_expires_at < now()
  AND processed_by_resolver_at IS NULL;

-- name: MarkThreadProcessedByResolver :one
UPDATE issue_review_thread
SET processed_by_resolver_at = now(),
    processed_by_agent       = $2,
    claimed_by_agent         = NULL,
    claim_expires_at         = NULL,
    updated_at               = now()
WHERE id = $1
  AND processed_by_resolver_at IS NULL
  AND claimed_by_agent = $2
  AND claim_expires_at IS NOT NULL
  AND claim_expires_at > now()
RETURNING *;

-- name: ListThreadsWithUnpostedFixerReplies :many
SELECT
    t.id                AS thread_id,
    t.gh_thread_node_id,
    t.state             AS thread_state,
    t.severity,
    t.file_path,
    t.line,
    reply.id            AS fixer_reply_comment_id,
    reply.content       AS reply_body
FROM issue_review_thread t
JOIN comment parent ON parent.review_thread_id = t.id AND parent.type = 'cr_review_comment'
JOIN comment reply  ON reply.parent_id = parent.id   AND reply.type  = 'fixer_reply'
WHERE t.issue_id = $1
  AND reply.posted_to_github_at IS NULL
ORDER BY
  CASE t.severity
    WHEN 'issue'      THEN 0
    WHEN 'refactor'   THEN 1
    WHEN 'suggestion' THEN 2
    WHEN 'nitpick'    THEN 3
    ELSE 4
  END,
  t.file_path ASC,
  t.line ASC NULLS LAST,
  reply.created_at ASC;

-- name: GetParentCRReviewCommentForThread :one
SELECT id FROM comment
WHERE review_thread_id = $1 AND type = 'cr_review_comment'
LIMIT 1;
