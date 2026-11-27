-- name: CreateProjectSpaceImport :one
INSERT INTO project_space_import (
  workspace_id, project_id, batch_name, status, total_files, total_bytes, created_by
) VALUES (
  $1, $2, $3, 'queued', $4, $5, $6
) RETURNING *;

-- name: CreateProjectSpaceImportFile :one
INSERT INTO project_space_import_file (
  import_id, workspace_id, project_id, relative_path, content_type, size_bytes
) VALUES (
  $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: GetProjectSpaceImport :one
SELECT * FROM project_space_import
WHERE id = $1 AND workspace_id = $2 AND project_id = $3;

-- name: ListProjectSpaceImports :many
SELECT * FROM project_space_import
WHERE workspace_id = $1 AND project_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: GetProjectSpaceImportFile :one
SELECT * FROM project_space_import_file
WHERE id = $1 AND import_id = $2 AND workspace_id = $3 AND project_id = $4;

-- name: ListProjectSpaceImportFiles :many
SELECT * FROM project_space_import_file
WHERE import_id = $1 AND workspace_id = $2 AND project_id = $3
ORDER BY created_at ASC;

-- name: MarkProjectSpaceImportUploading :exec
UPDATE project_space_import
SET status = CASE WHEN status = 'queued' THEN 'uploading' ELSE status END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND project_id = $3;

-- name: MarkProjectSpaceImportFileUploading :exec
UPDATE project_space_import_file
SET status = 'uploading', error_code = NULL, updated_at = now()
WHERE id = $1 AND import_id = $2 AND workspace_id = $3 AND project_id = $4
  AND status IN ('queued', 'failed', 'uploading');

-- name: CompleteProjectSpaceImportFile :exec
UPDATE project_space_import_file
SET status = $5,
    stored_relative_path = $6,
    sha256 = $7,
    content_type = $8,
    error_code = NULL,
    updated_at = now()
WHERE id = $1 AND import_id = $2 AND workspace_id = $3 AND project_id = $4;

-- name: FailProjectSpaceImportFile :exec
UPDATE project_space_import_file
SET status = 'failed', error_code = $5, updated_at = now()
WHERE id = $1 AND import_id = $2 AND workspace_id = $3 AND project_id = $4;

-- name: RefreshProjectSpaceImportCounts :one
UPDATE project_space_import psi
SET completed_files = counts.completed_files,
    failed_files = counts.failed_files,
    status = CASE
      WHEN counts.pending_files > 0 THEN 'uploading'
      WHEN counts.failed_files = 0 THEN 'completed'
      WHEN counts.completed_files > 0 THEN 'partial'
      ELSE 'failed'
    END,
    completed_at = CASE WHEN counts.pending_files = 0 THEN now() ELSE NULL END,
    updated_at = now()
FROM (
  SELECT
    import_id,
    count(*) FILTER (WHERE status IN ('completed', 'skipped'))::int AS completed_files,
    count(*) FILTER (WHERE status = 'failed')::int AS failed_files,
    count(*) FILTER (WHERE status IN ('queued', 'uploading'))::int AS pending_files
  FROM project_space_import_file
  WHERE import_id = $1
  GROUP BY import_id
) counts
WHERE psi.id = $1
  AND psi.workspace_id = $2
  AND psi.project_id = $3
  AND psi.id = counts.import_id
RETURNING psi.*;
