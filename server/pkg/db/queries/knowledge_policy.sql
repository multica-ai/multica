-- name: ListKnowledgePublishPolicies :many
SELECT * FROM knowledge_publish_policy WHERE workspace_id = $1 ORDER BY scope ASC, scope_key ASC;

-- name: UpsertKnowledgePublishPolicy :one
INSERT INTO knowledge_publish_policy (workspace_id, scope, scope_key, mode, updated_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, scope, scope_key) DO UPDATE SET
    mode = EXCLUDED.mode,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING *;

-- name: DeleteKnowledgePublishPolicy :execrows
DELETE FROM knowledge_publish_policy WHERE workspace_id = $1 AND scope = $2 AND scope_key = $3;
