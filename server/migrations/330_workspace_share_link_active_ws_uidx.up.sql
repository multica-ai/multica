-- Enforce at most one active share link per workspace. Partial unique index so
-- only active rows are constrained; revoked/expired links remain in place for
-- audit without blocking a new link.
-- IF NOT EXISTS keeps a retry idempotent when the build committed but the
-- version was never recorded; an interrupted build's INVALID leftover is
-- dropped first by this migration's cleanup hook in cmd/migrate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_share_link_active_ws_uidx
    ON workspace_share_link (workspace_id) WHERE is_active = true;
