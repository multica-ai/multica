-- Phase 7a — Release lifecycle (create / list / detail / mutate).
--
-- Naming convention:
--   * `Create` for a single inserting query.
--   * `List` returns multiple rows; ordering is the caller-friendly
--     default for that surface (workspace rail = updated_at DESC).
--   * `Update*` returns the updated row so the handler can echo it
--     to the client without an extra GET.

-- name: CreateRelease :one
INSERT INTO ship_release (
    workspace_id, project_id, title, description, risk_level,
    approver_id, second_approver_id, created_by
) VALUES (
    $1, $2, $3, sqlc.narg('description'), $4,
    sqlc.narg('approver_id'), sqlc.narg('second_approver_id'), sqlc.narg('created_by')
)
RETURNING *;

-- name: GetRelease :one
SELECT * FROM ship_release
WHERE id = $1;

-- name: GetReleaseInWorkspace :one
SELECT * FROM ship_release
WHERE id = $1 AND workspace_id = $2;

-- name: ListActiveReleasesByWorkspace :many
-- "Active" = anything not yet in a terminal stage. Drives the home-page
-- "Active releases" rail. Newest activity first.
SELECT * FROM ship_release
WHERE workspace_id = $1
  AND stage NOT IN ('done', 'rolled_back', 'cancelled')
ORDER BY updated_at DESC;

-- name: ListReleasesByProject :many
-- Project-scoped list, defaults to active stages. Pass include_terminal=TRUE
-- to include done / rolled_back / cancelled.
SELECT * FROM ship_release
WHERE project_id = $1
  AND (
      sqlc.arg('include_terminal')::bool = TRUE
      OR stage NOT IN ('done', 'rolled_back', 'cancelled')
  )
ORDER BY updated_at DESC;

-- name: ListRecentReleasesByProject :many
-- Bounded "Recent" tail used by the project page footer. Includes every
-- stage so a recently-shipped release stays visible for a few days.
SELECT * FROM ship_release
WHERE project_id = $1
ORDER BY updated_at DESC
LIMIT $2;

-- name: UpdateReleaseMetadata :one
-- COALESCE/narg pattern — caller leaves a field nil to leave it
-- untouched. updated_at always bumps so the rail re-orders.
UPDATE ship_release SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    approver_id = CASE
        WHEN sqlc.arg('approver_id_set')::bool THEN sqlc.narg('approver_id')
        ELSE approver_id
    END,
    second_approver_id = CASE
        WHEN sqlc.arg('second_approver_id_set')::bool THEN sqlc.narg('second_approver_id')
        ELSE second_approver_id
    END,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateReleaseStage :one
-- Stage transitions stamp the matching ladder timestamp when it
-- becomes non-NULL. We pass the timestamp explicitly from the service
-- layer instead of computing in SQL so the audit log carries the same
-- value the row records (no clock skew).
UPDATE ship_release SET
    stage = $2,
    merged_at = COALESCE(sqlc.narg('merged_at'), merged_at),
    staged_at = COALESCE(sqlc.narg('staged_at'), staged_at),
    promoted_at = COALESCE(sqlc.narg('promoted_at'), promoted_at),
    done_at = COALESCE(sqlc.narg('done_at'), done_at),
    rollback_reason = COALESCE(sqlc.narg('rollback_reason'), rollback_reason),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateReleaseRiskLevel :one
-- Aggregate-recompute path. Called after AddPullRequestToRelease /
-- RemovePullRequestFromRelease to keep the denormalized risk_level
-- column aligned with the join set.
UPDATE ship_release SET
    risk_level = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateReleaseChannel :one
UPDATE ship_release SET
    channel_id = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateReleaseIssue :one
UPDATE ship_release SET
    issue_id = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: AddPullRequestToRelease :one
-- Idempotent on (release_id, pull_request_id) via ON CONFLICT — repeat
-- adds return the existing row instead of failing.
INSERT INTO ship_release_pull_request (
    release_id, pull_request_id, position, is_active
) VALUES (
    $1, $2, $3, TRUE
)
ON CONFLICT (release_id, pull_request_id) DO UPDATE SET
    position = EXCLUDED.position,
    is_active = EXCLUDED.is_active
RETURNING *;

-- name: RemovePullRequestFromRelease :exec
-- Phase 7a removes the row outright; once a release leaves the
-- assembling stage we instead flip is_active=FALSE (see
-- DeactivateReleasePullRequests below).
DELETE FROM ship_release_pull_request
WHERE release_id = $1 AND pull_request_id = $2;

-- name: ListReleasePullRequests :many
-- Returns the PRs in this release joined with the membership row.
-- We expose the join columns explicitly because the merged_sha /
-- merge_error / merge_state fields are release-specific (they don't
-- live on pull_request itself).
SELECT
    pr.*,
    rpr.position AS membership_position,
    rpr.merged_sha AS membership_merged_sha,
    rpr.merged_at AS membership_merged_at,
    rpr.merge_error AS membership_merge_error,
    rpr.added_at AS membership_added_at,
    rpr.is_active AS membership_is_active,
    rpr.merge_state AS membership_merge_state
FROM ship_release_pull_request rpr
JOIN pull_request pr ON pr.id = rpr.pull_request_id
WHERE rpr.release_id = $1
ORDER BY rpr.position ASC, rpr.added_at ASC;

-- name: GetActiveReleaseForPullRequest :one
-- Returns the ACTIVE release a PR belongs to, if any. The partial
-- unique index guarantees at most one row. Used by the per-PR card
-- "release badge" — it surfaces "🚂 in <release_title>" on every
-- card whose PR is currently in flight.
SELECT r.*
FROM ship_release_pull_request rpr
JOIN ship_release r ON r.id = rpr.release_id
WHERE rpr.pull_request_id = $1 AND rpr.is_active = TRUE
LIMIT 1;

-- name: ListActiveReleasesForPullRequests :many
-- Batch variant: take a set of PR ids and return the active release
-- for each (zero or one). Powers the bulk-decoration of the Kanban
-- so we don't N+1.
SELECT
    rpr.pull_request_id,
    r.id          AS release_id,
    r.title       AS release_title,
    r.stage       AS release_stage,
    r.project_id  AS release_project_id
FROM ship_release_pull_request rpr
JOIN ship_release r ON r.id = rpr.release_id
WHERE rpr.pull_request_id = ANY($1::uuid[])
  AND rpr.is_active = TRUE;

-- name: DeactivateReleasePullRequests :exec
-- Flip is_active=FALSE on every join row for a release. Used when the
-- release reaches a terminal stage so the partial unique index frees
-- up those PRs for new releases.
UPDATE ship_release_pull_request
SET is_active = FALSE
WHERE release_id = $1;

-- name: CountActiveReleasePullRequests :one
SELECT COUNT(*)::int AS pr_count
FROM ship_release_pull_request
WHERE release_id = $1;

-- name: InsertReleaseEvent :one
INSERT INTO ship_release_event (
    release_id, event_type, actor_user_id, payload
) VALUES (
    $1, $2, sqlc.narg('actor_user_id'), sqlc.narg('payload')
)
RETURNING *;

-- name: ListReleaseEvents :many
-- Newest-first; the detail timeline panel renders top-down.
SELECT * FROM ship_release_event
WHERE release_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- ---------------------------------------------------------------------------
-- Phase 7b — Merge train orchestration.
-- ---------------------------------------------------------------------------

-- name: SetReleaseMergePaused :one
-- Flip the soft-state pause flag. Used when a PR fails mid-train
-- (paused=TRUE) and on resume (paused=FALSE). updated_at bumps so the
-- workspace rail re-orders.
UPDATE ship_release SET
    merge_paused = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetReleaseMergeMethod :one
-- Stamps the merge method that the orchestrator will use for this
-- release. Called once at start_merge time; ignored by the orchestrator
-- thereafter. Returns the row so the handler can echo it.
UPDATE ship_release SET
    merge_method = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: NextQueuedReleasePR :one
-- Picks the next PR to merge from a release: the lowest-position row
-- whose merge_state is still 'queued'. Bounded to one row by LIMIT 1.
-- The orchestrator calls this in a loop until it returns no row.
SELECT
    rpr.release_id,
    rpr.pull_request_id,
    rpr.position
FROM ship_release_pull_request rpr
WHERE rpr.release_id = $1
  AND rpr.merge_state = 'queued'
ORDER BY rpr.position ASC
LIMIT 1;

-- name: SetReleasePRMergeState :one
-- Drives the per-PR merge_state machine: queued → merging → merged
-- (with sha + ts), failed (with error), or skipped. Caller passes the
-- merge_state via the typed string; sqlc maps it through the enum.
UPDATE ship_release_pull_request SET
    merge_state = $3,
    merged_sha = COALESCE(sqlc.narg('merged_sha'), merged_sha),
    merged_at = COALESCE(sqlc.narg('merged_at'), merged_at),
    merge_error = COALESCE(sqlc.narg('merge_error'), merge_error)
WHERE release_id = $1 AND pull_request_id = $2
RETURNING *;

-- name: CountReleasePRsByMergeState :many
-- Returns one row per merge_state value present in this release with
-- a count. Used by the merge_state poll endpoint and by the WS
-- payloads to surface "merged_count / total" without N round-trips.
SELECT
    merge_state,
    COUNT(*)::int AS count
FROM ship_release_pull_request
WHERE release_id = $1
GROUP BY merge_state;

-- name: ListReleasePRsForMerge :many
-- Lite shape for the merge orchestrator: ordered by position, with
-- only the columns the goroutine actually needs. We avoid pulling
-- the full PR row on every iteration because the orchestrator can
-- pick the PR up via GetPullRequest only when it's about to merge it.
SELECT
    rpr.pull_request_id,
    rpr.position,
    rpr.merge_state
FROM ship_release_pull_request rpr
WHERE rpr.release_id = $1
ORDER BY rpr.position ASC;

-- ---------------------------------------------------------------------------
-- Phase 7c — Staging deploy linkage, smoke tests, manual verify gate.
-- ---------------------------------------------------------------------------

-- name: SetReleaseMergedMainSHA :one
-- Stamps the SHA of the merge commit produced by the LAST PR in the
-- train. Deploy webhook handlers match deploy.sha against this to
-- discover the release a deploy belongs to. updated_at bumps so the
-- workspace rail re-orders.
UPDATE ship_release SET
    merged_main_sha = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: FindReleaseByMergedMainSHA :one
-- Reverse lookup used by the deployment_status webhook: given a
-- successful staging deploy's sha, find the release that produced
-- it. Constrained to non-terminal releases so a stale sha from an
-- old release doesn't get re-linked. The composite WHERE matches the
-- partial index on merged_main_sha + the active-stage filter.
SELECT * FROM ship_release
WHERE workspace_id = $1
  AND merged_main_sha = $2
  AND merged_main_sha <> ''
  AND stage IN ('merging', 'in_staging', 'verifying')
ORDER BY updated_at DESC
LIMIT 1;

-- name: FindReleaseBySmokeRunID :one
-- Reverse lookup used by the check_run webhook: given a workflow run
-- id, find the release whose smoke_run_id matches. Workspace-scoped
-- so we never match across tenants.
SELECT * FROM ship_release
WHERE workspace_id = $1
  AND smoke_run_id = $2
  AND smoke_run_id <> ''
LIMIT 1;

-- name: SetReleaseStagingDeploy :one
-- Records the linked staging deploy + stamps staged_at. Called by the
-- deployment_status webhook handler when it matches a deploy's sha
-- to a release's merged_main_sha. staged_at is COALESCE'd so a
-- delayed deploy_status doesn't overwrite a value the merge train
-- might have already written.
UPDATE ship_release SET
    staging_deploy_id = $2,
    staged_at = COALESCE(staged_at, $3),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetReleaseSmokeRun :one
-- Records the workflow_dispatch result so the UI can surface a deep
-- link and the check_run webhook has something to match against.
UPDATE ship_release SET
    smoke_run_id = $2,
    smoke_run_url = $3,
    smoke_status = $4,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetReleaseSmokeStatus :one
-- Lightweight smoke_status update. Used by the check_run webhook
-- handler to flip from "queued" → "in_progress" → "completed_success"
-- / "completed_failure", and by the manual_pass / unverify flows.
-- smoke_completed_at gets COALESCE-narg'd so a status flip without a
-- ts (in_progress, manual_pass-after-failure) leaves the prior ts
-- intact rather than nulling it.
UPDATE ship_release SET
    smoke_status = $2,
    smoke_completed_at = COALESCE(sqlc.narg('smoke_completed_at'), smoke_completed_at),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetReleaseQAVerified :one
-- Stamps the QA verification fields. Called by mark_verified;
-- unverify clears via SetReleaseQAUnverified below. Caller is
-- responsible for transitioning stage in a paired UpdateReleaseStage
-- call so the audit log records both moves coherently.
UPDATE ship_release SET
    qa_verified_at = $2,
    qa_verified_by = $3,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetReleaseQAUnverified :one
-- Clears qa_verified_at + qa_verified_by. Caller flips stage back to
-- in_staging via UpdateReleaseStage in the same service-layer flow.
UPDATE ship_release SET
    qa_verified_at = NULL,
    qa_verified_by = NULL,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetLastMergedReleasePR :one
-- Returns the highest-position membership row whose merge_state =
-- 'merged'. Used by the merge train at completion time to derive the
-- merged_main_sha — it's the LAST PR in the train (by position) that
-- actually landed on main, not necessarily the highest position
-- across all rows (skipped/failed don't produce a sha).
SELECT
    rpr.release_id,
    rpr.pull_request_id,
    rpr.position,
    rpr.merged_sha
FROM ship_release_pull_request rpr
WHERE rpr.release_id = $1
  AND rpr.merge_state = 'merged'
  AND rpr.merged_sha IS NOT NULL
  AND rpr.merged_sha <> ''
ORDER BY rpr.position DESC
LIMIT 1;

-- ---------------------------------------------------------------------------
-- Phase 7d — Production promotion, post-deploy health, rollback.
-- ---------------------------------------------------------------------------

-- name: SetReleasePromoted :one
-- Stamps the production deploy linkage + the promoted_at / promoted_by
-- pair when the user clicks Promote (or when a webhook auto-links).
-- production_deploy_id may be the zero UUID at click-time (we record
-- the intent before any deploy lands) and gets filled by
-- LinkProductionDeploy when the deployment_status webhook arrives.
UPDATE ship_release SET
    production_deploy_id = COALESCE(sqlc.narg('production_deploy_id'), production_deploy_id),
    production_main_sha  = COALESCE(sqlc.narg('production_main_sha'), production_main_sha),
    promoted_at          = COALESCE(promoted_at, $2),
    promoted_by          = COALESCE(promoted_by, sqlc.narg('promoted_by')),
    updated_at           = NOW()
WHERE id = $1
RETURNING *;

-- name: SetReleaseInProduction :one
-- Final flip when the production deploy is confirmed landed. Caller
-- pairs with UpdateReleaseStage(stage='in_production') in the same
-- service-layer flow.
UPDATE ship_release SET
    promoted_at = COALESCE(promoted_at, $2),
    updated_at  = NOW()
WHERE id = $1
RETURNING *;

-- name: SetReleaseRolledBack :one
-- Records the user's decision to roll back. Phase 7d v1 sets BOTH
-- rolled_back_at and rolled_back_completed_at to the same timestamp
-- because we don't auto-revert — the moment the user clicks, the
-- release is treated as rolled back. v2's auto-revert orchestrator
-- will instead set rolled_back_at on click and let
-- SetReleaseRolledBackComplete fill the completed_at when the reverts
-- merge.
UPDATE ship_release SET
    rolled_back_by           = COALESCE(rolled_back_by, sqlc.narg('rolled_back_by')),
    rollback_reason          = COALESCE(sqlc.narg('rollback_reason'), rollback_reason),
    rolled_back_completed_at = $2,
    updated_at               = NOW()
WHERE id = $1
RETURNING *;

-- name: SetReleaseRolledBackComplete :one
-- Phase 7e hook — sets rolled_back_completed_at when the v2 orchestrator
-- finishes merging revert PRs. v1 doesn't call this directly but the
-- column needs the targeted update path so the orchestrator can land
-- without further migrations.
UPDATE ship_release SET
    rolled_back_completed_at = $2,
    updated_at               = NOW()
WHERE id = $1
RETURNING *;

-- name: SetReleaseDone :one
-- Final flip when 24h elapses post-promote without a rollback.
UPDATE ship_release SET
    done_at    = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: FindReleaseByProductionMainSHA :one
-- Webhook linkage lookup — given a successful production deploy's sha,
-- find the release that produced it. Mirrors FindReleaseByMergedMainSHA
-- but constrained to production-side stages so a stale sha from an old
-- release doesn't get re-linked.
SELECT * FROM ship_release
WHERE workspace_id = $1
  AND (
      production_main_sha = $2
      OR (
          (production_main_sha IS NULL OR production_main_sha = '')
          AND merged_main_sha = $2
      )
  )
  AND $2 <> ''
  AND stage IN ('verifying', 'promoting', 'in_production')
ORDER BY updated_at DESC
LIMIT 1;

-- name: ListReleasesPastMonitoringWindow :many
-- The release finalizer goroutine reads this every 15 minutes. Returns
-- in_production releases whose promoted_at is older than the supplied
-- threshold AND have no rolled_back_at set. Caller transitions each
-- to stage='done'.
SELECT * FROM ship_release
WHERE stage = 'in_production'
  AND promoted_at IS NOT NULL
  AND promoted_at < $1
  AND rolled_back_completed_at IS NULL;

-- name: ListInProductionReleases :many
-- Used by the health monitor rollup goroutine. Cheap because in_production
-- releases are at most a handful per workspace at any time. We scope by
-- the ship_release table directly rather than joining workspaces.
SELECT * FROM ship_release
WHERE stage = 'in_production'
ORDER BY promoted_at DESC NULLS LAST;

-- name: UpdatePRRevertState :one
-- Used by a future revert orchestrator. v1 calls this once on
-- rollback initiation to mark every still-merged PR as 'pending', so
-- the UI can show "revert needed" affordances per-PR.
UPDATE ship_release_pull_request SET
    revert_state     = $3,
    revert_pr_number = COALESCE(sqlc.narg('revert_pr_number'), revert_pr_number),
    revert_pr_url    = COALESCE(sqlc.narg('revert_pr_url'), revert_pr_url),
    revert_error     = COALESCE(sqlc.narg('revert_error'), revert_error)
WHERE release_id = $1 AND pull_request_id = $2
RETURNING *;

-- name: ListReleasePRsByMergeOrderDesc :many
-- For rollback. Returns the merged PRs in REVERSE merge order so the
-- caller (or the user clicking GitHub's Revert button per-PR) reverts
-- the last-merged commit first. Skipped / failed PRs aren't reverted —
-- they didn't land.
SELECT
    rpr.release_id,
    rpr.pull_request_id,
    rpr.position,
    rpr.merged_sha,
    rpr.merged_at,
    rpr.merge_state,
    rpr.revert_state,
    rpr.revert_pr_number,
    rpr.revert_pr_url,
    pr.pr_number,
    pr.title,
    pr.html_url
FROM ship_release_pull_request rpr
JOIN pull_request pr ON pr.id = rpr.pull_request_id
WHERE rpr.release_id = $1
  AND rpr.merge_state = 'merged'
ORDER BY rpr.position DESC, rpr.merged_at DESC;

-- name: UpsertReleaseHealth :one
-- Health rollup write. Idempotent on release_id (PRIMARY KEY) so the
-- 5-minute health monitor can simply call this with each tick's
-- computed values.
INSERT INTO ship_release_health (
    release_id, workspace_id,
    error_rate_delta, p99_latency_delta_ms,
    inbox_issues_since_promote, agent_failure_rate_delta,
    overall_status, snapshot_at
) VALUES (
    $1, $2,
    sqlc.narg('error_rate_delta'), sqlc.narg('p99_latency_delta_ms'),
    $3, sqlc.narg('agent_failure_rate_delta'),
    $4, NOW()
)
ON CONFLICT (release_id) DO UPDATE SET
    workspace_id              = EXCLUDED.workspace_id,
    error_rate_delta          = EXCLUDED.error_rate_delta,
    p99_latency_delta_ms      = EXCLUDED.p99_latency_delta_ms,
    inbox_issues_since_promote = EXCLUDED.inbox_issues_since_promote,
    agent_failure_rate_delta  = EXCLUDED.agent_failure_rate_delta,
    overall_status            = EXCLUDED.overall_status,
    snapshot_at               = NOW()
RETURNING *;

-- name: GetReleaseHealth :one
-- Reads the latest rollup for a release. Returns pgx.ErrNoRows when
-- the monitor hasn't written one yet (release just promoted).
SELECT * FROM ship_release_health
WHERE release_id = $1;

