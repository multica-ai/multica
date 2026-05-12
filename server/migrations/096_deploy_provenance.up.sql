-- Evidence-bound deploy events.
--
-- Pre-this-migration, deploy rows had no notion of HOW they were
-- learned about. Three different code paths wrote deploys:
--
--   1. The deploy workflow poller — saw a successful workflow_run on
--      GitHub and inserted a row.
--   2. `mark_production_deployed` (and `mark_release_staging_deployed`)
--      — a user (or agent) clicked a button asserting that a deploy
--      happened, and we inserted a synthetic row with no evidence at
--      all.
--   3. Deployment-status webhook (when fired) — inserted a row from
--      the webhook payload.
--
-- All three wrote `env.current_sha` directly, with no ordering check.
-- That meant a manual assertion for an OLDER commit could overwrite a
-- newer deploy's `current_sha` — and that's what happened on
-- 2026-05-12 when `mark_production_deployed` was clicked for PR #38's
-- release after PR #47 had already deployed: env.current_sha rolled
-- backwards to the older SHA and the deploys table grew a synthetic
-- row with no provenance whatsoever.
--
-- This migration adds two pieces of evidence to every deploy row:
--
--   * `provenance` enum — declares HOW the row was learned about.
--     Values: workflow_run | manual_assertion | webhook | legacy.
--   * `provenance_ref` text — the URL of the workflow run, the user's
--     assertion note, or the webhook delivery id. Required for the
--     workflow_run / webhook kinds (enforced in app layer); optional
--     for manual_assertion (the assertion note is free-form).
--
-- env.current_sha + current_deployed_at are NOT removed in this
-- migration — they stay as denormalized fields. But after this lands
-- the SOLE writer becomes a new query `RecomputeEnvCurrentFromDeploys`
-- that picks the latest succeeded deploy by triggered_at DESC. Calls
-- to `UpdateDeployEnvironmentCurrent` from outside that path are
-- audited out in a follow-up.
--
-- Backfill: deploys with a non-null log_url are assumed to be workflow
-- runs (provenance=workflow_run, provenance_ref=log_url). Deploys
-- without a log_url are legacy (provenance=legacy, provenance_ref=NULL).
-- Webhook-sourced deploys are indistinguishable from poller-sourced
-- by storage shape, so they all default to workflow_run if log_url is
-- set — accurate enough for backfill.

CREATE TYPE deploy_provenance AS ENUM (
    'workflow_run',
    'manual_assertion',
    'webhook',
    'legacy'
);

ALTER TABLE deploy
    ADD COLUMN provenance deploy_provenance NOT NULL DEFAULT 'legacy';

ALTER TABLE deploy
    ADD COLUMN provenance_ref TEXT;

-- Backfill: existing rows with a log_url → workflow_run.
-- The poller and webhook paths both populate log_url; the manual
-- mark_*_deployed paths don't (they pass an empty pgtype.Text). So
-- log_url presence is the discriminator we have for the back-fill.
UPDATE deploy
SET provenance = 'workflow_run',
    provenance_ref = log_url
WHERE log_url IS NOT NULL AND log_url <> '';

-- Index for the new "latest succeeded deploy per env" lookup that
-- env.current_sha will recompute through. Partial-index on
-- status='succeeded' keeps it small; ordering matches the query.
CREATE INDEX idx_deploy_env_succeeded_triggered
    ON deploy (environment_id, triggered_at DESC)
    WHERE status = 'succeeded';
