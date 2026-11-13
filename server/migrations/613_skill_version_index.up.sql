-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or a multi-command string.
--
-- One version number per skill. Serves the version list, latest-version
-- lookup, and per-skill cleanup on delete.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_skill_version_skill_version
    ON skill_version (skill_id, version);
