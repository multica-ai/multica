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

-- name: EnsureDefaultVisionPlanSections :exec
INSERT INTO cerebro_vision_plan_section (workspace_id, key, name, section_type, position)
SELECT sqlc.arg('workspace_id')::uuid, defaults.key, defaults.name, defaults.section_type, defaults.position
FROM (VALUES
    ('core-values', 'Core Values', 'list', 0),
    ('core-focus', 'Core Focus', 'list', 1),
    ('long-term-target', 'Long-Term Target', 'list', 2),
    ('marketing-strategy', 'Marketing Strategy', 'structured', 3),
    ('three-year-picture', 'Three-Year Picture', 'list', 4),
    ('one-year-plan', 'One-Year Plan', 'list', 5),
    ('quarterly-goals', 'Quarterly Goals', 'list', 6),
    ('issues-list', 'Issues List', 'list', 7),
    ('core-processes', 'Core Processes', 'process', 8)
) AS defaults(key, name, section_type, position)
ON CONFLICT (workspace_id, key) DO NOTHING;

-- name: EnsureLegacyVisionPlanSections :exec
INSERT INTO cerebro_vision_plan_section (workspace_id, key, name, section_type, position)
SELECT DISTINCT item.workspace_id,
       'legacy-horizon-' || md5(COALESCE(NULLIF(item.horizon_label, ''), item.horizon_count::text || '-' || item.horizon_unit)),
       COALESCE(NULLIF(item.horizon_label, ''), item.horizon_count::text || '-' || initcap(item.horizon_unit) || ' Horizon'),
       'list', 6
FROM cerebro_strategy_item item
WHERE item.workspace_id = $1 AND item.section_id IS NULL
  AND item.kind = 'horizon_goal'
  AND NOT (item.horizon_unit = 'year' AND item.horizon_count IN (1, 3))
  AND NOT (item.horizon_unit = 'year' AND item.horizon_count >= 4)
ON CONFLICT (workspace_id, key) DO NOTHING;

-- name: AssignLegacyVisionPlanItems :exec
UPDATE cerebro_strategy_item item
SET section_id = section.id
FROM cerebro_vision_plan_section section
WHERE item.workspace_id = $1 AND item.section_id IS NULL
  AND section.workspace_id = item.workspace_id
  AND section.key = CASE
      WHEN item.kind = 'core_value' THEN 'core-values'
      WHEN item.kind = 'core_focus' THEN 'core-focus'
      WHEN item.horizon_unit = 'year' AND item.horizon_count >= 4 THEN 'long-term-target'
      WHEN item.horizon_unit = 'year' AND item.horizon_count = 3 THEN 'three-year-picture'
      WHEN item.horizon_unit = 'year' AND item.horizon_count = 1 THEN 'one-year-plan'
      ELSE 'legacy-horizon-' || md5(COALESCE(NULLIF(item.horizon_label, ''), item.horizon_count::text || '-' || item.horizon_unit))
  END;

-- Seeded once, on the workspace's first read. A page the workspace deletes
-- afterwards must stay deleted, so this never tops the set back up.
-- name: EnsureDefaultVisionPlanPages :exec
INSERT INTO cerebro_vision_plan_page (workspace_id, key, name, column_count, position)
SELECT sqlc.arg('workspace_id')::uuid, defaults.key, defaults.name, defaults.column_count, defaults.position
FROM (VALUES
    ('vision', 'Vision', 2, 0),
    ('traction', 'Traction', 3, 1)
) AS defaults(key, name, column_count, position)
WHERE NOT EXISTS (
    SELECT 1 FROM cerebro_vision_plan_page existing
    WHERE existing.workspace_id = sqlc.arg('workspace_id')::uuid
)
ON CONFLICT (workspace_id, key) DO NOTHING;

-- Runs before the sections are assigned to pages, so "Traction has no blocks"
-- is only true on the first read.
-- name: EnsureDefaultVisionPlanGoalsBlock :exec
INSERT INTO cerebro_vision_plan_section (workspace_id, key, name, section_type, position, page_id, column_index)
SELECT page.workspace_id, 'goals-board', 'Goals', 'goals', 0, page.id, 1
FROM cerebro_vision_plan_page page
WHERE page.workspace_id = $1 AND page.key = 'traction'
  AND NOT EXISTS (
      SELECT 1 FROM cerebro_vision_plan_section block WHERE block.page_id = page.id
  )
ON CONFLICT (workspace_id, key) DO NOTHING;

-- name: AssignDefaultVisionPlanSectionPages :exec
UPDATE cerebro_vision_plan_section section
SET page_id = page.id, column_index = layout.column_index, position = layout.position
FROM (VALUES
    ('core-values', 'vision', 0, 0),
    ('core-focus', 'vision', 0, 1),
    ('long-term-target', 'vision', 0, 2),
    ('marketing-strategy', 'vision', 0, 3),
    ('three-year-picture', 'vision', 1, 0),
    ('core-processes', 'vision', 1, 1),
    ('one-year-plan', 'traction', 0, 0),
    ('quarterly-goals', 'traction', 0, 1),
    ('issues-list', 'traction', 2, 0)
) AS layout(section_key, page_key, column_index, position),
     cerebro_vision_plan_page page
WHERE section.workspace_id = $1 AND section.page_id IS NULL
  AND section.key = layout.section_key
  AND page.workspace_id = section.workspace_id
  AND page.key = layout.page_key;

-- name: AssignRemainingVisionPlanSectionPages :exec
UPDATE cerebro_vision_plan_section section
SET page_id = page.id, column_index = 0
FROM cerebro_vision_plan_page page
WHERE section.workspace_id = $1 AND section.page_id IS NULL
  AND page.workspace_id = section.workspace_id
  AND page.id = (
      SELECT candidate.id FROM cerebro_vision_plan_page candidate
      WHERE candidate.workspace_id = section.workspace_id
      ORDER BY candidate.position ASC, candidate.created_at ASC
      LIMIT 1
  );

-- name: ListVisionPlanPages :many
SELECT id, workspace_id, key, name, column_count, position, created_at, updated_at
FROM cerebro_vision_plan_page
WHERE workspace_id = $1
ORDER BY position ASC, created_at ASC;

-- name: CreateVisionPlanPage :one
INSERT INTO cerebro_vision_plan_page (workspace_id, key, name, column_count, position)
VALUES ($1, 'page-' || gen_random_uuid()::text, $2, $3, $4)
RETURNING id, workspace_id, key, name, column_count, position, created_at, updated_at;

-- name: UpdateVisionPlanPage :one
UPDATE cerebro_vision_plan_page
SET name = $3, column_count = $4, position = $5, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, key, name, column_count, position, created_at, updated_at;

-- name: DeleteVisionPlanPage :execrows
DELETE FROM cerebro_vision_plan_page page
WHERE page.id = $1 AND page.workspace_id = $2
  AND (SELECT count(*) FROM cerebro_vision_plan_page other WHERE other.workspace_id = page.workspace_id) > 1;

-- name: ListVisionPlanSections :many
SELECT id, workspace_id, key, name, section_type, position, page_id, column_index, created_at, updated_at
FROM cerebro_vision_plan_section
WHERE workspace_id = $1
ORDER BY column_index ASC, position ASC, created_at ASC;

-- name: CreateVisionPlanSection :one
INSERT INTO cerebro_vision_plan_section (workspace_id, key, name, section_type, position, page_id, column_index)
SELECT page.workspace_id, 'custom-' || gen_random_uuid()::text,
       sqlc.arg('name'), sqlc.arg('section_type'), sqlc.arg('position'), page.id, sqlc.arg('column_index')
FROM cerebro_vision_plan_page page
WHERE page.id = sqlc.arg('page_id') AND page.workspace_id = sqlc.arg('workspace_id')
RETURNING id, workspace_id, key, name, section_type, position, page_id, column_index, created_at, updated_at;

-- name: UpdateVisionPlanSection :one
UPDATE cerebro_vision_plan_section section
SET name = $3, section_type = $4, position = $5, page_id = $6, column_index = $7, updated_at = now()
WHERE section.id = $1 AND section.workspace_id = $2
  AND EXISTS (
      SELECT 1 FROM cerebro_vision_plan_page page
      WHERE page.id = $6 AND page.workspace_id = section.workspace_id
  )
RETURNING id, workspace_id, key, name, section_type, position, page_id, column_index, created_at, updated_at;

-- name: DeleteVisionPlanSection :execrows
DELETE FROM cerebro_vision_plan_section
WHERE id = $1 AND workspace_id = $2;

-- name: ListVisionPlanItems :many
SELECT item.id, item.workspace_id, item.section_id, item.title, item.description,
       item.part_label, item.owner_type, item.owner_id,
       COALESCE(u.name, a.name, '') AS owner_name,
       item.position, item.state, item.created_at, item.updated_at
FROM cerebro_strategy_item item
JOIN cerebro_vision_plan_section section
  ON section.id = item.section_id AND section.workspace_id = item.workspace_id
LEFT JOIN member m ON item.owner_type = 'member' AND m.id = item.owner_id AND m.workspace_id = item.workspace_id
LEFT JOIN "user" u ON u.id = m.user_id
LEFT JOIN agent a ON item.owner_type = 'agent' AND a.id = item.owner_id AND a.workspace_id = item.workspace_id
WHERE item.workspace_id = $1
ORDER BY section.position ASC, item.position ASC, item.created_at ASC;

-- name: CreateVisionPlanItem :one
INSERT INTO cerebro_strategy_item (
    workspace_id, kind, title, description, position, state,
    section_id, part_label, owner_type, owner_id
)
SELECT section.workspace_id, 'core_value', $3, $4, $5, $6,
       section.id, $7, $8, $9
FROM cerebro_vision_plan_section section
WHERE section.id = $1 AND section.workspace_id = $2
RETURNING id, workspace_id, section_id, title, description, part_label,
          owner_type, owner_id, position, state, created_at, updated_at;

-- name: UpdateVisionPlanItem :one
UPDATE cerebro_strategy_item item
SET section_id = $3, title = $4, description = $5, position = $6,
    state = $7, part_label = $8, owner_type = $9, owner_id = $10,
    updated_at = now()
WHERE item.id = $1 AND item.workspace_id = $2
  AND EXISTS (
      SELECT 1 FROM cerebro_vision_plan_section section
      WHERE section.id = $3 AND section.workspace_id = item.workspace_id
  )
RETURNING id, workspace_id, section_id, title, description, part_label,
          owner_type, owner_id, position, state, created_at, updated_at;

-- name: ListVisionPlanGoalConnections :many
SELECT id, source_id AS strategy_item_id, target_id AS goal_id
FROM cerebro_object_connection
WHERE workspace_id = $1 AND source_type = 'strategy_item'
  AND target_type = 'rock' AND relationship_type = 'supports'
ORDER BY created_at ASC;

-- name: ListVisionPlanObjectLinks :many
SELECT c.id, c.source_id AS strategy_item_id, c.target_type, c.target_id,
       COALESCE(p.title, i.title, '') AS target_title,
       COALESCE(w.issue_prefix || '-' || i.number, '')::text AS target_identifier
FROM cerebro_object_connection c
LEFT JOIN project p ON c.target_type = 'project' AND p.id = c.target_id AND p.workspace_id = c.workspace_id
LEFT JOIN issue i ON c.target_type = 'issue' AND i.id = c.target_id AND i.workspace_id = c.workspace_id
LEFT JOIN workspace w ON w.id = c.workspace_id
WHERE c.workspace_id = $1 AND c.source_type = 'strategy_item'
  AND c.target_type IN ('project', 'issue')
ORDER BY c.created_at ASC;

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

-- FIR-3421 Stage 4: Meetings.

-- name: GetOperatingMeeting :one
SELECT workspace_id, note_type_id, cadence_unit, cadence_count, agenda, created_at, updated_at
FROM cerebro_operating_meeting
WHERE workspace_id = $1;

-- name: UpsertOperatingMeeting :one
INSERT INTO cerebro_operating_meeting (
    workspace_id, note_type_id, cadence_unit, cadence_count, agenda
)
VALUES ($1, sqlc.narg(note_type_id), $2, $3, $4)
ON CONFLICT (workspace_id) DO UPDATE SET
    note_type_id = EXCLUDED.note_type_id,
    cadence_unit = EXCLUDED.cadence_unit,
    cadence_count = EXCLUDED.cadence_count,
    agenda = EXCLUDED.agenda,
    updated_at = now()
RETURNING workspace_id, note_type_id, cadence_unit, cadence_count, agenda, created_at, updated_at;

-- FIR-3421 Stage 4: Org Chart.

-- name: ListOrgChartSeats :many
SELECT seat.id, seat.workspace_id, seat.parent_id, seat.name, seat.responsibilities,
       seat.owner_type, seat.owner_id,
       COALESCE(u.name, a.name, '') AS owner_name,
       seat.position, seat.created_at, seat.updated_at
FROM cerebro_org_chart_seat seat
LEFT JOIN member m ON seat.owner_type = 'member' AND m.id = seat.owner_id AND m.workspace_id = seat.workspace_id
LEFT JOIN "user" u ON u.id = m.user_id
LEFT JOIN agent a ON seat.owner_type = 'agent' AND a.id = seat.owner_id AND a.workspace_id = seat.workspace_id
WHERE seat.workspace_id = $1
ORDER BY seat.parent_id NULLS FIRST, seat.position ASC, seat.created_at ASC;

-- name: CreateOrgChartSeat :one
INSERT INTO cerebro_org_chart_seat (
    workspace_id, parent_id, name, responsibilities, owner_type, owner_id, position
)
VALUES ($1, sqlc.narg(parent_id), $2, $3, sqlc.narg(owner_type), sqlc.narg(owner_id), $4)
RETURNING id, workspace_id, parent_id, name, responsibilities, owner_type, owner_id,
          ''::text AS owner_name, position, created_at, updated_at;

-- name: UpdateOrgChartSeat :one
UPDATE cerebro_org_chart_seat
SET parent_id = sqlc.narg(parent_id),
    name = $3,
    responsibilities = $4,
    owner_type = sqlc.narg(owner_type),
    owner_id = sqlc.narg(owner_id),
    position = $5,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, parent_id, name, responsibilities, owner_type, owner_id,
          ''::text AS owner_name, position, created_at, updated_at;

-- name: DeleteOrgChartSeat :execrows
DELETE FROM cerebro_org_chart_seat
WHERE id = $1 AND workspace_id = $2;
