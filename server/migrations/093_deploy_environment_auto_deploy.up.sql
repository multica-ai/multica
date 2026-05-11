-- Add per-env auto_deploy flag. When set, PromoteRelease's stage flip
-- (verifying → promoting) ALSO fires a workflow_dispatch against the
-- env's configured deploy_workflow_filename — turning Multica's
-- promote button into an actual "click → deploys to prod" button
-- instead of "click → waits for someone to click Run Workflow on GH".
--
-- Defaults to FALSE so existing envs preserve the tracking-only
-- behavior. Opt-in via the Configure deploy env dialog; requires
-- the workspace's GitHub PAT plus the env's deploy_workflow_filename
-- to be set (we don't gate on those at write time — the dispatch is
-- a no-op log warning if they're missing, so misconfiguration
-- doesn't break Promote).
ALTER TABLE deploy_environment
    ADD COLUMN auto_deploy BOOLEAN NOT NULL DEFAULT FALSE;
