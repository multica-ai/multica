-- The daily cap counts one workspace's asks for one UTC day; the digest reads
-- the same rows for today and yesterday.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_brief_inquiry_workspace_asked ON brief_inquiry (workspace_id, asked_on);
