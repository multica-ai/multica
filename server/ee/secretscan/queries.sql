-- name: GetSetting :one
SELECT * FROM secret_scan_setting WHERE workspace_id = $1;

-- name: EnsureSetting :one
INSERT INTO secret_scan_setting (workspace_id) VALUES ($1)
ON CONFLICT (workspace_id) DO UPDATE SET workspace_id = EXCLUDED.workspace_id
RETURNING *;

-- name: SetMode :one
INSERT INTO secret_scan_setting (workspace_id, mode) VALUES ($1, $2)
ON CONFLICT (workspace_id) DO UPDATE SET mode = EXCLUDED.mode, updated_at = now()
RETURNING *;

-- name: RecordFinding :exec
-- A secret someone marked as not a secret stays dismissed; one that is back
-- in the text after being redacted or edited out is open again.
INSERT INTO secret_finding (
    workspace_id, issue_id, comment_id, author_type, author_id,
    rule, masked, fingerprint, status, resolved_at
) VALUES (
    sqlc.arg(workspace_id), sqlc.arg(issue_id), sqlc.arg(comment_id), sqlc.arg(author_type), sqlc.arg(author_id),
    sqlc.arg(rule), sqlc.arg(masked), sqlc.arg(fingerprint), sqlc.arg(status),
    CASE WHEN sqlc.arg(status)::text = 'open' THEN NULL ELSE now() END
)
ON CONFLICT (comment_id, fingerprint) DO UPDATE SET
    status = EXCLUDED.status,
    resolved_by_type = NULL,
    resolved_by_id = NULL,
    resolved_at = EXCLUDED.resolved_at,
    updated_at = now()
WHERE secret_finding.status <> 'dismissed' AND secret_finding.status <> EXCLUDED.status;

-- name: ResolveMissingFindings :execrows
-- Open findings whose secret an edit took out of the comment.
UPDATE secret_finding SET
    status = 'resolved',
    resolved_at = now(),
    updated_at = now()
WHERE comment_id = sqlc.arg(comment_id)
  AND status = 'open'
  AND NOT (fingerprint = ANY(sqlc.arg(keep)::text[]));

-- name: ListFindings :many
SELECT sqlc.embed(f), i.number AS issue_number, i.title AS issue_title, i.project_id
FROM secret_finding f
JOIN comment c ON c.id = f.comment_id AND c.deleted_at IS NULL
JOIN issue i ON i.id = f.issue_id
WHERE f.workspace_id = $1 AND f.status = $2
ORDER BY f.created_at DESC
LIMIT $3;

-- name: CountFindings :many
-- Counts secrets, not comments: one pasted into three comments is one row
-- in the list.
SELECT f.status, i.project_id, count(DISTINCT f.fingerprint)::bigint AS total
FROM secret_finding f
JOIN comment c ON c.id = f.comment_id AND c.deleted_at IS NULL
JOIN issue i ON i.id = f.issue_id
WHERE f.workspace_id = $1
GROUP BY f.status, i.project_id;

-- name: ListOpenIssueFindings :many
SELECT f.*
FROM secret_finding f
JOIN comment c ON c.id = f.comment_id AND c.deleted_at IS NULL
WHERE f.workspace_id = $1 AND f.issue_id = $2 AND f.status = 'open'
ORDER BY f.created_at;

-- name: GetFindings :many
SELECT f.*, i.project_id
FROM secret_finding f
JOIN issue i ON i.id = f.issue_id
WHERE f.workspace_id = $1 AND f.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: SettleFindings :many
UPDATE secret_finding SET
    status = sqlc.arg(status),
    resolved_by_type = sqlc.arg(resolved_by_type),
    resolved_by_id = sqlc.arg(resolved_by_id),
    resolved_at = now(),
    updated_at = now()
WHERE workspace_id = sqlc.arg(workspace_id)
  AND id = ANY(sqlc.arg(ids)::uuid[])
  AND status = 'open'
RETURNING id, issue_id;

-- name: DeleteCommentFindings :exec
DELETE FROM secret_finding WHERE comment_id = ANY(sqlc.arg(comment_ids)::uuid[]);

-- name: DeleteIssueFindings :exec
DELETE FROM secret_finding WHERE issue_id = $1;

-- name: ListCommentInboxBodies :many
SELECT id, body FROM inbox_item
WHERE issue_id = sqlc.arg(issue_id)
  AND details->>'comment_id' = sqlc.arg(comment_id)::text
  AND body IS NOT NULL;

-- name: SetInboxBody :exec
UPDATE inbox_item SET body = $2 WHERE id = $1;

-- name: StartBackfill :one
-- A run whose server went away stops reporting progress; after ten quiet
-- minutes a new run may take over.
UPDATE secret_scan_setting SET
    backfill_status = 'running',
    backfill_cursor = NULL,
    backfill_scanned = 0,
    backfill_started_at = now(),
    backfill_finished_at = NULL,
    updated_at = now()
WHERE workspace_id = $1
  AND (backfill_status <> 'running' OR updated_at < now() - interval '10 minutes')
RETURNING *;

-- name: ListCommentsAfter :many
SELECT id, issue_id, author_type, author_id, content
FROM comment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(cursor)::uuid IS NULL OR id > sqlc.narg(cursor)::uuid)
ORDER BY id
LIMIT sqlc.arg(batch);

-- name: AdvanceBackfill :exec
UPDATE secret_scan_setting SET
    backfill_cursor = sqlc.arg(cursor),
    backfill_scanned = backfill_scanned + sqlc.arg(scanned)::bigint,
    updated_at = now()
WHERE workspace_id = sqlc.arg(workspace_id) AND backfill_status = 'running';

-- name: FinishBackfill :exec
UPDATE secret_scan_setting SET
    backfill_status = $2,
    backfill_finished_at = now(),
    updated_at = now()
WHERE workspace_id = $1 AND backfill_status = 'running';
