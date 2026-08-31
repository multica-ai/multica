-- Head SHA is selective enough to narrow the CI-webhook fallback to the few
-- mirrored rows whose installation and repository are then checked by SQL.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_github_pull_request_head_sha
    ON github_pull_request (head_sha);
