-- Cerebro runtime tool registry + access grants (JEH-1710).
--
-- These queries support the admin UI for "scan, vis og styr" of runtime
-- tools: list the unified tool inventory per runtime, toggle a tool on/off,
-- add/remove group and user access, and read the per-agent override.

-- name: ListCerebroRuntimeTools :many
SELECT id, runtime_id, tool_name, source, mcp_server_name, description,
       schema_json, enabled, last_scanned_at, created_at, updated_at
FROM cerebro_runtime_tool
WHERE runtime_id = $1
ORDER BY source ASC, lower(tool_name) ASC;

-- name: GetCerebroRuntimeTool :one
SELECT id, runtime_id, tool_name, source, mcp_server_name, description,
       schema_json, enabled, last_scanned_at, created_at, updated_at
FROM cerebro_runtime_tool
WHERE runtime_id = $1 AND tool_name = $2;

-- name: UpsertCerebroRuntimeTool :one
-- Insert or update a tool row. Used by the scan path to refresh the inventory
-- (preserves enabled flag if row already exists) and by manual seed paths.
INSERT INTO cerebro_runtime_tool (runtime_id, tool_name, source, mcp_server_name,
                                  description, schema_json, enabled, last_scanned_at)
VALUES ($1, $2, $3, $4, $5, $6, COALESCE(sqlc.narg('enabled')::boolean, false), $7)
ON CONFLICT (runtime_id, tool_name, mcp_server_name) DO UPDATE
SET source = EXCLUDED.source,
    description = EXCLUDED.description,
    schema_json = EXCLUDED.schema_json,
    last_scanned_at = EXCLUDED.last_scanned_at,
    updated_at = now()
RETURNING id, runtime_id, tool_name, source, mcp_server_name, description,
          schema_json, enabled, last_scanned_at, created_at, updated_at;

-- name: SetCerebroRuntimeToolEnabled :one
UPDATE cerebro_runtime_tool
SET enabled = $3, updated_at = now()
WHERE runtime_id = $1 AND tool_name = $2
RETURNING id, runtime_id, tool_name, source, mcp_server_name, description,
          schema_json, enabled, last_scanned_at, created_at, updated_at;

-- name: DeleteCerebroRuntimeTool :exec
DELETE FROM cerebro_runtime_tool
WHERE runtime_id = $1 AND tool_name = $2;

-- name: ListCerebroRuntimeToolGroupGrants :many
SELECT gg.runtime_id, gg.tool_name, gg.group_id, gg.granted_by, gg.granted_at,
       g.name AS group_name
FROM cerebro_runtime_tool_group_grant gg
JOIN cerebro_group g ON g.id = gg.group_id
WHERE gg.runtime_id = $1
ORDER BY gg.tool_name ASC, lower(g.name) ASC;

-- name: AddCerebroRuntimeToolGroupGrant :exec
INSERT INTO cerebro_runtime_tool_group_grant (runtime_id, tool_name, group_id, granted_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (runtime_id, tool_name, group_id) DO NOTHING;

-- name: RemoveCerebroRuntimeToolGroupGrant :exec
DELETE FROM cerebro_runtime_tool_group_grant
WHERE runtime_id = $1 AND tool_name = $2 AND group_id = $3;

-- name: ListCerebroRuntimeToolUserGrants :many
SELECT ug.runtime_id, ug.tool_name, ug.user_id, ug.granted_by, ug.granted_at,
       u.name AS user_name, u.email AS user_email, u.avatar_url AS user_avatar_url
FROM cerebro_runtime_tool_user_grant ug
JOIN "user" u ON u.id = ug.user_id
WHERE ug.runtime_id = $1
ORDER BY ug.tool_name ASC, lower(u.name) ASC;

-- name: AddCerebroRuntimeToolUserGrant :exec
INSERT INTO cerebro_runtime_tool_user_grant (runtime_id, tool_name, user_id, granted_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (runtime_id, tool_name, user_id) DO NOTHING;

-- name: RemoveCerebroRuntimeToolUserGrant :exec
DELETE FROM cerebro_runtime_tool_user_grant
WHERE runtime_id = $1 AND tool_name = $2 AND user_id = $3;

-- name: ListCerebroAgentRuntimeToolOverrides :many
SELECT agent_id, tool_name, enabled, updated_by, updated_at
FROM cerebro_agent_runtime_tool_override
WHERE agent_id = $1
ORDER BY tool_name ASC;

-- name: UpsertCerebroAgentRuntimeToolOverride :one
INSERT INTO cerebro_agent_runtime_tool_override (agent_id, tool_name, enabled, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (agent_id, tool_name) DO UPDATE
SET enabled = EXCLUDED.enabled,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING agent_id, tool_name, enabled, updated_by, updated_at;

-- name: DeleteCerebroAgentRuntimeToolOverride :exec
DELETE FROM cerebro_agent_runtime_tool_override
WHERE agent_id = $1 AND tool_name = $2;

-- name: ResolveCerebroAgentToolAccess :many
-- Returns the set of (tool_name) callable by the given agent on its runtime
-- for the given acting user. Implements the default-deny rule from
-- 9032_cerebro_runtime_tool_grants:
--
--   tool is callable iff
--     runtime_tool.enabled = true
--     AND (user has a direct user_grant OR user is in a group with group_grant
--          OR the user is workspace owner/admin)
--     AND (no override OR override.enabled = true)
SELECT rt.tool_name, rt.source, rt.mcp_server_name
FROM cerebro_runtime_tool rt
JOIN agent a ON a.runtime_id = rt.runtime_id
LEFT JOIN cerebro_agent_runtime_tool_override ov
       ON ov.agent_id = a.id AND ov.tool_name = rt.tool_name
LEFT JOIN member wm
       ON wm.workspace_id = a.workspace_id AND wm.user_id = $2
WHERE a.id = $1
  AND rt.enabled = true
  AND (ov.tool_name IS NULL OR ov.enabled = true)
  AND (
    wm.role IN ('owner', 'admin')
    OR EXISTS (
        SELECT 1 FROM cerebro_runtime_tool_user_grant ug
        WHERE ug.runtime_id = rt.runtime_id
          AND ug.tool_name = rt.tool_name
          AND ug.user_id = $2
    )
    OR EXISTS (
        SELECT 1 FROM cerebro_runtime_tool_group_grant gg
        JOIN cerebro_group_member gm ON gm.group_id = gg.group_id
        WHERE gg.runtime_id = rt.runtime_id
          AND gg.tool_name = rt.tool_name
          AND gm.user_id = $2
    )
  )
ORDER BY rt.tool_name ASC;
