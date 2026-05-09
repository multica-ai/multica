-- Ship Hub Phase 7b — Merge train orchestration.
--
-- Phase 7a's release row carries enough state to model "assembling" and
-- the terminal stages; Phase 7b adds the in-flight bookkeeping needed
-- to drive a per-PR merge train end-to-end:
--
--   * `merge_paused` — soft-state flag on the release. When a PR fails
--     to merge mid-train, the orchestrator stops and sets this. The
--     stage stays "merging" because the user-facing summary is still
--     "merging" — they can resume without a new state machine entry.
--     Modeling this as a flag (not a new stage value) keeps the
--     release_stage enum tight and avoids the "every cancel/abort
--     path needs a new stage check" sprawl.
--
--   * `merge_method` — workspace-level default merge method for the
--     release. Defaults to "merge"; per-PR override deferred to a
--     future phase. Stored on the release rather than per-membership
--     because every PR in the train uses the same method today.
--     SQL CHECK constraint (rather than a Postgres enum) so adding
--     "rebase" or another GitHub-supported method later is a check
--     swap, not a CREATE TYPE migration.
--
--   * `pr_merge_state` (enum + column on the join row) — discrete
--     state machine for each PR's place in the train. Replaces the
--     bare merged_sha / merge_error indicator from 7a (those columns
--     stay so we don't wipe data, but the orchestrator drives the new
--     enum and the frontend reads the enum for its pill styling).
--     queued → merging → (merged | failed | skipped). failed PRs are
--     resumable via the user clicking "Resume" from the detail page;
--     skipped PRs are explicitly abandoned via "Skip & resume".

ALTER TABLE ship_release ADD COLUMN merge_paused BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE ship_release ADD COLUMN merge_method TEXT NOT NULL DEFAULT 'merge';
ALTER TABLE ship_release ADD CONSTRAINT ship_release_merge_method_check
    CHECK (merge_method IN ('merge', 'squash', 'rebase'));

CREATE TYPE pr_merge_state AS ENUM ('queued', 'merging', 'merged', 'failed', 'skipped');

ALTER TABLE ship_release_pull_request
    ADD COLUMN merge_state pr_merge_state NOT NULL DEFAULT 'queued';

-- Lookup index for the orchestrator's "next PR to merge" scan: queued
-- rows in a release, ordered by position. Partial because `merging`
-- and terminal states never need this read path.
CREATE INDEX idx_ship_release_pr_queued
    ON ship_release_pull_request(release_id, position)
    WHERE merge_state = 'queued';
