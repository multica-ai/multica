-- TECH-3522: per-workspace web_fetch URL policy.
--
-- One row per workspace. `mode` selects allow-list ("allow" — only listed hosts
-- may be fetched) or disallow-list ("disallow" — everything except listed hosts).
-- `host_rules` is a JSON array of host rules ("github.com", "*.github.com").
-- A missing row means "use the seeded default" (allow-list with the legacy
-- hardcoded hosts), so behaviour is unchanged for workspaces that never touch it.
CREATE TABLE IF NOT EXISTS cerebro_web_fetch_policy (
    workspace_id UUID        PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    mode         TEXT        NOT NULL DEFAULT 'allow' CHECK (mode IN ('allow', 'disallow')),
    host_rules   JSONB       NOT NULL DEFAULT '[]'::jsonb,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   UUID
);
