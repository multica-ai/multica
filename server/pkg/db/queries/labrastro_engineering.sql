-- name: LabrastroLockEngineeringProject :one
-- Serializes baseline decisions, configuration changes and project deletion.
SELECT * FROM project WHERE workspace_id = $1 AND id = $2 FOR UPDATE;

-- name: LabrastroReadEngineeringProject :one
SELECT * FROM project WHERE workspace_id = $1 AND id = $2 FOR SHARE;

-- name: LabrastroLockEngineeringMember :one
SELECT * FROM member WHERE workspace_id = $1 AND user_id = $2 FOR SHARE;

-- name: LabrastroGetEngineering :one
SELECT * FROM project_engineering WHERE workspace_id = $1 AND project_id = $2;

-- name: LabrastroPutEngineering :one
INSERT INTO project_engineering (workspace_id, project_id, enabled, current_stage, mode_summary,
    decider_member_ids, updated_by_type, updated_by_id, stages)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (workspace_id, project_id) DO UPDATE SET
    enabled = EXCLUDED.enabled, current_stage = EXCLUDED.current_stage, mode_summary = EXCLUDED.mode_summary,
    decider_member_ids = EXCLUDED.decider_member_ids, stages = EXCLUDED.stages,
    updated_by_type = EXCLUDED.updated_by_type, updated_by_id = EXCLUDED.updated_by_id, updated_at = now()
RETURNING *;

-- name: LabrastroEnsureBaselineDoc :exec
INSERT INTO project_baseline_doc (workspace_id, project_id, chapter) VALUES ($1,$2,$3)
ON CONFLICT (workspace_id, project_id, chapter) DO NOTHING;

-- name: LabrastroLockBaselineDocs :many
SELECT * FROM project_baseline_doc WHERE workspace_id = $1 AND project_id = $2 ORDER BY chapter FOR UPDATE;

-- name: LabrastroListBaselineDocs :many
SELECT * FROM project_baseline_doc WHERE workspace_id = $1 AND project_id = $2 ORDER BY chapter;

-- name: LabrastroGetBaselineRevision :one
SELECT * FROM project_baseline_revision WHERE workspace_id = $1 AND project_id = $2 AND id = $3;

-- name: LabrastroGetBaselineRevisionNumber :one
SELECT * FROM project_baseline_revision WHERE workspace_id = $1 AND project_id = $2 AND chapter = $3 AND revision_number = $4;

-- name: LabrastroListBaselineRevisions :many
SELECT * FROM project_baseline_revision WHERE workspace_id = $1 AND project_id = $2 AND chapter = $3
    AND (sqlc.narg('before_number')::int IS NULL OR revision_number < sqlc.narg('before_number'))
ORDER BY revision_number DESC LIMIT sqlc.arg('page_limit');

-- name: LabrastroCreateBaselineRevision :one
INSERT INTO project_baseline_revision (workspace_id, project_id, chapter, revision_number, status,
    content_markdown, change_summary, base_revision_id, replaces_revision_id, anchors_diff, source_issue_id,
    created_by_type, created_by_id, created_by_task_id)
VALUES (sqlc.arg('workspace_id'), sqlc.arg('project_id'), sqlc.arg('chapter'),
    (SELECT COALESCE(MAX(r.revision_number),0)::int + 1 FROM project_baseline_revision r
     WHERE r.workspace_id = sqlc.arg('workspace_id') AND r.project_id = sqlc.arg('project_id') AND r.chapter = sqlc.arg('chapter')),
    'candidate', sqlc.arg('content_markdown'), sqlc.arg('change_summary'), sqlc.narg('base_revision_id'),
    sqlc.narg('replaces_revision_id'), sqlc.arg('anchors_diff'), sqlc.narg('source_issue_id'),
    sqlc.arg('created_by_type'), sqlc.arg('created_by_id'), sqlc.narg('created_by_task_id')) RETURNING *;

-- name: LabrastroRetireBaselineRevision :exec
UPDATE project_baseline_revision SET status = $4 WHERE workspace_id = $1 AND project_id = $2 AND id = $3;

-- name: LabrastroDecideBaselineRevision :one
UPDATE project_baseline_revision SET status = $4, decided_by_type = $5, decided_by_id = $6,
    decided_by_task_id = $7, decision_credential = $8, decided_at = now()
WHERE workspace_id = $1 AND project_id = $2 AND id = $3 RETURNING *;

-- name: LabrastroUpdateBaselineDoc :exec
UPDATE project_baseline_doc SET adopted_revision_id = $4, candidate_revision_id = $5, updated_at = now()
WHERE workspace_id = $1 AND project_id = $2 AND chapter = $3;

-- name: LabrastroBaselineOpenIssues :many
SELECT i.id, i.title, i.status, i.number, i.assignee_type, i.assignee_id
FROM issue i WHERE i.workspace_id = $1 AND i.project_id = $2 AND i.status = ANY(sqlc.arg('status_keys')::text[])
ORDER BY i.created_at, i.id LIMIT 201;

-- name: LabrastroDeleteProjectEngineering :exec
WITH revisions AS (DELETE FROM project_baseline_revision WHERE project_baseline_revision.workspace_id = $1 AND project_baseline_revision.project_id = $2),
docs AS (DELETE FROM project_baseline_doc WHERE project_baseline_doc.workspace_id = $1 AND project_baseline_doc.project_id = $2)
DELETE FROM project_engineering WHERE project_engineering.workspace_id = $1 AND project_engineering.project_id = $2;

-- name: LabrastroDeleteWorkspaceEngineering :exec
WITH revisions AS (DELETE FROM project_baseline_revision WHERE project_baseline_revision.workspace_id = $1),
docs AS (DELETE FROM project_baseline_doc WHERE project_baseline_doc.workspace_id = $1)
DELETE FROM project_engineering WHERE project_engineering.workspace_id = $1;
