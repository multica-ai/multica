-- How many times each community store theme has been downloaded from this
-- server: every install and every update counts once. Keyed by the registry's
-- theme id, so it is global, not workspace-owned. Rows are created on the
-- first download.
CREATE TABLE IF NOT EXISTS theme_store_download (
    theme_id TEXT PRIMARY KEY,
    downloads BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
