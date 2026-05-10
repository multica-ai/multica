-- Ship Hub Phase 7d — Production promotion, post-deploy health rollup,
-- and rollback. Closes the release flow: a release that reached the
-- verifying stage can be promoted to production with one click,
-- monitored over a 24h window, and rolled back if something breaks.
--
-- Most of Phase 7d is per-release telemetry on top of the existing
-- ship_release row plus a new health-rollup table. The columns we add
-- here mirror the staging-stage columns from Phase 7c — the goal is
-- that the production half of a release is observable with the same
-- shape the staging half already exposed.
--
-- Note on rollback v1: we deliberately do NOT auto-create revert PRs in
-- Phase 7d. Generating a true revert via the GitHub REST API requires
-- assembling new tree objects (no "create revert PR" endpoint), and the
-- correctness/idempotency surface is significant. v1 records the user's
-- rollback intent + posts the merged-PR list to the channel; the user
-- clicks GitHub's per-PR "Revert" button. Phase 7e or later can build
-- the auto-revert orchestrator — the per-PR `revert_state` columns we
-- add here will already be in place when it lands.

-- promoted_by records the user who clicked Promote. The existing
-- promoted_at column from Phase 7a was for the stage transition; we
-- keep it and just populate it on click of the Promote button now.
ALTER TABLE ship_release ADD COLUMN promoted_by UUID REFERENCES "user"(id) ON DELETE SET NULL;

-- production_main_sha is the sha that landed in production. Usually
-- equal to merged_main_sha (we promote the same commit), but tracking
-- it separately handles the case where a hotfix or revert sha lands in
-- prod that's different from what was merged.
ALTER TABLE ship_release ADD COLUMN production_main_sha TEXT;

-- Rollback metadata. rolled_back_at marks when the rollback orchestrator
-- begins (intent recorded); rolled_back_completed_at is when reverts
-- have all landed. The gap between them is the "rollback in flight"
-- state. v1 sets both at the same time because we don't auto-merge
-- reverts — the moment the user clicks Rollback, the release is
-- treated as rolled back. The completed-at column is in place for the
-- v2 orchestrator's two-phase shape.
ALTER TABLE ship_release ADD COLUMN rolled_back_by UUID REFERENCES "user"(id) ON DELETE SET NULL;
ALTER TABLE ship_release ADD COLUMN rolled_back_completed_at TIMESTAMPTZ;

-- Per-PR revert tracking on the join table. Mirrors merge_state's
-- discrete machine. Today's v1 only flips state="pending" when the
-- rollback is initiated; phase 7e can set in_progress / reverted /
-- failed / skipped from the orchestrator. The columns are nullable so
-- existing rows that pre-date Phase 7d don't need a backfill.
CREATE TYPE pr_revert_state AS ENUM (
    'pending',
    'in_progress',
    'reverted',
    'failed',
    'skipped'
);
ALTER TABLE ship_release_pull_request ADD COLUMN revert_state pr_revert_state;
ALTER TABLE ship_release_pull_request ADD COLUMN revert_pr_number INTEGER;
ALTER TABLE ship_release_pull_request ADD COLUMN revert_pr_url TEXT;
ALTER TABLE ship_release_pull_request ADD COLUMN revert_error TEXT;

-- Health rollup — aggregated 24h post-deploy signals at the release
-- level. Phase 5 has per-deploy snapshots (deploy_health_snapshot);
-- this is the user-facing release-level summary the release page
-- renders + the periodic finalizer reads to decide done vs rollback.
--
-- All deltas are vs baseline; negative = improvement, positive =
-- regression. Each is nullable so a missing signal renders "—" rather
-- than a fake zero.
CREATE TABLE ship_release_health (
    release_id    UUID PRIMARY KEY REFERENCES ship_release(id) ON DELETE CASCADE,
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    snapshot_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    error_rate_delta             DOUBLE PRECISION,
    p99_latency_delta_ms         DOUBLE PRECISION,
    inbox_issues_since_promote   INTEGER NOT NULL DEFAULT 0,
    agent_failure_rate_delta     DOUBLE PRECISION,
    -- One of: "ok" | "warning" | "alert". Drives the release page's
    -- color treatment + whether the rollback affordance is highlighted.
    overall_status               TEXT NOT NULL DEFAULT 'ok'
);

-- Lookup index on (workspace_id, overall_status, snapshot_at). Powers
-- "any unhealthy releases?" lookups for the workspace-wide rail and
-- keeps the alert-status sweep cheap.
CREATE INDEX idx_ship_release_health_status_workspace
    ON ship_release_health(workspace_id, overall_status, snapshot_at DESC);

-- Lookup index — webhook handler scans by sha to find a matching
-- release in the production stages. Same partial-index rationale as
-- merged_main_sha: only releases that actually landed in production
-- carry a production_main_sha, so a partial index keeps the BTree
-- lean.
CREATE INDEX idx_ship_release_production_main_sha
    ON ship_release(production_main_sha)
    WHERE production_main_sha IS NOT NULL AND production_main_sha <> '';
