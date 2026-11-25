-- name: ListProjectFolders :many
SELECT * FROM project_folder
WHERE workspace_id = $1
ORDER BY LOWER(name), created_at;

-- name: ListFiledProjects :many
SELECT id, folder_id FROM project
WHERE workspace_id = $1 AND folder_id IS NOT NULL;

-- name: CreateProjectFolder :one
INSERT INTO project_folder (workspace_id, name, creator_type, creator_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: RenameProjectFolder :one
UPDATE project_folder SET
    name = $3,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteProjectFolder :one
DELETE FROM project_folder
WHERE id = $1 AND workspace_id = $2
RETURNING id;

-- Filing a project is not an edit of the project, so neither query touches
-- updated_at (and with it the project's last activity).

-- name: ClearProjectFolder :exec
UPDATE project SET folder_id = NULL
WHERE workspace_id = $1 AND folder_id = $2;

-- name: FileProject :execrows
-- Affects no row when the folder is not in the workspace.
UPDATE project SET folder_id = sqlc.arg('folder_id')
WHERE project.id = sqlc.arg('project_id') AND project.workspace_id = sqlc.arg('workspace_id')
  AND EXISTS (
    SELECT 1 FROM project_folder f
    WHERE f.id = sqlc.arg('folder_id') AND f.workspace_id = sqlc.arg('workspace_id')
  );

-- name: UnfileProject :exec
UPDATE project SET folder_id = NULL
WHERE id = $1 AND workspace_id = $2;
