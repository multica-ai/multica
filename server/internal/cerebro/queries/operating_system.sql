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
    workspace_id, kind, title, description, horizon_unit, horizon_count, horizon_label, position, state
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, workspace_id, kind, title, description, horizon_unit, horizon_count, horizon_label,
          position, state, created_at, updated_at;

-- name: GetStrategyItem :one
SELECT id, workspace_id, kind, title, description, horizon_unit, horizon_count, horizon_label,
       position, state, created_at, updated_at
FROM cerebro_strategy_item
WHERE id = $1 AND workspace_id = $2;

-- name: ListStrategyItems :many
SELECT id, workspace_id, kind, title, description, horizon_unit, horizon_count, horizon_label,
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
    horizon_label = $8,
    position = $9,
    state = $10,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, kind, title, description, horizon_unit, horizon_count, horizon_label,
          position, state, created_at, updated_at;

-- name: DeleteStrategyItem :execrows
DELETE FROM cerebro_strategy_item
WHERE id = $1 AND workspace_id = $2;

-- name: ListStrategyItemHistory :many
SELECT id, strategy_item_id, action, title, snapshot, changed_at
FROM cerebro_strategy_item_history
WHERE workspace_id = $1
ORDER BY changed_at DESC, id DESC
LIMIT 100;

-- name: ListOperatingPeriods :many
SELECT id, workspace_id, name, unit, starts_on, ends_on, created_at, updated_at
FROM cerebro_operating_period
WHERE workspace_id = $1
ORDER BY starts_on DESC;

-- name: GetOperatingPeriod :one
SELECT id, workspace_id, name, unit, starts_on, ends_on, created_at, updated_at
FROM cerebro_operating_period
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertOperatingPeriod :one
INSERT INTO cerebro_operating_period (workspace_id, name, starts_on, ends_on)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, starts_on, ends_on) DO UPDATE
SET name = EXCLUDED.name, updated_at = now()
RETURNING id, workspace_id, name, unit, starts_on, ends_on, created_at, updated_at;

-- name: CreateOperatingPeriod :one
INSERT INTO cerebro_operating_period (workspace_id, name, unit, starts_on, ends_on)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, starts_on, ends_on) DO UPDATE
SET name = EXCLUDED.name, unit = EXCLUDED.unit, updated_at = now()
RETURNING id, workspace_id, name, unit, starts_on, ends_on, created_at, updated_at;

-- name: UpdateOperatingPeriod :one
UPDATE cerebro_operating_period
SET name = $3, unit = $4, starts_on = $5, ends_on = $6, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, name, unit, starts_on, ends_on, created_at, updated_at;

-- name: DeleteOperatingPeriod :execrows
DELETE FROM cerebro_operating_period
WHERE id = $1 AND workspace_id = $2;

-- name: ListOsElementSettings :many
SELECT workspace_id, element_key, enabled, created_at, updated_at
FROM cerebro_os_element_setting
WHERE workspace_id = $1
ORDER BY element_key;

-- name: UpsertOsElementSetting :one
INSERT INTO cerebro_os_element_setting (workspace_id, element_key, enabled)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, element_key) DO UPDATE
SET enabled = EXCLUDED.enabled, updated_at = now()
RETURNING workspace_id, element_key, enabled, created_at, updated_at;

-- name: CreateGoalType :one
INSERT INTO cerebro_goal_type (workspace_id, name, color, scope_label, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, workspace_id, name, color, scope_label, position, created_at, updated_at;

-- name: ListGoalTypes :many
SELECT id, workspace_id, name, color, scope_label, position, created_at, updated_at
FROM cerebro_goal_type
WHERE workspace_id = $1
ORDER BY position ASC, created_at ASC;

-- name: GetGoalType :one
SELECT id, workspace_id, name, color, scope_label, position, created_at, updated_at
FROM cerebro_goal_type
WHERE id = $1 AND workspace_id = $2;

-- name: UpdateGoalType :one
UPDATE cerebro_goal_type
SET name = $3, color = $4, scope_label = $5, position = $6, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, name, color, scope_label, position, created_at, updated_at;

-- name: DeleteGoalType :execrows
DELETE FROM cerebro_goal_type
WHERE id = $1 AND workspace_id = $2;

-- name: CreateRock :one
INSERT INTO cerebro_rock (
    workspace_id, title, description, owner_type, owner_id, period_id,
    period_start, period_end, confidence, reported_health, goal_type_id
)
SELECT sqlc.arg(workspace_id), sqlc.arg(title), sqlc.arg(description), sqlc.arg(owner_type), sqlc.arg(owner_id), op.id, op.starts_on, op.ends_on, sqlc.arg(confidence), sqlc.arg(reported_health), sqlc.narg(goal_type_id)
FROM cerebro_operating_period op
WHERE op.id = sqlc.arg(period_id) AND op.workspace_id = sqlc.arg(workspace_id)
RETURNING id, project_id, workspace_id, title, description, owner_type, owner_id,
          period_id, period_start, period_end, confidence, reported_health, goal_type_id, created_at, updated_at;

-- name: UpdateRock :one
UPDATE cerebro_rock r
SET title = $3,
    description = $4,
    owner_type = $5,
    owner_id = $6,
    period_id = $7,
    period_start = op.starts_on,
    period_end = op.ends_on,
    confidence = $8,
    reported_health = $9,
    goal_type_id = $10,
    updated_at = now()
FROM cerebro_operating_period op
WHERE r.id = $1 AND r.workspace_id = $2
  AND op.id = $7 AND op.workspace_id = $2
RETURNING r.id, r.project_id, r.workspace_id, r.title, r.description, r.owner_type, r.owner_id,
          r.period_id, r.period_start, r.period_end, r.confidence, r.reported_health, r.goal_type_id, r.created_at, r.updated_at;

-- name: UpsertLegacyRock :one
INSERT INTO cerebro_rock (
    project_id, workspace_id, title, description, owner_type, owner_id, period_id,
    period_start, period_end, confidence, reported_health
)
SELECT p.id, p.workspace_id, p.title, COALESCE(p.description, ''), p.lead_type, p.lead_id,
       op.id, op.starts_on, op.ends_on, $4, $5
FROM project p
JOIN cerebro_operating_period op ON op.id = $3 AND op.workspace_id = p.workspace_id
WHERE p.id = $1 AND p.workspace_id = $2
ON CONFLICT (project_id) WHERE project_id IS NOT NULL DO UPDATE
SET title = EXCLUDED.title,
    description = EXCLUDED.description,
    owner_type = EXCLUDED.owner_type,
    owner_id = EXCLUDED.owner_id,
    period_id = EXCLUDED.period_id,
    period_start = EXCLUDED.period_start,
    period_end = EXCLUDED.period_end,
    confidence = EXCLUDED.confidence,
    reported_health = EXCLUDED.reported_health,
    updated_at = now()
RETURNING id, project_id, workspace_id, title, description, owner_type, owner_id,
          period_id, period_start, period_end, confidence, reported_health, created_at, updated_at;

-- name: GetRock :one
SELECT id, project_id, workspace_id, title, description, owner_type, owner_id,
       period_id, period_start, period_end, confidence, reported_health, created_at, updated_at
FROM cerebro_rock
WHERE id = $1 AND workspace_id = $2;

-- name: DeleteRock :execrows
DELETE FROM cerebro_rock
WHERE id = $1 AND workspace_id = $2;

-- name: ListRockRollups :many
WITH rock_issue AS (
    SELECT c.source_id AS rock_id, i.id, i.status
    FROM cerebro_object_connection c
    JOIN issue i ON c.target_type = 'issue' AND i.id = c.target_id AND i.workspace_id = c.workspace_id
    WHERE c.source_type = 'rock' AND c.workspace_id = $1
    UNION
    SELECT c.source_id AS rock_id, i.id, i.status
    FROM cerebro_object_connection c
    JOIN issue i ON c.target_type = 'project' AND i.project_id = c.target_id AND i.workspace_id = c.workspace_id
    WHERE c.source_type = 'rock' AND c.workspace_id = $1
), rollup AS (
    SELECT rock_id,
           COUNT(*)::integer AS issue_count,
           COUNT(*) FILTER (WHERE status = 'done')::integer AS done_issue_count,
           COUNT(*) FILTER (WHERE status = 'blocked')::integer AS blocked_issue_count
    FROM rock_issue GROUP BY rock_id
)
SELECT r.id, r.project_id, r.workspace_id, r.title, r.description, r.owner_type, r.owner_id,
       COALESCE(u.name, a.name, '') AS owner_name,
       r.period_id, op.name AS period_name, r.period_start, r.period_end, r.confidence,
       r.reported_health, r.goal_type_id,
       COALESCE(gt.name, '') AS goal_type_name,
       COALESCE(gt.color, '') AS goal_type_color,
       COALESCE(gt.scope_label, '') AS goal_type_scope_label,
       r.created_at, r.updated_at,
       COALESCE(p.title, '') AS project_title, COALESCE(p.description, '') AS project_description,
       COALESCE(p.status, '') AS project_status,
       COALESCE(rollup.issue_count, 0)::integer AS issue_count,
       COALESCE(rollup.done_issue_count, 0)::integer AS done_issue_count,
       COALESCE(rollup.blocked_issue_count, 0)::integer AS blocked_issue_count,
       (SELECT COUNT(*)::integer FROM cerebro_object_connection c
        WHERE c.workspace_id = r.workspace_id AND c.source_type = 'rock'
          AND c.source_id = r.id AND c.target_type = 'project') AS project_count,
       COALESCE(si.id, '00000000-0000-0000-0000-000000000000'::uuid) AS strategy_item_id,
       COALESCE(si.title, '') AS strategy_item_title
FROM cerebro_rock r
JOIN cerebro_operating_period op ON op.id = r.period_id AND op.workspace_id = r.workspace_id
LEFT JOIN cerebro_goal_type gt ON gt.id = r.goal_type_id AND gt.workspace_id = r.workspace_id
LEFT JOIN project p ON p.id = r.project_id AND p.workspace_id = r.workspace_id
LEFT JOIN member m ON r.owner_type = 'member' AND m.id = r.owner_id AND m.workspace_id = r.workspace_id
LEFT JOIN "user" u ON u.id = m.user_id
LEFT JOIN agent a ON r.owner_type = 'agent' AND a.id = r.owner_id AND a.workspace_id = r.workspace_id
LEFT JOIN rollup ON rollup.rock_id = r.id
LEFT JOIN cerebro_object_connection sc ON sc.workspace_id = r.workspace_id
    AND sc.source_type = 'strategy_item' AND sc.target_type = 'rock' AND sc.target_id = r.id
LEFT JOIN cerebro_strategy_item si ON si.id = sc.source_id AND si.workspace_id = r.workspace_id
WHERE r.workspace_id = $1
ORDER BY r.period_end ASC, r.title ASC;

-- name: ListRockProjects :many
SELECT p.id, p.title,
       COUNT(i.id)::integer AS issue_count,
       COUNT(i.id) FILTER (WHERE i.status = 'done')::integer AS done_issue_count
FROM cerebro_object_connection c
JOIN project p ON c.target_type = 'project' AND p.id = c.target_id AND p.workspace_id = c.workspace_id
LEFT JOIN issue i ON i.project_id = p.id AND i.workspace_id = p.workspace_id
WHERE c.workspace_id = $1 AND c.source_type = 'rock' AND c.source_id = $2
GROUP BY p.id, p.title
ORDER BY p.title;

-- name: ListRockIssues :many
WITH connected_issue AS (
    SELECT i.id
    FROM cerebro_object_connection c
    JOIN issue i ON c.target_type = 'issue' AND i.id = c.target_id AND i.workspace_id = c.workspace_id
    WHERE c.workspace_id = $1 AND c.source_type = 'rock' AND c.source_id = $2
    UNION
    SELECT i.id
    FROM cerebro_object_connection c
    JOIN issue i ON c.target_type = 'project' AND i.project_id = c.target_id AND i.workspace_id = c.workspace_id
    WHERE c.workspace_id = $1 AND c.source_type = 'rock' AND c.source_id = $2
)
SELECT i.id, (w.issue_prefix || '-' || i.number)::text AS identifier, i.title, i.status, i.project_id, COALESCE(p.title, '') AS project_title
FROM connected_issue ci
JOIN issue i ON i.id = ci.id AND i.workspace_id = $1
JOIN workspace w ON w.id = i.workspace_id
LEFT JOIN project p ON p.id = i.project_id AND p.workspace_id = i.workspace_id
ORDER BY p.title NULLS LAST, i.position, i.created_at;

-- name: CreateRockCheckIn :one
INSERT INTO cerebro_rock_check_in (
    workspace_id, rock_id, confidence, reported_health, note, created_by_type, created_by_id
)
SELECT $1, r.id, $3, $4, $5, $6, $7
FROM cerebro_rock r
WHERE r.id = $2 AND r.workspace_id = $1
RETURNING id, workspace_id, rock_id, confidence, reported_health, note,
          created_by_type, created_by_id, created_at;

-- name: ListRockCheckIns :many
SELECT id, workspace_id, rock_id, confidence, reported_health, note,
       created_by_type, created_by_id, created_at
FROM cerebro_rock_check_in
WHERE workspace_id = $1 AND rock_id = $2
ORDER BY created_at DESC;

-- name: ApplyRockCheckIn :one
UPDATE cerebro_rock
SET confidence = $3, reported_health = $4, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id;

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

-- name: DeleteObjectConnectionsForSource :exec
DELETE FROM cerebro_object_connection
WHERE workspace_id = $1 AND source_type = $2 AND source_id = $3
  AND target_type = ANY(sqlc.arg('target_types')::text[]);

-- name: DeleteRockStrategyConnections :exec
DELETE FROM cerebro_object_connection
WHERE workspace_id = $1 AND source_type = 'strategy_item'
  AND target_type = 'rock' AND target_id = $2;
