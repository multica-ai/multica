-- name: GetUser :one
SELECT * FROM "user"
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM "user"
WHERE email = $1;

-- name: CreateUser :one
INSERT INTO "user" (name, email, avatar_url, google_id, email_verified)
VALUES ($1, $2, $3, sqlc.narg('google_id'), sqlc.arg('email_verified'))
RETURNING *;

-- name: UpdateUser :one
UPDATE "user" SET
    name = COALESCE($2, name),
    avatar_url = COALESCE($3, avatar_url),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkUserOnboarded :one
UPDATE "user" SET
    onboarded_at = COALESCE(onboarded_at, now()),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: PatchUserOnboarding :one
UPDATE "user" SET
    onboarding_questionnaire = COALESCE(sqlc.narg('questionnaire'), onboarding_questionnaire),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: JoinCloudWaitlist :one
-- Records interest in cloud runtimes. Does NOT mark onboarding
-- complete — the user still has to pick a real path (CLI / Skip)
-- in Step 3. Repeating the call overwrites email + reason.
UPDATE "user" SET
    cloud_waitlist_email = $2,
    cloud_waitlist_reason = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetUserByGoogleID :one
SELECT * FROM "user"
WHERE google_id = $1;

-- name: LinkGoogleIdentity :one
-- Sets google_id on first Google login. email_verified flips to TRUE because
-- the caller has already validated Google's email_verified claim against a
-- verified ID token before invoking this query.
UPDATE "user" SET
    google_id = $2,
    email_verified = TRUE,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetStarterContentState :one
-- Atomically transition starter_content_state. The handler is
-- responsible for checking the current value first (to decide between
-- "transition NULL -> imported and run the seeding" vs "already
-- decided, short-circuit"). Using COALESCE here would swallow the
-- transition, so this is a straight assignment.
UPDATE "user" SET
    starter_content_state = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;
