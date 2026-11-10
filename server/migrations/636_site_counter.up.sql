-- Running totals shown on the public marketing site, keyed by name (e.g. how
-- many turns every visitor together has blown the footer star). Rows are
-- created on first increment.
CREATE TABLE IF NOT EXISTS site_counter (
    name TEXT PRIMARY KEY,
    value BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
