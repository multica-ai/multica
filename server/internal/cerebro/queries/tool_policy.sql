-- Cerebro per-tool policy settings (FIR-2230 phase 1, persistence; FIR-2505
-- slice 1 added the resource_pattern dimension).
--
-- These queries back the unified tool-policy chain: each row is one layer's
-- explicit Allow/Ask/Deny/Inherit choice for one tool, optionally narrowed to a
-- specific resource_pattern (e.g. one repo URL). An empty resource_pattern is
-- the capability-wide row that pre-FIR-2505 callers wrote; a non-empty one
-- targets that exact pattern verbatim. ListCerebroToolPolicyForContext gathers
-- the five layers a single resolution needs in one round trip; the
-- per-subject / per-tool reads back the authoring tables in the admin UI.

-- name: ListCerebroToolPolicyForContext :many
-- All explicit settings that apply to one (tool, resource_pattern) for one
-- (workspace, runtime, agent, user, groups) context. An empty resource_pattern
-- argument selects only the capability-wide rows (the legacy shape); a
-- non-empty value selects rows for that exact pattern. A layer whose subject id
-- is NULL (absent from the request) never matches, so the resolver treats it as
-- Inherit. The workspace root layer is always keyed on the workspace itself, so
-- it enters every resolution. group_ids may be empty.
SELECT layer, subject_id, setting
FROM cerebro_tool_policy
WHERE workspace_id = sqlc.arg(workspace_id)
  AND tool_key = sqlc.arg(tool_key)
  AND resource_pattern = sqlc.arg(resource_pattern)
  AND (
    (layer = 'workspace' AND subject_id = sqlc.arg(workspace_id)) OR
    (layer = 'runtime'   AND subject_id = sqlc.arg(runtime_id)) OR
    (layer = 'agent'     AND subject_id = sqlc.arg(agent_id)) OR
    (layer = 'user'      AND subject_id = sqlc.arg(user_id)) OR
    (layer = 'group'     AND subject_id = ANY(sqlc.arg(group_ids)::uuid[]))
  );

-- name: UpsertCerebroToolPolicy :one
-- Set the explicit choice for one (tool, layer, subject, resource_pattern).
-- Re-setting the same quadruple overwrites the prior choice and records who
-- changed it.
INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, resource_pattern, setting, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern) DO UPDATE
SET setting = EXCLUDED.setting,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING id, workspace_id, tool_key, layer, subject_id, resource_pattern, setting, updated_by, created_at, updated_at;

-- name: DeleteCerebroToolPolicy :exec
-- Clear the explicit choice for one (tool, layer, subject, resource_pattern);
-- the layer falls back to Inherit for that pattern.
DELETE FROM cerebro_tool_policy
WHERE workspace_id = $1 AND tool_key = $2 AND layer = $3 AND subject_id = $4 AND resource_pattern = $5;

-- name: ListCerebroToolPolicyForSubject :many
-- Every explicit setting for one (layer, subject) across all tools and
-- resource patterns. Drives the "This agent" / "Runtime" column of the admin
-- table; the caller groups by (tool_key, resource_pattern) as needed.
SELECT tool_key, resource_pattern, setting, updated_by, updated_at
FROM cerebro_tool_policy
WHERE workspace_id = $1 AND layer = $2 AND subject_id = $3
ORDER BY tool_key ASC, resource_pattern ASC;
