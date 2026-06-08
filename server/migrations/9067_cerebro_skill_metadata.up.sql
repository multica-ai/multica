-- TECH-3077: Add structured metadata column to skill table.
-- Parsed from SKILL.md frontmatter (category, domain, tags, type, scope,
-- status, data_domains, triggered_by, auto_learn) and synced on every
-- create/update. GIN index makes category/domain/tag filtering fast.

ALTER TABLE skill ADD COLUMN IF NOT EXISTS
    metadata JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_skill_metadata_gin
    ON skill USING GIN (metadata);

CREATE INDEX IF NOT EXISTS idx_skill_metadata_domain
    ON skill ((metadata->>'domain'));

CREATE INDEX IF NOT EXISTS idx_skill_metadata_category
    ON skill ((metadata->>'category'));

CREATE INDEX IF NOT EXISTS idx_skill_metadata_status
    ON skill ((metadata->>'status'));
