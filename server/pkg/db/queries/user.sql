-- name: GetUser :one
SELECT * FROM multica_user
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM multica_user
WHERE email = $1;

-- name: CreateUser :one
INSERT INTO multica_user (name, email, avatar_url)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateUser :one
-- Patches the user-controlled profile fields. Each parameter follows
-- COALESCE-on-NULL semantics so the handler can omit any field it
-- doesn't intend to write.
--
-- `timezone` (Viewing-tz preference) participates in
-- the same shape but uses sqlc.narg + a sentinel-string convention:
-- the handler passes the empty string "" to mean "clear back to NULL"
-- (browser-detected fallback), an IANA name like "Asia/Shanghai" to
-- pin a value, and `sqlc.narg('timezone') IS NULL` (no value at all)
-- to leave the existing column untouched. Folding it into UpdateUser
-- rather than carrying a dedicated UpdateUserTimezone keeps the
-- profile-patch shape uniform between Preferences fields.
UPDATE multica_user SET
    name = COALESCE($2, name),
    avatar_url = COALESCE($3, avatar_url),
    language = COALESCE($4, language),
    profile_description = COALESCE(sqlc.narg('profile_description'), profile_description),
    timezone = CASE
        WHEN sqlc.narg('timezone')::text IS NULL THEN timezone
        WHEN sqlc.narg('timezone')::text = ''    THEN NULL
        ELSE sqlc.narg('timezone')::text
    END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkUserOnboarded :one
UPDATE multica_user SET
    onboarded_at = COALESCE(onboarded_at, now()),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: PatchUserOnboarding :one
-- Partial update of the user's onboarding decision fields. Currently only the
-- questionnaire JSONB is patchable — the v2 attempt at persisting Step 3
-- runtime choice on the user row was reverted; that state now lives in a
-- frontend Zustand transient store.
UPDATE multica_user SET
    onboarding_questionnaire = COALESCE(sqlc.narg('questionnaire'), onboarding_questionnaire),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: JoinCloudWaitlist :one
-- Records interest in cloud runtimes. Does NOT mark onboarding
-- complete — the user still has to pick a real path (CLI / Skip)
-- in Step 3. Repeating the call overwrites email + reason.
UPDATE multica_user SET
    cloud_waitlist_email = $2,
    cloud_waitlist_reason = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetUserBySubjectID :one
SELECT * FROM multica_user
WHERE subject_id = $1
LIMIT 1;

-- name: SetUserSubjectID :exec
UPDATE multica_user SET subject_id = $2, updated_at = now()
WHERE id = $1;

-- name: SetStarterContentState :one
-- Atomically transition starter_content_state. The handler is
-- responsible for checking the current value first (to decide between
-- "transition NULL -> imported and run the seeding" vs "already
-- decided, short-circuit"). Using COALESCE here would swallow the
-- transition, so this is a straight assignment.
UPDATE multica_user SET
    starter_content_state = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateUserNameAndEmail :one
-- Syncs name and email from an external identity provider (Casdoor).
-- Called by the SubjectResolver when the JWT claims differ from the
-- locally stored profile.
UPDATE multica_user SET
    name = $2,
    email = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;
