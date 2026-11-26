-- name: GetUserAccountState :one
-- What auth needs beyond a valid credential (MUL-10296): whether the account
-- is suspended, and the session generation a session token must carry.
SELECT (suspended_at IS NOT NULL)::bool AS suspended, token_version
FROM "user"
WHERE id = $1;

-- name: GetUserForSuspension :one
SELECT * FROM "user"
WHERE id = $1
FOR UPDATE;

-- name: SuspendUser :one
-- Keeps the first suspension's time and reason when the account is already
-- suspended: no row is returned then.
UPDATE "user"
SET suspended_at = now(),
    suspended_reason = @reason,
    suspended_by = @suspended_by
WHERE id = @id AND suspended_at IS NULL
RETURNING *;

-- name: UnsuspendUser :one
UPDATE "user"
SET suspended_at = NULL,
    suspended_reason = NULL,
    suspended_by = NULL
WHERE id = $1
RETURNING *;

-- name: EndUserSessions :one
-- Every session token minted before this carries an older token_version and
-- is refused from now on, including after the account is unsuspended.
UPDATE "user"
SET token_version = token_version + 1
WHERE id = $1
RETURNING token_version;

-- name: CreateUserSuspensionEvent :one
INSERT INTO user_suspension_event (user_id, action, actor, reason, status, report, completed_at)
VALUES (
    @user_id, @action, @actor, @reason, @status, @report,
    CASE WHEN @status::text = 'completed' THEN now() END
)
RETURNING *;

-- name: GetRunningIncidentResponse :one
-- The incident response a rerun resumes.
SELECT * FROM user_suspension_event
WHERE user_id = $1 AND action = 'incident_response' AND status = 'running'
ORDER BY created_at DESC
LIMIT 1
FOR UPDATE;

-- name: LockIncidentWorkspaceRecorded :one
-- Serializes reruns of one incident response: the second waits here, then
-- sees the workspace the first one recorded.
SELECT ((report -> 'workspaces') ? @workspace_id::text)::bool AS recorded
FROM user_suspension_event
WHERE id = @id
FOR UPDATE;

-- name: RecordIncidentWorkspace :exec
-- Written in the same transaction as the workspace's changes, so a workspace
-- recorded here is never stopped twice.
UPDATE user_suspension_event
SET report = jsonb_set(report, ARRAY['workspaces', @workspace_id::text], @result::jsonb, true)
WHERE id = @id;

-- name: CompleteUserSuspensionEvent :one
UPDATE user_suspension_event
SET status = 'completed',
    completed_at = now(),
    report = report || @summary::jsonb
WHERE id = @id
RETURNING *;

-- name: ListUserSuspensionEvents :many
SELECT * FROM user_suspension_event
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT 20;
