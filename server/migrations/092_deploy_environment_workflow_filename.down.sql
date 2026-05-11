-- Reverse 092: drop the per-env workflow filename column. The
-- workspace-level fallback (added in 091) stays, so rolling back loses
-- only env-specific overrides; envs that had their value backfilled
-- from the workspace setting are unaffected because the workspace
-- columns weren't touched.
ALTER TABLE deploy_environment
    DROP COLUMN deploy_workflow_filename;
