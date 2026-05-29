-- Cerebro per-tool policy settings (FIR-2230 phase 1, persistence).
--
-- These queries back the unified tool-policy chain: each row is one layer's
-- explicit Allow/Ask/Deny/Inherit choice for one tool. ListCerebroToolPolicyForContext
-- gathers the five layers a single resolution needs in one round trip; the
-- per-subject / per-tool reads back the authoring tables in the admin UI.

-- name: ListCerebroToolPolicyForContext :many
-- All explicit settings that apply to one tool for one (workspace, runtime,
-- agent, user, groups) context. A layer whose subject id is NULL (absent from
-- the request) never matches, so the resolver treats it as Inherit. The
-- workspace root layer is always keyed on the workspace itself, so it enters
-- every resolution. group_ids may be empty.
SELECT layer, subject_id, setting
FROM cerebro_tool_policy
WHERE workspace_id = sqlc.arg(workspace_id)
  AND tool_key = sqlc.arg(tool_key)
  AND (
    (layer = 'workspace' AND subject_id = sqlc.arg(workspace_id)) OR
    (layer = 'runtime'   AND subject_id = sqlc.arg(runtime_id)) OR
    (layer = 'agent'     AND subject_id = sqlc.arg(agent_id)) OR
    (layer = 'user'      AND subject_id = sqlc.arg(user_id)) OR
    (layer = 'group'     AND subject_id = ANY(sqlc.arg(group_ids)::uuid[]))
  );

-- name: UpsertCerebroToolPolicy :one
-- Set the explicit choice for one (tool, layer, subject). Re-setting the same
-- triple overwrites the prior choice and records who changed it.
INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, setting, updated_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, tool_key, layer, subject_id) DO UPDATE
SET setting = EXCLUDED.setting,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING id, workspace_id, tool_key, layer, subject_id, setting, updated_by, created_at, updated_at;

-- name: DeleteCerebroToolPolicy :exec
-- Clear the explicit choice for one (tool, layer, subject); the layer falls back
-- to Inherit.
DELETE FROM cerebro_tool_policy
WHERE workspace_id = $1 AND tool_key = $2 AND layer = $3 AND subject_id = $4;

-- name: ListCerebroToolPolicyForSubject :many
-- Every explicit setting for one (layer, subject) across all tools. Drives the
-- "This agent" / "Runtime" column of the admin table.
SELECT tool_key, setting, updated_by, updated_at
FROM cerebro_tool_policy
WHERE workspace_id = $1 AND layer = $2 AND subject_id = $3
ORDER BY tool_key ASC;
