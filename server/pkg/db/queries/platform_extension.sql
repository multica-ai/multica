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

-- name: ListPlatformExtensionReleasesInWorkspace :many
SELECT * FROM platform_extension_release
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC;

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
