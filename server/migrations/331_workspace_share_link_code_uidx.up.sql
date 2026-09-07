-- Lookup index for joins/previews by invite code.
--
-- IF NOT EXISTS keeps a retry idempotent when the build committed but the
-- version was never recorded; an interrupted build's INVALID leftover is
-- dropped first by this migration's cleanup hook in cmd/migrate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_share_link_code_uidx
    ON workspace_share_link (code);
