-- name: CreateReviewAsset :one
INSERT INTO review_assets (
  issue_id, workspace_id, name, asset_type, file_url, thumbnail_url, width, height, duration, version, uploaded_by, asset_group_id
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: GetReviewAsset :one
SELECT * FROM review_assets WHERE id = $1;

-- name: ListReviewAssetsByIssue :many
SELECT * FROM review_assets WHERE issue_id = $1 ORDER BY created_at DESC;

-- name: ListPendingReviewIssueIDs :many
SELECT DISTINCT issue_id FROM review_assets WHERE workspace_id = $1 AND status != 'approved';

-- name: UpdateReviewAssetStatus :one
UPDATE review_assets SET status = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ListReviewAssetVersions :many
SELECT * FROM review_assets WHERE asset_group_id = $1 ORDER BY version DESC;

-- name: BulkApproveReviewAssets :exec
UPDATE review_assets SET status = 'approved', updated_at = now() WHERE issue_id = $1 AND status = 'pending';

-- name: DeleteReviewAsset :exec
DELETE FROM review_assets WHERE id = $1;

-- name: DeleteReviewAssetGroup :exec
DELETE FROM review_assets WHERE asset_group_id = $1;

-- name: CreateReviewComment :one
INSERT INTO review_comments (
  asset_id, author_id, content, timestamp, shapes, parent_id
) VALUES (
  $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: GetReviewComment :one
SELECT * FROM review_comments WHERE id = $1;

-- name: ListReviewCommentsByAsset :many
SELECT * FROM review_comments WHERE asset_id = $1 ORDER BY created_at ASC;

-- name: UpdateReviewComment :one
UPDATE review_comments SET content = $2, shapes = $3, timestamp = $4, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ResolveReviewComment :one
UPDATE review_comments SET resolved = true, resolved_by = $2, resolved_at = now(), updated_at = now() WHERE id = $1 RETURNING *;

-- name: UnresolveReviewComment :one
UPDATE review_comments SET resolved = false, resolved_by = NULL, resolved_at = NULL, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteReviewComment :exec
DELETE FROM review_comments WHERE id = $1;
