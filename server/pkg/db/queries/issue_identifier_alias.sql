-- name: UpsertIssueIdentifierAlias :exec
INSERT INTO issue_identifier_alias (workspace_id, space_key_lower, number, issue_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, space_key_lower, number)
DO UPDATE SET issue_id = EXCLUDED.issue_id;

-- name: GetIssueByIdentifierAlias :one
SELECT i.* FROM issue_identifier_alias a
JOIN issue i ON i.id = a.issue_id
WHERE a.workspace_id = $1
  AND a.space_key_lower = $2
  AND a.number = $3;
