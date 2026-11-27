-- name: GetPlatformSettings :one
SELECT settings FROM platform_settings;

-- name: GetPlatformSetting :one
SELECT COALESCE(settings -> (sqlc.arg(key)::text), 'null'::jsonb)::jsonb FROM platform_settings;

-- name: SetPlatformSetting :exec
UPDATE platform_settings SET settings = CASE
    WHEN sqlc.arg(value)::jsonb = 'null'::jsonb THEN settings - (sqlc.arg(key)::text)
    ELSE jsonb_set(settings, ARRAY[sqlc.arg(key)::text], sqlc.arg(value)::jsonb)
END;

-- name: ListPlatformWorkspaces :many
SELECT id, name, slug FROM workspace
WHERE (sqlc.narg(after_id)::uuid IS NULL OR id > sqlc.narg(after_id)::uuid)
  AND (strpos(lower(name), lower(sqlc.arg(search)::text)) > 0
    OR strpos(lower(slug), lower(sqlc.arg(search)::text)) > 0
    OR sqlc.arg(search)::text = '')
ORDER BY id
LIMIT sqlc.arg(page_size)::int;

-- name: ListPlatformUsers :many
SELECT u.id, u.name, u.email, EXISTS (
    SELECT 1 FROM member m
    WHERE m.user_id = u.id AND m.role IN ('owner', 'admin', 'member', 'guest')
) AS has_workspace
FROM "user" u
WHERE (sqlc.narg(after_id)::uuid IS NULL OR u.id > sqlc.narg(after_id)::uuid)
  AND (strpos(lower(u.name), lower(sqlc.arg(search)::text)) > 0
    OR strpos(lower(u.email), lower(sqlc.arg(search)::text)) > 0
    OR sqlc.arg(search)::text = '')
ORDER BY u.id
LIMIT sqlc.arg(page_size)::int;

-- name: LockWorkspaceForPlatformAssignment :one
SELECT id FROM workspace WHERE id = $1 FOR KEY SHARE;
