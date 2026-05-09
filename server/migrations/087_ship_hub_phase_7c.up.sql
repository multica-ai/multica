-- Ship Hub Phase 7c — Staging deploy linkage, smoke test signals, manual
-- verify gate. Phase 7b leaves the release in stage=in_staging once the
-- merge train finishes; 7c wires the rest of the staging story:
--
--   * `merged_main_sha`. The merge train records each PR's merge sha on
--     the membership row, but the LAST PR's merge sha is the commit
--     that the project's CI/CD will deploy to staging. We persist it
--     on the release at completion time so the deployment_status
--     webhook handler can match `deploy.sha == release.merged_main_sha`
--     and link the deploy back to the release. Without this, the
--     handler would have to scan every release's per-PR rows on every
--     deploy event — wasteful and racy.
--
--   * Smoke workflow run linkage. When the staging deploy lands and
--     the workspace has a smoke workflow configured, we trigger it
--     via workflow_dispatch. The check_run.completed webhook then
--     matches the run id back to the release so smoke_status flips
--     from "queued" to "completed_success" / "completed_failure".
--     `smoke_run_id` and `smoke_run_url` are TEXT (not FK) because
--     they reference a GitHub-side identifier we never store as a
--     row in our DB.
--
--   * `smoke_status`. Free-form text rather than enum so future
--     additions ("retrying", "skipped_via_label", etc.) don't require
--     a CREATE TYPE migration. Known values today:
--       ""                  — not yet run / no smoke configured
--       "queued"             — workflow dispatched, awaiting check_run
--       "in_progress"        — check_run started
--       "completed_success"  — passed
--       "completed_failure"  — failed (user can retry / manual_pass)
--       "skipped"            — workspace has no smoke workflow
--       "manual_pass"        — owner/admin marked it passing
--
--   * QA verification. `qa_verified_at` + `qa_verified_by` populated by
--     the mark_verified endpoint; transitions stage in_staging →
--     verifying. Reversible via unverify (verifying → in_staging).
--     The columns stay set across an unverify so a re-verify writes
--     the new actor over the old one.

ALTER TABLE ship_release ADD COLUMN smoke_run_id TEXT;
ALTER TABLE ship_release ADD COLUMN smoke_run_url TEXT;
ALTER TABLE ship_release ADD COLUMN smoke_status TEXT;
ALTER TABLE ship_release ADD COLUMN smoke_completed_at TIMESTAMPTZ;
ALTER TABLE ship_release ADD COLUMN qa_verified_at TIMESTAMPTZ;
ALTER TABLE ship_release ADD COLUMN qa_verified_by UUID REFERENCES "user"(id) ON DELETE SET NULL;
-- The merged_main_sha is the merge commit of the LAST PR in the train.
-- That's the SHA the project's CI/CD will deploy to staging — webhooks
-- listen for deploys whose sha matches THIS, so we can link the
-- deploy_id to the release.
ALTER TABLE ship_release ADD COLUMN merged_main_sha TEXT;

-- Lookup index — webhook handler scans by sha to find a matching
-- release. Partial because only releases that have actually merged
-- carry a sha; everything else is filtered out.
CREATE INDEX idx_ship_release_merged_main_sha
    ON ship_release(merged_main_sha)
    WHERE merged_main_sha IS NOT NULL AND merged_main_sha <> '';

-- Lookup index — check_run webhook scans by smoke_run_id. Same
-- partial-index rationale: only releases that triggered a workflow
-- ever populate this column.
CREATE INDEX idx_ship_release_smoke_run_id
    ON ship_release(smoke_run_id)
    WHERE smoke_run_id IS NOT NULL AND smoke_run_id <> '';
