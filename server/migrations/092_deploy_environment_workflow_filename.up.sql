-- Per-environment deploy-workflow filename.
--
-- The previous design (migration 091) put deploy_workflow_filename on
-- the workspace row, which forced every project in a workspace to
-- share the same workflow filename. Wrong for any multi-project
-- workspace — each project has its own GitHub repo, with potentially
-- different `.github/workflows/*.yml` files. A workspace like
-- "Multica + RoastConsole Cloud + RC Mobile + Control Panel" can't
-- agree on one filename because each repo's workflow is named
-- differently.
--
-- This migration moves the setting to deploy_environment (one row per
-- {project, kind=staging|production}), which is the natural granularity
-- the poller already iterates. The workspace columns are KEPT as a
-- fallback for back-compat: poller prefers the env's value, then
-- falls back to the workspace setting if env's value is null. Future
-- migration can drop the workspace columns once all workspaces have
-- migrated their settings to the env level.

ALTER TABLE deploy_environment
    ADD COLUMN deploy_workflow_filename TEXT;

-- Backfill: copy the workspace's per-kind setting onto every existing
-- environment row of the matching kind. Idempotent — re-running this
-- migration after a re-up would re-fill any nulls but not overwrite
-- env-specific overrides (the WHERE clause guards both sides).
UPDATE deploy_environment env
SET deploy_workflow_filename = ws.ship_hub_deploy_workflow_staging
FROM workspace ws
WHERE env.workspace_id = ws.id
  AND env.kind = 'staging'
  AND env.deploy_workflow_filename IS NULL
  AND ws.ship_hub_deploy_workflow_staging IS NOT NULL
  AND ws.ship_hub_deploy_workflow_staging <> '';

UPDATE deploy_environment env
SET deploy_workflow_filename = ws.ship_hub_deploy_workflow_production
FROM workspace ws
WHERE env.workspace_id = ws.id
  AND env.kind = 'production'
  AND env.deploy_workflow_filename IS NULL
  AND ws.ship_hub_deploy_workflow_production IS NOT NULL
  AND ws.ship_hub_deploy_workflow_production <> '';
