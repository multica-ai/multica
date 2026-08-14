-- name: LockPluginRegistryKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(@plugin_key, 0));

-- name: CreatePluginIdentity :one
INSERT INTO plugin_identity (
    plugin_key, display_name, publisher_id, publisher_type, trust_tier,
    owner_workspace_id
) VALUES (
    @plugin_key, @display_name, @publisher_id, @publisher_type, @trust_tier,
    sqlc.narg('owner_workspace_id')
)
RETURNING *;

-- name: GetPluginIdentity :one
SELECT * FROM plugin_identity
WHERE id = $1;

-- name: GetOfficialPluginIdentityByKey :one
SELECT * FROM plugin_identity
WHERE plugin_key = $1 AND owner_workspace_id IS NULL;

-- name: GetWorkspacePrivatePluginIdentityByKey :one
SELECT * FROM plugin_identity
WHERE owner_workspace_id = @workspace_id AND plugin_key = @plugin_key;

-- name: RetirePluginIdentity :one
UPDATE plugin_identity
SET retired_at = COALESCE(retired_at, now())
WHERE id = $1
RETURNING *;

-- name: CreatePluginRelease :one
WITH parent AS MATERIALIZED (
    SELECT plugin_identity.id
    FROM plugin_identity
    WHERE plugin_identity.id = @plugin_id AND plugin_identity.retired_at IS NULL
    FOR KEY SHARE
)
INSERT INTO plugin_release (
    plugin_id, version, manifest, manifest_digest,
    source_kind, source_ref,
    archive_digest, artifact_ref, artifact_digest, artifact_size,
    signature, signature_key_id
)
SELECT
    parent.id, @version, @manifest, @manifest_digest,
    @source_kind, @source_ref,
    @archive_digest, @artifact_ref, @artifact_digest, @artifact_size,
    sqlc.narg('signature'), sqlc.narg('signature_key_id')
FROM parent
RETURNING *;

-- name: GetPluginRelease :one
SELECT * FROM plugin_release
WHERE id = $1;

-- name: GetPluginReleaseByVersion :one
SELECT * FROM plugin_release
WHERE plugin_id = $1 AND version = $2 AND revocation_status = 'active';

-- name: GetRegisteredPluginReleaseByVersion :one
SELECT * FROM plugin_release
WHERE plugin_id = $1 AND version = $2;

-- name: RevokePluginRelease :one
UPDATE plugin_release
SET revocation_status = @revocation_status,
    revoked_at = now(),
    revocation_reason = @revocation_reason
WHERE id = @id AND revocation_status = 'active'
RETURNING *;

-- name: CreatePluginContribution :one
WITH parent AS MATERIALIZED (
    SELECT plugin_release.id, plugin_release.artifact_digest
    FROM plugin_release
    WHERE plugin_release.id = @release_id AND plugin_release.revocation_status = 'active'
    FOR KEY SHARE
)
INSERT INTO plugin_contribution (
    release_id, contribution_key, type, schema_version,
    display_name, description, entry_path, entry_digest,
    artifact_digest, required_daemon_features, ordinal
)
SELECT
    parent.id, @contribution_key, @type, @schema_version,
    @display_name, @description, @entry_path, @entry_digest,
    parent.artifact_digest, @required_daemon_features, @ordinal
FROM parent
RETURNING *;

-- name: ListPluginContributionsByRelease :many
SELECT * FROM plugin_contribution
WHERE release_id = $1
ORDER BY ordinal, id;

-- name: ListPluginReleasesByPlugin :many
SELECT * FROM plugin_release
WHERE plugin_id = $1 AND revocation_status = 'active'
ORDER BY published_at DESC, id DESC;

-- name: CreatePluginInstallation :one
WITH parents AS MATERIALIZED (
    SELECT w.id AS workspace_id, p.id AS plugin_id, r.id AS release_id,
           r.source_kind, r.source_ref
    FROM workspace w
    JOIN plugin_identity p ON p.id = @plugin_id AND p.retired_at IS NULL
    JOIN plugin_release r ON r.id = @release_id
                         AND r.plugin_id = p.id
                         AND r.revocation_status = 'active'
    WHERE w.id = @workspace_id
    FOR KEY SHARE OF w, p, r
)
INSERT INTO plugin_installation (
    workspace_id, plugin_id, source_kind, source_ref,
    desired_release_id, enabled, lifecycle_status,
    installed_by, updated_by
)
SELECT
    parents.workspace_id, parents.plugin_id, parents.source_kind, parents.source_ref,
    parents.release_id, FALSE, 'installed',
    sqlc.narg('installed_by'), sqlc.narg('installed_by')
FROM parents
RETURNING *;

-- name: GetPluginInstallation :one
SELECT * FROM plugin_installation
WHERE id = $1;

-- name: GetWorkspacePluginInstallation :one
SELECT * FROM plugin_installation
WHERE workspace_id = $1 AND plugin_id = $2 AND uninstalled_at IS NULL;

-- name: ListWorkspacePluginInstallations :many
SELECT * FROM plugin_installation
WHERE workspace_id = $1 AND uninstalled_at IS NULL
ORDER BY installed_at, id;

-- name: ListWorkspacePluginReleases :many
SELECT release.*
FROM plugin_release release
JOIN plugin_installation installation ON installation.plugin_id = release.plugin_id
WHERE installation.workspace_id = @workspace_id
  AND installation.uninstalled_at IS NULL
  AND release.revocation_status = 'active'
ORDER BY release.plugin_id, release.published_at DESC, release.id DESC;

-- name: ListWorkspacePrivatePluginInstallations :many
SELECT installation.*
FROM plugin_installation installation
JOIN plugin_identity identity ON identity.id = installation.plugin_id
WHERE installation.workspace_id = @workspace_id
  AND identity.owner_workspace_id = @workspace_id
  AND installation.source_kind = 'private_dev'
  AND installation.uninstalled_at IS NULL
ORDER BY installation.installed_at, installation.id;

-- name: GetWorkspacePrivatePluginInstallationByRef :one
SELECT installation.*
FROM plugin_installation installation
JOIN plugin_identity identity ON identity.id = installation.plugin_id
WHERE installation.workspace_id = @workspace_id
  AND identity.owner_workspace_id = @workspace_id
  AND installation.source_kind = 'private_dev'
  AND installation.uninstalled_at IS NULL
  AND (installation.id::text = @plugin_ref::text OR identity.plugin_key = @plugin_ref::text)
LIMIT 1;

-- name: GetWorkspacePrivatePluginUsage :one
SELECT
    COUNT(release.id)::bigint AS release_count,
    COALESCE(SUM(release.artifact_size), 0)::bigint AS total_bytes
FROM plugin_release release
JOIN plugin_identity identity ON identity.id = release.plugin_id
WHERE identity.owner_workspace_id = @workspace_id
  AND release.source_kind = 'private_dev';

-- name: SetPluginInstallationDesiredState :one
WITH workspace_guard AS MATERIALIZED (
    SELECT workspace.id
    FROM workspace
    WHERE workspace.id = @workspace_id
    FOR KEY SHARE
),
target AS MATERIALIZED (
    SELECT i.id, r.id AS release_id
    FROM plugin_installation i
    JOIN workspace_guard w ON w.id = i.workspace_id
    JOIN plugin_release r ON r.id = @desired_release_id
                         AND r.plugin_id = i.plugin_id
                         AND r.revocation_status = 'active'
    WHERE i.id = @id
      AND i.workspace_id = @workspace_id
      AND i.uninstalled_at IS NULL
    FOR UPDATE OF i
)
UPDATE plugin_installation i
SET desired_release_id = target.release_id,
    enabled = @enabled,
    desired_generation = i.desired_generation + 1,
    lifecycle_status = 'activating',
    updated_by = sqlc.narg('updated_by'),
    updated_at = now(),
    disabled_at = CASE WHEN @enabled::boolean THEN NULL ELSE now() END
FROM target
WHERE i.id = target.id
RETURNING i.*;

-- name: UninstallPluginInstallation :one
WITH target AS MATERIALIZED (
    SELECT installation.id
    FROM plugin_installation installation
    JOIN workspace ON workspace.id = installation.workspace_id
    WHERE installation.id = @id
      AND installation.workspace_id = @workspace_id
      AND installation.uninstalled_at IS NULL
    FOR UPDATE OF installation
    FOR KEY SHARE OF workspace
)
UPDATE plugin_installation installation
SET enabled = FALSE,
    desired_generation = installation.desired_generation + 1,
    lifecycle_status = 'uninstalled',
    updated_by = sqlc.narg('updated_by'),
    updated_at = now(),
    disabled_at = COALESCE(installation.disabled_at, now()),
    uninstalled_at = now()
FROM target
WHERE installation.id = target.id
RETURNING installation.*;

-- name: CreatePluginGrantRevision :one
WITH target AS MATERIALIZED (
    SELECT i.id
    FROM plugin_installation i
    JOIN workspace w ON w.id = i.workspace_id
    WHERE i.id = @installation_id
      AND i.workspace_id = @workspace_id
      AND i.uninstalled_at IS NULL
    FOR UPDATE OF i
    FOR KEY SHARE OF w
),
next_revision AS (
    SELECT COALESCE(MAX(g.grant_revision), 0) + 1 AS revision
    FROM plugin_grant g
    JOIN target ON target.id = g.installation_id
    WHERE g.capability = @capability
)
INSERT INTO plugin_grant (
    installation_id, capability, decision, limits,
    grant_revision, approved_by, revoked_at
)
SELECT
    target.id, @capability, @decision, @limits,
    next_revision.revision, sqlc.narg('approved_by'),
    CASE WHEN @decision::text = 'denied' THEN now() ELSE NULL END
FROM target CROSS JOIN next_revision
RETURNING *;

-- name: ListLatestPluginGrants :many
SELECT DISTINCT ON (capability) *
FROM plugin_grant
WHERE installation_id = $1
ORDER BY capability, grant_revision DESC;

-- name: CreatePluginBindingRevision :one
WITH workspace_scope AS MATERIALIZED (
    SELECT i.id
    FROM plugin_installation i
    JOIN workspace w ON w.id = i.workspace_id
    WHERE @scope_type::text = 'workspace'
      AND i.id = @installation_id
      AND i.workspace_id = @workspace_id
      AND @scope_id::uuid = i.workspace_id
      AND i.uninstalled_at IS NULL
    FOR UPDATE OF i
    FOR KEY SHARE OF w
),
agent_scope AS MATERIALIZED (
    SELECT i.id
    FROM plugin_installation i
    JOIN workspace w ON w.id = i.workspace_id
    JOIN agent a ON a.id = @scope_id AND a.workspace_id = i.workspace_id
    WHERE @scope_type::text = 'agent'
      AND i.id = @installation_id
      AND i.workspace_id = @workspace_id
      AND i.uninstalled_at IS NULL
    FOR UPDATE OF i
    FOR KEY SHARE OF w, a
),
target AS MATERIALIZED (
    SELECT id FROM workspace_scope
    UNION ALL
    SELECT id FROM agent_scope
),
next_revision AS (
    SELECT COALESCE(MAX(b.binding_revision), 0) + 1 AS revision
    FROM plugin_binding b
    JOIN target ON target.id = b.installation_id
    WHERE b.scope_type = @scope_type AND b.scope_id = @scope_id
)
INSERT INTO plugin_binding (
    installation_id, scope_type, scope_id, enabled,
    binding_revision, created_by
)
SELECT
    target.id, @scope_type, @scope_id, @enabled,
    next_revision.revision, sqlc.narg('created_by')
FROM target CROSS JOIN next_revision
RETURNING *;

-- name: ListLatestPluginBindings :many
SELECT DISTINCT ON (scope_type, scope_id) *
FROM plugin_binding
WHERE installation_id = $1
ORDER BY scope_type, scope_id, binding_revision DESC;

-- name: CreatePluginArtifactFile :one
WITH parent AS MATERIALIZED (
    SELECT plugin_release.id
    FROM plugin_release
    WHERE plugin_release.id = @release_id
    FOR KEY SHARE
)
INSERT INTO plugin_artifact_file (
    release_id, path, digest, size_bytes, content
)
SELECT parent.id, @path, @digest, @size_bytes, @content
FROM parent
RETURNING *;

-- name: GetPluginArtifactFileByReleasePath :one
SELECT * FROM plugin_artifact_file
WHERE release_id = $1 AND path = $2;

-- name: GetPluginArtifactFile :one
SELECT * FROM plugin_artifact_file
WHERE id = $1;

-- name: GetLatestPluginRelease :one
SELECT release.*
FROM plugin_release release
JOIN plugin_identity identity ON identity.id = release.plugin_id
WHERE identity.plugin_key = @plugin_key
  AND identity.retired_at IS NULL
  AND release.revocation_status = 'active'
ORDER BY release.published_at DESC, release.id DESC
LIMIT 1;

-- name: GetPluginReleaseByPluginKeyVersion :one
SELECT release.*
FROM plugin_release release
JOIN plugin_identity identity ON identity.id = release.plugin_id
WHERE identity.plugin_key = @plugin_key
  AND identity.owner_workspace_id IS NULL
  AND identity.retired_at IS NULL
  AND release.version = @version
  AND release.revocation_status = 'active';

-- name: EnsurePluginWorkspaceCapabilityState :one
WITH workspace_guard AS MATERIALIZED (
    SELECT workspace.id
    FROM workspace
    WHERE workspace.id = @workspace_id
    FOR KEY SHARE
)
INSERT INTO plugin_workspace_capability_state (workspace_id)
SELECT workspace_guard.id FROM workspace_guard
ON CONFLICT (workspace_id) DO UPDATE
SET workspace_id = EXCLUDED.workspace_id
RETURNING *;

-- name: GetPluginWorkspaceCapabilityStateForUpdate :one
SELECT * FROM plugin_workspace_capability_state
WHERE workspace_id = $1
FOR UPDATE;

-- name: ListPluginInstallationsForCompile :many
SELECT * FROM plugin_installation
WHERE workspace_id = $1 AND uninstalled_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: ListPluginCompilationContributions :many
WITH latest_grants AS (
    SELECT DISTINCT ON (installation_id, capability)
        installation_id, capability, decision, limits, grant_revision
    FROM plugin_grant
    ORDER BY installation_id, capability, grant_revision DESC
),
latest_bindings AS (
    SELECT DISTINCT ON (installation_id, scope_type, scope_id)
        installation_id, scope_type, scope_id, enabled, binding_revision
    FROM plugin_binding
    ORDER BY installation_id, scope_type, scope_id, binding_revision DESC
)
SELECT
    installation.id AS installation_id,
    installation.workspace_id,
    installation.desired_generation,
    identity.id AS plugin_id,
    identity.plugin_key,
    release.id AS release_id,
    release.version AS release_version,
    release.source_kind,
    release.artifact_ref,
    release.artifact_digest,
    contribution.id AS contribution_id,
    contribution.contribution_key,
    contribution.type AS contribution_type,
    contribution.display_name,
    contribution.description,
    contribution.entry_path,
    contribution.entry_digest,
    contribution.required_daemon_features,
    contribution.ordinal,
    artifact.id AS artifact_file_id,
    artifact.content AS entry_content,
    artifact.size_bytes AS entry_size_bytes,
    binding.scope_type,
    binding.scope_id,
    binding.enabled AS binding_enabled,
    binding.binding_revision,
    grant_row.grant_revision
FROM plugin_installation installation
JOIN plugin_identity identity
  ON identity.id = installation.plugin_id
JOIN plugin_release release
  ON release.id = installation.desired_release_id
 AND release.plugin_id = identity.id
 AND release.revocation_status = 'active'
JOIN plugin_contribution contribution
  ON contribution.release_id = release.id
JOIN plugin_artifact_file artifact
  ON artifact.release_id = release.id
 AND artifact.path = contribution.entry_path
JOIN latest_grants grant_row
  ON grant_row.installation_id = installation.id
 AND grant_row.capability = 'agent.skill.contribute'
 AND grant_row.decision = 'granted'
LEFT JOIN latest_bindings binding
  ON binding.installation_id = installation.id
WHERE installation.workspace_id = @workspace_id
  AND installation.uninstalled_at IS NULL
  AND installation.enabled = TRUE
ORDER BY identity.plugin_key, contribution.ordinal,
         contribution.contribution_key,
         binding.scope_type NULLS FIRST, binding.scope_id NULLS FIRST;

-- name: CreatePluginCapabilitySnapshot :one
INSERT INTO plugin_capability_snapshot (
    workspace_id, revision, source_generations,
    compiler_version, schema_version, snapshot_digest,
    compiled_entries, diagnostics
) VALUES (
    @workspace_id, @revision, @source_generations,
    @compiler_version, @schema_version, @snapshot_digest,
    @compiled_entries, @diagnostics
)
RETURNING *;

-- name: ActivatePluginWorkspaceCapabilitySnapshot :one
UPDATE plugin_workspace_capability_state
SET active_snapshot_id = @active_snapshot_id,
    active_revision = @active_revision,
    next_revision = @next_revision,
    updated_at = now()
WHERE workspace_id = @workspace_id
  AND next_revision = @active_revision
RETURNING *;

-- name: ActivatePluginInstallations :many
UPDATE plugin_installation
SET active_release_id = desired_release_id,
    active_generation = desired_generation,
    lifecycle_status = CASE WHEN enabled THEN 'active' ELSE 'installed' END,
    updated_at = now()
WHERE workspace_id = $1
  AND uninstalled_at IS NULL
RETURNING *;

-- name: CreatePluginHealth :one
INSERT INTO plugin_health (
    workspace_id, installation_id, scope_type, scope_id,
    state, reason_code, safe_detail,
    observed_generation, last_good_snapshot_id
) VALUES (
    @workspace_id, @installation_id, @scope_type, sqlc.narg('scope_id'),
    @state, @reason_code, @safe_detail,
    @observed_generation, sqlc.narg('last_good_snapshot_id')
)
RETURNING *;

-- name: GetPluginExecutionManifestByTask :one
SELECT manifest.*
FROM plugin_execution_manifest manifest
JOIN agent_task_queue task
  ON task.plugin_execution_manifest_id = manifest.id
WHERE task.id = @task_id
  AND manifest.task_id = task.id;

-- name: ListWorkspacePluginHealth :many
SELECT * FROM plugin_health
WHERE workspace_id = $1
ORDER BY observed_at DESC, id DESC;

-- name: GetWorkspacePluginHealthByInstallation :one
SELECT * FROM plugin_health
WHERE workspace_id = @workspace_id AND installation_id = @installation_id
ORDER BY observed_at DESC, id DESC
LIMIT 1;
