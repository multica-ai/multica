-- name: GetUser :one
SELECT * FROM "user"
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM "user"
WHERE email = $1;

-- name: GetUserByExternalIdentity :one
-- Look up user by OIDC / external IDP identity. Used as the primary
-- lookup path for OIDC logins; the partial unique index on
-- (external_provider, external_user_id) WHERE external_user_id IS NOT NULL
-- enforces uniqueness and makes this an index-only fetch.
SELECT * FROM "user"
WHERE external_provider = $1 AND external_user_id = $2
LIMIT 1;

-- name: SetUserExternalIdentity :one
-- Bind a multica user to an external OIDC identity. Called on first
-- OIDC login (either by user creation or by email-link fallback) to
-- record the (provider, sub) pair so subsequent logins can find the
-- user by external_id directly.
UPDATE "user" SET
    external_provider = $2,
    external_user_id  = $3,
    updated_at        = now()
WHERE id = $1
RETURNING *;

-- name: CreateUser :one
INSERT INTO "user" (name, email, avatar_url)
VALUES ($1, $2, $3)
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
