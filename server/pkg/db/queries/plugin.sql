-- name: CreatePluginInstallation :one
INSERT INTO plugin_installation (
    workspace_id, plugin_key, source_url, version, manifest, granted_scopes, installed_by
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdatePluginInstallationManifest :one
-- Upgrade path: the re-consented manifest snapshot replaces the old one in
-- place. Config values survive on purpose; fields the new manifest dropped are
-- pruned by the service before this runs.
UPDATE plugin_installation
SET source_url = $2,
    version = $3,
    manifest = $4,
    granted_scopes = $5,
    config = $6,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdatePluginInstallationConfig :one
UPDATE plugin_installation
SET config = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetPluginInstallationEnabled :one
UPDATE plugin_installation
SET enabled = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetPluginInstallation :one
SELECT * FROM plugin_installation WHERE id = $1;

-- name: GetWorkspacePluginInstallation :one
SELECT * FROM plugin_installation
WHERE workspace_id = $1 AND id = $2;

-- name: GetWorkspacePluginInstallationByKey :one
SELECT * FROM plugin_installation
WHERE workspace_id = $1 AND plugin_key = $2;

-- name: ListWorkspacePluginInstallations :many
SELECT * FROM plugin_installation
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: DeletePluginInstallation :exec
DELETE FROM plugin_installation WHERE id = $1;

-- name: UpsertPluginStorageValue :one
INSERT INTO plugin_storage (installation_id, scope_type, scope_id, key, value)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (installation_id, scope_type, scope_id, key)
DO UPDATE SET value = EXCLUDED.value, updated_at = now()
RETURNING *;

-- name: GetPluginStorageValue :one
SELECT * FROM plugin_storage
WHERE installation_id = $1 AND scope_type = $2 AND scope_id = $3 AND key = $4;

-- name: ListPluginStorageKeys :many
SELECT key, octet_length(value)::bigint AS size_bytes, updated_at
FROM plugin_storage
WHERE installation_id = $1 AND scope_type = $2 AND scope_id = $3
ORDER BY key ASC;

-- name: DeletePluginStorageValue :execrows
DELETE FROM plugin_storage
WHERE installation_id = $1 AND scope_type = $2 AND scope_id = $3 AND key = $4;

-- name: GetPluginStorageUsage :one
-- Quota accounting for one (installation, scope) pair. The candidate key is
-- excluded so overwriting an existing key is measured as a replacement rather
-- than an addition. octet_length, not char_length: the service compares these
-- against byte budgets, and a UTF-8 character is up to 4 bytes.
SELECT COUNT(*)::bigint AS key_count,
       COALESCE(SUM(octet_length(value)), 0)::bigint AS total_bytes
FROM plugin_storage
WHERE installation_id = $1 AND scope_type = $2 AND scope_id = $3 AND key <> $4;

-- name: DeletePluginStorageByInstallation :exec
DELETE FROM plugin_storage WHERE installation_id = $1;

-- name: UpsertPluginSecret :exec
INSERT INTO plugin_secret (installation_id, key, ciphertext)
VALUES ($1, $2, $3)
ON CONFLICT (installation_id, key)
DO UPDATE SET ciphertext = EXCLUDED.ciphertext, updated_at = now();

-- name: ListPluginSecretKeys :many
-- Deliberately never selects ciphertext: the presence of a secret is readable,
-- its value is not.
SELECT key, updated_at FROM plugin_secret
WHERE installation_id = $1
ORDER BY key ASC;

-- name: GetPluginSecret :one
SELECT * FROM plugin_secret
WHERE installation_id = $1 AND key = $2;

-- name: DeletePluginSecret :execrows
DELETE FROM plugin_secret WHERE installation_id = $1 AND key = $2;

-- name: DeletePluginSecretsByInstallation :exec
DELETE FROM plugin_secret WHERE installation_id = $1;
