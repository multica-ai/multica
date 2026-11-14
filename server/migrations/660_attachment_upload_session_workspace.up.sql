-- Serves workspace teardown, which must hand every session of the workspace to
-- the reconciler before the workspace rows disappear. CONCURRENTLY in its own
-- file per the repo migration convention.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_attachment_upload_session_workspace
    ON attachment_upload_session (workspace_id);
