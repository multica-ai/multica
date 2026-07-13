-- name: ListLabels :many
SELECT l.*,
    CASE l.resource_type
        WHEN 'issue' THEN (SELECT COUNT(*) FROM issue_to_label x WHERE x.label_id = l.id)
        WHEN 'agent' THEN (SELECT COUNT(*) FROM agent_to_label x WHERE x.label_id = l.id)
        WHEN 'skill' THEN (SELECT COUNT(*) FROM skill_to_label x WHERE x.label_id = l.id)
        ELSE 0
    END::bigint AS usage_count
FROM issue_label l
WHERE l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = sqlc.arg('resource_type')::text
ORDER BY LOWER(name) ASC;

-- name: GetLabel :one
SELECT * FROM issue_label
WHERE id = $1 AND workspace_id = $2;

-- name: CreateLabel :one
INSERT INTO issue_label (workspace_id, resource_type, name, description, color)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateLabel :one
UPDATE issue_label SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    color = COALESCE(sqlc.narg('color'), color),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteLabel :one
-- :one RETURNING id so the handler distinguishes pgx.ErrNoRows (→ 404) from
-- infrastructure errors (→ 500), and avoids a TOCTOU precheck.
DELETE FROM issue_label
WHERE id = $1 AND workspace_id = $2
RETURNING id;

-- The resource-label junctions deliberately have no foreign keys. Keeping
-- their cleanup in the same application transaction as the owner deletion
-- avoids database-level cascades with unreviewed locking and audit behavior.

-- name: DeleteIssueLabelAssignmentsByLabel :exec
DELETE FROM issue_to_label WHERE label_id = $1;

-- name: DeleteAgentLabelAssignmentsByLabel :exec
DELETE FROM agent_to_label WHERE label_id = $1;

-- name: DeleteSkillLabelAssignmentsByLabel :exec
DELETE FROM skill_to_label WHERE label_id = $1;

-- name: DeleteAgentLabelAssignmentsByAgent :exec
DELETE FROM agent_to_label WHERE agent_id = $1;

-- name: DeleteSkillLabelAssignmentsBySkill :exec
DELETE FROM skill_to_label WHERE skill_id = $1;

-- name: AttachLabelToIssue :exec
-- Workspace-guarded INSERT: the WHERE EXISTS clauses ensure both the issue
-- and the label belong to the given workspace. A future caller that forgets
-- handler-level prechecks still cannot attach labels across workspaces.
INSERT INTO issue_to_label (issue_id, label_id)
SELECT sqlc.arg('issue_id')::uuid, sqlc.arg('label_id')::uuid
WHERE EXISTS (
    SELECT 1 FROM issue i
    WHERE i.id = sqlc.arg('issue_id')::uuid
      AND i.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND EXISTS (
    SELECT 1 FROM issue_label l
    WHERE l.id = sqlc.arg('label_id')::uuid
      AND l.workspace_id = sqlc.arg('workspace_id')::uuid
      AND l.resource_type = 'issue'
)
ON CONFLICT DO NOTHING;

-- name: DetachLabelFromIssue :exec
-- Workspace-guarded DELETE: only deletes if the issue is in the given
-- workspace. Mirror of the attach query.
DELETE FROM issue_to_label
WHERE issue_id = sqlc.arg('issue_id')::uuid
  AND label_id = sqlc.arg('label_id')::uuid
  AND EXISTS (
      SELECT 1 FROM issue i
      WHERE i.id = sqlc.arg('issue_id')::uuid
        AND i.workspace_id = sqlc.arg('workspace_id')::uuid
  );

-- name: ListLabelsByIssue :many
-- Workspace filter at the SQL layer (mirrors GetProjectInWorkspace). Any caller
-- that passes the wrong workspace gets an empty list rather than leaking labels.
SELECT l.*
FROM issue_label l
JOIN issue_to_label il ON il.label_id = l.id
WHERE il.issue_id = sqlc.arg('issue_id')::uuid
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'issue'
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForIssues :many
-- Bulk variant: fetch labels for many issues in one round-trip so the issue
-- list endpoints can fold labels into each row without N+1 queries from the
-- client. Workspace-guarded the same way as ListLabelsByIssue.
SELECT il.issue_id, l.*
FROM issue_label l
JOIN issue_to_label il ON il.label_id = l.id
WHERE il.issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'issue'
ORDER BY il.issue_id, LOWER(l.name) ASC;

-- name: ListLabelsByAgent :many
SELECT l.*
FROM issue_label l
JOIN agent_to_label atl ON atl.label_id = l.id
WHERE atl.agent_id = sqlc.arg('agent_id')::uuid
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'agent'
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForAgents :many
SELECT atl.agent_id, l.*
FROM issue_label l
JOIN agent_to_label atl ON atl.label_id = l.id
WHERE atl.agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'agent'
ORDER BY atl.agent_id, LOWER(l.name) ASC;

-- name: AttachLabelToAgent :exec
INSERT INTO agent_to_label (agent_id, label_id)
SELECT sqlc.arg('agent_id')::uuid, sqlc.arg('label_id')::uuid
WHERE EXISTS (
    SELECT 1 FROM agent a
    WHERE a.id = sqlc.arg('agent_id')::uuid
      AND a.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND EXISTS (
    SELECT 1 FROM issue_label l
    WHERE l.id = sqlc.arg('label_id')::uuid
      AND l.workspace_id = sqlc.arg('workspace_id')::uuid
      AND l.resource_type = 'agent'
)
ON CONFLICT DO NOTHING;

-- name: DetachLabelFromAgent :exec
DELETE FROM agent_to_label
WHERE agent_id = sqlc.arg('agent_id')::uuid
  AND label_id = sqlc.arg('label_id')::uuid
  AND EXISTS (
      SELECT 1 FROM agent a
      WHERE a.id = sqlc.arg('agent_id')::uuid
        AND a.workspace_id = sqlc.arg('workspace_id')::uuid
  );

-- name: ListLabelsBySkill :many
SELECT l.*
FROM issue_label l
JOIN skill_to_label stl ON stl.label_id = l.id
WHERE stl.skill_id = sqlc.arg('skill_id')::uuid
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'skill'
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForSkills :many
SELECT stl.skill_id, l.*
FROM issue_label l
JOIN skill_to_label stl ON stl.label_id = l.id
WHERE stl.skill_id = ANY(sqlc.arg('skill_ids')::uuid[])
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'skill'
ORDER BY stl.skill_id, LOWER(l.name) ASC;

-- name: AttachLabelToSkill :exec
INSERT INTO skill_to_label (skill_id, label_id)
SELECT sqlc.arg('skill_id')::uuid, sqlc.arg('label_id')::uuid
WHERE EXISTS (
    SELECT 1 FROM skill s
    WHERE s.id = sqlc.arg('skill_id')::uuid
      AND s.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND EXISTS (
    SELECT 1 FROM issue_label l
    WHERE l.id = sqlc.arg('label_id')::uuid
      AND l.workspace_id = sqlc.arg('workspace_id')::uuid
      AND l.resource_type = 'skill'
)
ON CONFLICT DO NOTHING;

-- name: DetachLabelFromSkill :exec
DELETE FROM skill_to_label
WHERE skill_id = sqlc.arg('skill_id')::uuid
  AND label_id = sqlc.arg('label_id')::uuid
  AND EXISTS (
      SELECT 1 FROM skill s
      WHERE s.id = sqlc.arg('skill_id')::uuid
        AND s.workspace_id = sqlc.arg('workspace_id')::uuid
  );
