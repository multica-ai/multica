-- FIR-2816: Strategy and Rocks. Every public query is workspace-scoped.

-- name: GetOperatingSystemSettings :one
SELECT workspace_id, terminology, created_at, updated_at
FROM cerebro_operating_system_settings
WHERE workspace_id = $1;

-- name: UpsertOperatingSystemSettings :one
INSERT INTO cerebro_operating_system_settings (workspace_id, terminology)
VALUES ($1, $2)
ON CONFLICT (workspace_id) DO UPDATE
SET terminology = EXCLUDED.terminology,
    updated_at = now()
RETURNING workspace_id, terminology, created_at, updated_at;

-- name: CreateStrategyItem :one
INSERT INTO cerebro_strategy_item (
    workspace_id, kind, title, description, horizon_unit, horizon_count, position, state
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, workspace_id, kind, title, description, horizon_unit, horizon_count,
          position, state, created_at, updated_at;

-- name: GetStrategyItem :one
SELECT id, workspace_id, kind, title, description, horizon_unit, horizon_count,
       position, state, created_at, updated_at
FROM cerebro_strategy_item
WHERE id = $1 AND workspace_id = $2;

-- name: ListStrategyItems :many
SELECT id, workspace_id, kind, title, description, horizon_unit, horizon_count,
       position, state, created_at, updated_at
FROM cerebro_strategy_item
WHERE workspace_id = $1
ORDER BY position ASC, created_at ASC;

-- name: UpdateStrategyItem :one
UPDATE cerebro_strategy_item
SET kind = $3,
    title = $4,
    description = $5,
    horizon_unit = $6,
    horizon_count = $7,
    position = $8,
    state = $9,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, kind, title, description, horizon_unit, horizon_count,
          position, state, created_at, updated_at;

-- name: DeleteStrategyItem :execrows
DELETE FROM cerebro_strategy_item
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertRock :one
INSERT INTO cerebro_rock (
    project_id, workspace_id, period_start, period_end, confidence, reported_health
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (project_id) DO UPDATE
SET period_start = EXCLUDED.period_start,
    period_end = EXCLUDED.period_end,
    confidence = EXCLUDED.confidence,
    reported_health = EXCLUDED.reported_health,
    updated_at = now()
WHERE cerebro_rock.workspace_id = EXCLUDED.workspace_id
RETURNING project_id, workspace_id, period_start, period_end, confidence,
          reported_health, created_at, updated_at;

-- name: GetRock :one
SELECT project_id, workspace_id, period_start, period_end, confidence,
       reported_health, created_at, updated_at
FROM cerebro_rock
WHERE project_id = $1 AND workspace_id = $2;

-- name: DeleteRock :execrows
DELETE FROM cerebro_rock
WHERE project_id = $1 AND workspace_id = $2;

-- name: ListRockRollups :many
SELECT r.project_id, r.workspace_id, r.period_start, r.period_end, r.confidence,
       r.reported_health, r.created_at, r.updated_at,
       p.title AS project_title, p.description AS project_description,
       p.status AS project_status, p.lead_type, p.lead_id,
       COUNT(i.id)::integer AS issue_count,
       COUNT(i.id) FILTER (WHERE i.status = 'done')::integer AS done_issue_count,
       COUNT(i.id) FILTER (WHERE i.status = 'blocked')::integer AS blocked_issue_count
FROM cerebro_rock r
JOIN project p ON p.id = r.project_id AND p.workspace_id = r.workspace_id
LEFT JOIN issue i ON i.project_id = r.project_id AND i.workspace_id = r.workspace_id
WHERE r.workspace_id = $1
GROUP BY r.project_id, r.workspace_id, r.period_start, r.period_end, r.confidence,
         r.reported_health, r.created_at, r.updated_at,
         p.title, p.description, p.status, p.lead_type, p.lead_id
ORDER BY r.period_end ASC, p.title ASC;

-- name: CreateObjectConnection :one
INSERT INTO cerebro_object_connection (
    workspace_id, source_type, source_id, target_type, target_id,
    relationship_type, provenance, created_by_type, created_by_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, workspace_id, source_type, source_id, target_type, target_id,
          relationship_type, provenance, created_by_type, created_by_id, created_at;

-- name: ListObjectConnections :many
SELECT id, workspace_id, source_type, source_id, target_type, target_id,
       relationship_type, provenance, created_by_type, created_by_id, created_at
FROM cerebro_object_connection
WHERE workspace_id = $1
  AND (
      (source_type = $2 AND source_id = $3)
      OR (target_type = $2 AND target_id = $3)
  )
ORDER BY created_at ASC;

-- name: DeleteObjectConnection :execrows
DELETE FROM cerebro_object_connection
WHERE id = $1 AND workspace_id = $2;
