-- name: CreatePlatformExtensionReleaseReservation :one
-- Reserves an immutable release identity before the importer allocates a
-- runtime and creates native resources in the same transaction.
INSERT INTO platform_extension_release (
    workspace_id,
    extension_key,
    name,
    version,
    digest,
    manifest,
    created_by
) VALUES (
    @workspace_id,
    @extension_key,
    @name,
    @version,
    @digest,
    @manifest,
    @created_by
)
ON CONFLICT (workspace_id, extension_key, version) DO NOTHING
RETURNING *;

-- name: CompletePlatformExtensionRelease :one
-- Stores the runtime, squad, and native-resource mapping after their creation
-- succeeds inside the import transaction.
UPDATE platform_extension_release
SET runtime_id = @runtime_id,
    squad_id = @squad_id,
    resources = @resources
WHERE id = @id
  AND workspace_id = @workspace_id
  AND runtime_id IS NULL
  AND squad_id IS NULL
RETURNING *;

-- name: GetPlatformExtensionReleaseByIdentity :one
SELECT * FROM platform_extension_release
WHERE workspace_id = @workspace_id
  AND extension_key = @extension_key
  AND version = @version;

-- name: GetPlatformExtensionReleaseInWorkspace :one
SELECT * FROM platform_extension_release
WHERE id = @id
  AND workspace_id = @workspace_id;

-- name: LockPlatformExtensionReleaseInWorkspace :one
-- The editable mapping belongs to one immutable Extension release. Lock it
-- before changing its Squad name, internal Agent bindings, and audit mapping
-- so concurrent saves cannot publish a mixed version.
SELECT * FROM platform_extension_release
WHERE id = @id
  AND workspace_id = @workspace_id
FOR UPDATE;

-- name: UpdatePlatformExtensionReleaseMapping :one
-- A release's manifest/version stay immutable. Only its versioned Squad
-- display name and per-Agent fixed-runtime bindings are editable.
UPDATE platform_extension_release
SET runtime_id = @runtime_id,
    resources = @resources
WHERE id = @id
  AND workspace_id = @workspace_id
RETURNING *;

-- name: UpdatePlatformExtensionSquadName :one
-- Keep a release-owned Squad inside its own workspace. The editable base name
-- is always rendered with the immutable Extension version by the handler.
UPDATE squad
SET name = @name,
    updated_at = now()
WHERE id = @squad_id
  AND workspace_id = @workspace_id
  AND archived_at IS NULL
RETURNING *;

-- name: ArchivePlatformExtensionSquad :one
-- Archive just this release's versioned Squad. Its internal resources remain
-- intact for audit/history, but the Squad is no longer normally selectable.
UPDATE squad
SET archived_at = now(),
    archived_by = @archived_by,
    updated_at = now()
WHERE id = @squad_id
  AND workspace_id = @workspace_id
  AND archived_at IS NULL
RETURNING *;

-- name: ListPlatformExtensionReleasesInWorkspace :many
SELECT * FROM platform_extension_release
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListPlatformExtensionSquadBindings :many
-- The Extension release is the sole authority for identifying a managed
-- Squad. UI consumers must not infer this relationship from names or Agent
-- system keys.
SELECT id AS release_id, squad_id, extension_key, version
FROM platform_extension_release
WHERE workspace_id = $1
  AND squad_id IS NOT NULL;

-- name: DeletePlatformExtensionReleasesByWorkspace :exec
-- platform_extension_release deliberately has no workspace foreign key, so
-- workspace teardown must remove its audit mappings explicitly.
DELETE FROM platform_extension_release
WHERE workspace_id = $1;

-- name: ListPlatformExtensionRuntimeCandidates :many
-- The handler applies canUseRuntimeForAgent and Redis liveness to this small
-- candidate set before the import transaction re-checks and locks a winner.
SELECT *
FROM agent_runtime
WHERE workspace_id = $1
  AND provider = 'platform-agent-cli'
  AND status = 'online'
ORDER BY id ASC;

-- name: LockIdlePlatformExtensionRuntime :one
-- Re-checks every database-owned eligibility condition while locking the
-- selected runtime. eligible_ids has already been filtered by the handler's
-- canUseRuntimeForAgent predicate and, when Redis is healthy, its batch
-- liveness result. When Redis is unavailable, last_seen_at supplies the same
-- 150-second fallback window used by the runtime sweeper.
WITH active_agent_counts AS (
    SELECT runtime_id, count(*) AS agent_count
    FROM agent
    WHERE archived_at IS NULL
      AND runtime_id IS NOT NULL
    GROUP BY runtime_id
)
SELECT rt.*
FROM agent_runtime rt
LEFT JOIN active_agent_counts counts ON counts.runtime_id = rt.id
WHERE rt.workspace_id = @workspace_id
  AND rt.provider = 'platform-agent-cli'
  AND rt.status = 'online'
  AND rt.id = ANY(sqlc.arg('eligible_ids')::uuid[])
  AND (
      sqlc.arg('use_redis_liveness')::boolean
      OR rt.last_seen_at >= now() - interval '150 seconds'
  )
  AND NOT EXISTS (
      SELECT 1
      FROM agent_task_queue task
      WHERE task.runtime_id = rt.id
        AND task.status IN (
            'queued',
            'deferred',
            'dispatched',
            'running',
            'waiting_local_directory'
        )
  )
ORDER BY
    rt.last_seen_at DESC NULLS LAST,
    COALESCE(counts.agent_count, 0) ASC,
    rt.created_at ASC,
    rt.id ASC
FOR UPDATE OF rt SKIP LOCKED
LIMIT 1;
