-- The squad directory (spec 3.5): what each squad takes on, its response time
-- and its open requests from other squads. Every query filters by workspace_id.

-- name: ListSquadDirectory :many
-- Live squads with their charter, floor name and open squad requests. A squad
-- without a charter reads the charter defaults.
SELECT
    s.id AS squad_id,
    s.name,
    s.floor_id,
    COALESCE(f.name, '')::text AS floor_name,
    COALESCE(c.mission, '')::text AS mission,
    COALESCE(c.intake, '')::text AS intake,
    COALESCE(c.response_hours, 48)::integer AS response_hours,
    (
        SELECT count(*) FROM issue r
        WHERE r.workspace_id = s.workspace_id
          AND r.origin_type = 'squad_request'
          AND r.assignee_type = 'squad'
          AND r.assignee_id = s.id
          AND r.status NOT IN ('done', 'cancelled')
    )::bigint AS open_requests
FROM squad s
LEFT JOIN squad_charter c ON c.squad_id = s.id AND c.workspace_id = s.workspace_id
LEFT JOIN office_floor f ON f.id = s.floor_id AND f.workspace_id = s.workspace_id
WHERE s.workspace_id = sqlc.arg('workspace_id')::uuid AND s.archived_at IS NULL
ORDER BY s.created_at ASC, s.id ASC;

-- name: ListOverdueSquadRequests :many
-- Open squad requests whose age has passed the receiving squad's response_hours.
-- The digest reads them for the boss's brief.
SELECT
    r.id AS issue_id,
    r.number,
    r.title,
    r.created_at,
    r.assignee_id AS squad_id,
    COALESCE(c.response_hours, 48)::integer AS response_hours
FROM issue r
LEFT JOIN squad_charter c ON c.squad_id = r.assignee_id AND c.workspace_id = r.workspace_id
WHERE r.workspace_id = sqlc.arg('workspace_id')::uuid
  AND r.origin_type = 'squad_request'
  AND r.assignee_type = 'squad'
  AND r.status NOT IN ('done', 'cancelled')
  AND r.created_at + make_interval(hours => COALESCE(c.response_hours, 48)) < sqlc.arg('now')::timestamptz
ORDER BY r.created_at ASC, r.id ASC
LIMIT 50;
