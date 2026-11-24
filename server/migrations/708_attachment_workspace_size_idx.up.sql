-- Attachment storage limit (MUL-9489): the workspace's used bytes are summed
-- on demand rather than kept in a counter, so cascade deletes are reflected at
-- once and nothing needs a backfill. Carrying size_bytes in the index lets the
-- sum run as an index-only scan. It also serves every query the plain
-- workspace_id index served; 709 drops that one.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_attachment_workspace_size
    ON attachment (workspace_id) INCLUDE (size_bytes);
