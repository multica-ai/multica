-- Project-level Ship Hub pipeline topology.
--
-- Pre-this-migration, "does this project have staging?" was answered
-- by querying for the existence of a deploy_environment row with
-- kind='staging'. That worked but was fragile: the poller used to
-- auto-create envs from workflow runs, which produced phantom staging
-- envs on direct-to-prod projects and parked their releases in
-- in_staging forever (PR #46 + #47 incident).
--
-- The fix in PR #47 made the env-creation path stricter, but the
-- inference itself remained indirect — the release flow was still
-- asking "does an env row exist?" rather than "what shape is this
-- project's pipeline?"
--
-- This migration makes pipeline topology an EXPLICIT first-class
-- attribute of a project. Release flow + UI both read this column
-- as the source of truth; env rows go back to being purely about
-- "where does code physically deploy to," not about "what stages a
-- release passes through."
--
-- Backfill: any project that already has a kind='staging' env stays
-- on `staged` (the conservative default for legacy data). Projects
-- without one are downgraded to `direct_to_prod`. Fresh projects
-- created post-migration default to `staged` since "more gates =
-- safer" is the right default until an operator opts out.
--
-- The two values cover today's needs:
--   * staged          → merging → in_staging → verifying → promoting → in_production → done
--   * direct_to_prod  → merging → promoting → in_production → done
-- More kinds (e.g. `staged_with_canary`) can be appended later
-- without another migration on existing data.

CREATE TYPE project_pipeline_kind AS ENUM ('staged', 'direct_to_prod');

ALTER TABLE project
    ADD COLUMN pipeline_kind project_pipeline_kind NOT NULL DEFAULT 'staged';

-- Backfill from current deploy_environment state. Projects with no
-- staging env get the direct-to-prod flow; projects with one keep the
-- default `staged`.
UPDATE project p
SET pipeline_kind = 'direct_to_prod'
WHERE NOT EXISTS (
    SELECT 1 FROM deploy_environment de
    WHERE de.project_id = p.id AND de.kind = 'staging'
);
