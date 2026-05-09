-- Phase 5 Ship Hub — pre-flight production gate.
--
-- The handler exposes:
--   POST   /api/deploy_environments/{id}/preflight  — get-or-create
--   PATCH  /api/deploy_preflight/{id}               — partial update
--   POST   /api/deploy_preflight/{id}/promote       — gate + create deploy
--
-- Idempotency comes from UNIQUE (environment_id, target_sha) — re-opening
-- the dialog hits the existing row, so the user's checklist progress
-- isn't lost when the page reloads.

-- name: GetOrCreateDeployPreflight :one
-- ON CONFLICT DO NOTHING + RETURNING + a follow-up SELECT would race; the
-- DO UPDATE SET id = id pattern lets us RETURN the row whether it was
-- inserted or already present, in a single statement.
INSERT INTO deploy_preflight (workspace_id, environment_id, target_sha)
VALUES ($1, $2, $3)
ON CONFLICT (environment_id, target_sha) DO UPDATE
    SET updated_at = deploy_preflight.updated_at
RETURNING *;

-- name: GetDeployPreflightByID :one
SELECT * FROM deploy_preflight WHERE id = $1;

-- name: GetDeployPreflightByEnvAndSHA :one
SELECT * FROM deploy_preflight
WHERE environment_id = $1 AND target_sha = $2;

-- name: UpdateDeployPreflight :one
-- Partial update via narg COALESCE. Boolean flags use COALESCE on the
-- narg-bool because TRUE → set, NULL → leave alone (no way to "clear" a
-- bool from an open checklist). approver_id and rollback_plan are
-- nullable narg pgtype's so passing NULL clears them.
UPDATE deploy_preflight SET
    migrations_ok       = COALESCE(sqlc.narg('migrations_ok')::bool, migrations_ok),
    smoke_tests_ok      = COALESCE(sqlc.narg('smoke_tests_ok')::bool, smoke_tests_ok),
    qa_verified_at      = sqlc.narg('qa_verified_at'),
    qa_verified_by      = sqlc.narg('qa_verified_by'),
    rollback_plan       = sqlc.narg('rollback_plan'),
    approver_id         = sqlc.narg('approver_id'),
    second_approver_id  = sqlc.narg('second_approver_id'),
    approved_at         = sqlc.narg('approved_at'),
    updated_at          = NOW()
WHERE id = $1
RETURNING *;

-- name: MarkDeployPreflightPromoted :one
UPDATE deploy_preflight SET
    promoted_at = NOW(),
    updated_at  = NOW()
WHERE id = $1
RETURNING *;
