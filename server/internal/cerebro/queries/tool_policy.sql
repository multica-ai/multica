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
-- (workspace, runtime, agent, user, groups, system) context. An empty
-- resource_pattern argument selects only the capability-wide rows (the legacy
-- shape); a non-empty value selects rows for that exact pattern. A layer whose
-- subject id is NULL (absent from the request) never matches, so the resolver
-- treats it as Inherit. The workspace root layer is always keyed on the
-- workspace itself, so it enters every resolution. group_ids may be empty. The
-- system layer is the mandate-actor ceiling for human-less runs (FIR-1609): its
-- subject is the autopilot id, so it only matches when a system run supplies one.
-- The on_behalf_of layer is the delegated member (the task initiator) as a real,
-- tighten-only actor level (FIR-2441): its subject is the human the work is
-- performed for, distinct from user (the agent owner), so it only matches when a
-- delegated run supplies one. This keeps the Resolve path (which powers the claim
-- brief) at parity with the TableQuery path that already honours it — listed ==
-- callable.
-- conditions is the optional WHEN layer (FIR-1609): the resolver evaluates it
-- against the request context and drops a row whose terms are not met, so the
-- Terms axis bites at the gate, not only in the admin table read.
SELECT layer, subject_id, setting, conditions
FROM cerebro_tool_policy
WHERE workspace_id = sqlc.arg(workspace_id)
  AND tool_key = sqlc.arg(tool_key)
  AND resource_pattern = sqlc.arg(resource_pattern)
  AND (
    (layer = 'workspace'    AND subject_id = sqlc.arg(workspace_id)) OR
    (layer = 'runtime'      AND subject_id = sqlc.arg(runtime_id)) OR
    (layer = 'agent'        AND subject_id = sqlc.arg(agent_id)) OR
    (layer = 'user'         AND subject_id = sqlc.arg(user_id)) OR
    (layer = 'group'        AND subject_id = ANY(sqlc.arg(group_ids)::uuid[])) OR
    (layer = 'system'       AND subject_id = sqlc.arg(system_id)) OR
    (layer = 'on_behalf_of' AND subject_id = sqlc.arg(on_behalf_of_id))
  );

-- name: UpsertCerebroToolPolicy :one
-- Set the explicit choice for one (tool, layer, subject, resource_pattern).
-- Re-setting the same quadruple overwrites the prior choice and records who
-- changed it.
INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, resource_pattern, setting, conditions, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern) DO UPDATE
SET setting = EXCLUDED.setting,
    conditions = EXCLUDED.conditions,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING id, workspace_id, tool_key, layer, subject_id, resource_pattern, setting, conditions, updated_by, created_at, updated_at;

-- name: DeleteCerebroToolPolicy :exec
-- Clear the explicit choice for one (tool, layer, subject, resource_pattern);
-- the layer falls back to Inherit for that pattern.
DELETE FROM cerebro_tool_policy
WHERE workspace_id = $1 AND tool_key = $2 AND layer = $3 AND subject_id = $4 AND resource_pattern = $5;

-- name: ListCerebroToolPolicyForSubject :many
-- Every explicit setting for one (layer, subject) across all tools and
-- resource patterns. Drives the "This agent" / "Runtime" column of the admin
-- table; the caller groups by (tool_key, resource_pattern) as needed.
SELECT tool_key, resource_pattern, setting, conditions, updated_by, updated_at
FROM cerebro_tool_policy
WHERE workspace_id = $1 AND layer = $2 AND subject_id = $3
ORDER BY tool_key ASC, resource_pattern ASC;

-- name: ListCerebroToolPolicyHolders :many
-- Every explicit setting for one tool across all layers and subjects in the
-- workspace — the reverse of ListCerebroToolPolicyForSubject. Backs the
-- per-permission detail page (FIR-3091 punkt 8): "who has this permission and
-- which layer grants it."
SELECT layer, subject_id, resource_pattern, setting, updated_by, updated_at
FROM cerebro_tool_policy
WHERE workspace_id = sqlc.arg(workspace_id) AND tool_key = sqlc.arg(tool_key)
ORDER BY layer ASC, subject_id ASC, resource_pattern ASC;
