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
