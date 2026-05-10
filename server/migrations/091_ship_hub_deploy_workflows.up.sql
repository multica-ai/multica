-- Phase 7d follow-up — auto-detect deploys by polling GitHub Actions
-- workflow runs. Today users have to click "Mark deploy as landed"
-- because CI providers (Vercel / Netlify / Cloudflare / custom)
-- don't fire GitHub `deployment_status` webhooks. Configuring the
-- workflow filename here lets Multica poll completed runs on main
-- and auto-link them to releases by sha.
--
-- These are workflow paths relative to the repo's `.github/workflows/`
-- directory, e.g. "staging-deploy.yml" or just "production.yml".
-- Empty / NULL means the auto-poll is off for that environment;
-- the manual "Mark deployed" button stays as the fallback.
ALTER TABLE workspace ADD COLUMN ship_hub_deploy_workflow_staging    TEXT;
ALTER TABLE workspace ADD COLUMN ship_hub_deploy_workflow_production TEXT;
