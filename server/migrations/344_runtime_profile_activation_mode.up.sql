-- Workspace profiles keep their existing fan-out behavior. Local profiles are
-- opt-in on each daemon through `runtime profile set-path`, so an application
-- runtime can bind only the computers where the user explicitly enabled it.
ALTER TABLE runtime_profile
    ADD COLUMN activation_mode TEXT NOT NULL DEFAULT 'workspace'
    CHECK (activation_mode IN ('workspace', 'local'));
