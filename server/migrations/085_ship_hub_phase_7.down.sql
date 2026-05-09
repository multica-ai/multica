-- Reverse 085_ship_hub_phase_7.up.sql. Drop in dependency order: events
-- and join table (which reference ship_release), then ship_release,
-- then the enum.
DROP INDEX IF EXISTS idx_ship_release_event_release;
DROP TABLE IF EXISTS ship_release_event;

DROP INDEX IF EXISTS idx_ship_release_pr_active_unique;
DROP INDEX IF EXISTS idx_ship_release_pr_pr;
DROP TABLE IF EXISTS ship_release_pull_request;

DROP INDEX IF EXISTS idx_ship_release_project_stage;
DROP INDEX IF EXISTS idx_ship_release_workspace_stage;
DROP TABLE IF EXISTS ship_release;

DROP TYPE IF EXISTS release_stage;
