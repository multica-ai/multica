-- Cerebro per-credential policy settings (FIR-1479 — credentials as a
-- first-class permission type, write-API slice).
--
-- Each row is one layer's explicit Allow/Deny/Inherit choice for one credential
-- (one Agent Vault box, keyed on credential_key — e.g. 'bigquery',
-- 'personal-jesper', a per-repo box). The rows deliberately mirror
-- cerebro_tool_policy so the permissions interface resolves credentials through
-- the same five-layer chain it uses for tools (workspace → runtime → agent →
-- group → user/system). Unlike tool policy there is no resource_pattern and no
-- 'ask' setting: a box is either reachable or not.
--
-- ListCerebroCredentialPolicyForContext gathers the layers a single resolution
-- needs in one round trip (the resolver folds them via credentialpolicy.Resolve);
-- the per-subject read backs the authoring column in the admin UI.

-- name: ListCerebroCredentialPolicyForContext :many
-- All explicit settings that apply to one credential for one (workspace,
-- runtime, agent, user, groups, system) context. A layer whose subject id is
-- NULL (absent from the request) never matches, so the resolver treats it as
-- Inherit. The workspace root layer is always keyed on the workspace itself, so
-- it enters every resolution. group_ids may be empty. The system layer is the
-- mandate-actor ceiling for human-less runs: its subject is the autopilot id, so
-- it only matches when a system run supplies one.
SELECT layer, subject_id, setting
FROM cerebro_credential_policy
WHERE workspace_id = sqlc.arg(workspace_id)
  AND credential_key = sqlc.arg(credential_key)
  AND (
    (layer = 'workspace' AND subject_id = sqlc.arg(workspace_id)) OR
    (layer = 'runtime'   AND subject_id = sqlc.arg(runtime_id)) OR
    (layer = 'agent'     AND subject_id = sqlc.arg(agent_id)) OR
    (layer = 'user'      AND subject_id = sqlc.arg(user_id)) OR
    (layer = 'group'     AND subject_id = ANY(sqlc.arg(group_ids)::uuid[])) OR
    (layer = 'system'    AND subject_id = sqlc.arg(system_id))
  );

-- name: UpsertCerebroCredentialPolicy :one
-- Set the explicit choice for one (credential, layer, subject). Re-setting the
-- same triple overwrites the prior choice and records who changed it.
INSERT INTO cerebro_credential_policy (workspace_id, credential_key, layer, subject_id, setting, updated_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, credential_key, layer, subject_id) DO UPDATE
SET setting = EXCLUDED.setting,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING id, workspace_id, credential_key, layer, subject_id, setting, updated_by, created_at, updated_at;

-- name: DeleteCerebroCredentialPolicy :exec
-- Clear the explicit choice for one (credential, layer, subject); the layer
-- falls back to Inherit, which folds to the deny-by-default base.
DELETE FROM cerebro_credential_policy
WHERE workspace_id = $1 AND credential_key = $2 AND layer = $3 AND subject_id = $4;

-- name: ListCerebroCredentialPolicyForSubject :many
-- Every explicit setting for one (layer, subject) across all credentials.
-- Drives the "This agent" / "Runtime" column of the permissions table.
SELECT credential_key, setting, updated_by, updated_at
FROM cerebro_credential_policy
WHERE workspace_id = $1 AND layer = $2 AND subject_id = $3
ORDER BY credential_key ASC;
