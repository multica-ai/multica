-- name: ListKnowledgeKinds :many
SELECT * FROM knowledge_kind WHERE workspace_id = $1 ORDER BY key ASC;

-- name: GetKnowledgeKind :one
SELECT * FROM knowledge_kind WHERE workspace_id = $1 AND key = $2;

-- name: SeedKnowledgeKind :execrows
-- Provisioning and the backfill insert the default kinds. An existing row, which
-- an admin may have edited, is left alone.
INSERT INTO knowledge_kind (workspace_id, key, name, default_base_key, template, review_days, required_sections)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, key) DO NOTHING;

-- name: UpdateKnowledgeKind :one
UPDATE knowledge_kind SET
    name = $3,
    template = $4,
    review_days = $5,
    required_sections = $6,
    default_base_key = $7,
    updated_at = now()
WHERE workspace_id = $1 AND key = $2
RETURNING *;

-- name: ListReadyKnowledgeWorkspaceIDs :many
-- The workspaces whose knowledge is provisioned: the jobs and the backfill visit
-- only these.
SELECT workspace_id FROM workspace_knowledge WHERE state = 'ready' ORDER BY workspace_id;
