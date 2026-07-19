-- name: LockIssueExternalIdentityKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('lock_key')::text, 7135));

-- name: GetIssueExternalIdentityForUpdate :one
SELECT *
FROM issue_external_identity
WHERE workspace_id = $1 AND namespace = $2 AND external_id = $3
FOR UPDATE;

-- name: GetExternalIdentityTargetIssueForUpdate :one
-- Hold the parent issue until alias insertion commits. Without this lock, an
-- application-owned delete could remove the issue after validation but before
-- the alias insert, leaving an orphan because this table intentionally has no FK.
SELECT * FROM issue
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: InsertIssueExternalIdentity :exec
INSERT INTO issue_external_identity (workspace_id, namespace, external_id, issue_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, namespace, external_id) DO NOTHING;

-- name: DeleteIssueExternalIdentitiesByIssue :exec
-- Relationships are application-owned: delete aliases in the same transaction
-- as the workspace-guarded parent issue deletion.
DELETE FROM issue_external_identity
WHERE workspace_id = $1 AND issue_id = $2;

-- name: MergeIssueMetadataPatch :one
UPDATE issue
SET metadata = metadata || sqlc.arg('patch')::jsonb,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND (
      SELECT count(*)
      FROM jsonb_object_keys(metadata || sqlc.arg('patch')::jsonb)
  ) <= 50
  AND pg_column_size(metadata || sqlc.arg('patch')::jsonb) <= 8192
RETURNING *;
