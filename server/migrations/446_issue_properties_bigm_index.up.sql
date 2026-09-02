-- Bigram GIN index behind the lossy prefilter that scalar `contains` property
-- filtering puts in front of its per-key ILIKE check (MUL-6928). Without it a
-- contains filter cannot reach any index and degrades to a workspace-wide
-- sequential scan: on a 1M-issue workspace throughput drops ~8x against an
-- equality filter, which does reach idx_issue_properties_gin.
--
-- LOWER(...) LIKE, not ILIKE: pg_bigm 1.2 (RDS) has no ILIKE index scan, the
-- same constraint migration 036 hit — an ILIKE-shaped predicate would never use
-- this index. The pattern side is lowered in SQL as well so the prefilter folds
-- case exactly the way the ILIKE it guards does.
--
-- Single-statement file because a concurrent build cannot share a migration,
-- and for the same reason it cannot be wrapped in the DO ... EXCEPTION block
-- migrations 032 / 036 use to tolerate a missing pg_bigm. The runner gates this
-- version on the extension instead (issuePropertiesBigramOperatorClass in
-- cmd/migrate): environments without pg_bigm record it with the SQL skipped and
-- keep the unaccelerated — still correct — prefilter. Recovery for a database
-- that installs pg_bigm later is in cmd/migrate/README.md.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_properties_bigm
    ON issue USING gin (LOWER(properties::text) gin_bigm_ops);
