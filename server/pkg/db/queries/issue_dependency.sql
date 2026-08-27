-- name: CreateIssueDependency :one
INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type) VALUES ($1,$2,'blocked_by') RETURNING *;
-- name: DeleteIssueDependency :exec
DELETE FROM issue_dependency WHERE issue_id=$1 AND depends_on_issue_id=$2 AND type='blocked_by';
-- name: ListIssueDependencies :many
SELECT * FROM issue_dependency WHERE issue_id=$1 AND type='blocked_by';
-- name: ListIssueDependencyDependents :many
SELECT * FROM issue_dependency WHERE depends_on_issue_id=$1 AND type='blocked_by';

