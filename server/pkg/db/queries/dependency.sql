-- name: CreateIssueDependency :one
INSERT INTO issue_dependency (
    issue_id, depends_on_issue_id, type
) VALUES (
    $1, $2, $3
) RETURNING *;

-- name: GetIssueDependency :one
SELECT * FROM issue_dependency
WHERE id = $1;

-- name: DeleteIssueDependency :exec
DELETE FROM issue_dependency
WHERE id = $1;

-- name: ListIssueDependenciesForIssue :many
SELECT * FROM issue_dependency
WHERE issue_id = $1 OR depends_on_issue_id = $1
ORDER BY id;
