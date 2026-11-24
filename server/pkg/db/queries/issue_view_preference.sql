-- name: GetIssueViewPreference :one
SELECT * FROM issue_view_preference
WHERE workspace_id = $1 AND user_id = $2 AND scope_type = $3 AND scope_id = $4;

-- name: UpsertIssueViewPreference :one
-- Whole-document replace: single-user data, last-write-wins.
INSERT INTO issue_view_preference (workspace_id, user_id, scope_type, scope_id, prefs)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, user_id, scope_type, scope_id)
DO UPDATE SET prefs = EXCLUDED.prefs, updated_at = now()
RETURNING *;
