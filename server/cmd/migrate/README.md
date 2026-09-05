# Migration runner operations

## Recover the comment content search index

Migration 371 keeps exactly one comment-content search index per environment:
`idx_comment_content_bigm` when `pg_bigm` is usable, otherwise the portable
`idx_comment_content_trgm` fallback. A conditionally skipped migration is still
recorded in `schema_migrations`, so rerunning `migrate up` does not recreate the
fallback if the selected bigram index is later dropped or becomes invalid.

First check whether either index is live, ready, and valid:

```sql
SELECT indexrelid::regclass AS index_name, indisvalid, indisready, indislive
FROM pg_index
WHERE indexrelid IN (
    to_regclass('idx_comment_content_bigm'),
    to_regclass('idx_comment_content_trgm')
);
```

If neither index is usable, restore the portable fallback before serving search
traffic. Run each statement separately and outside a transaction so the
concurrent index build is valid:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
DROP INDEX CONCURRENTLY IF EXISTS idx_comment_content_trgm;
CREATE INDEX CONCURRENTLY idx_comment_content_trgm
    ON comment USING gin (LOWER(content) gin_trgm_ops);
```

Verify that `idx_comment_content_trgm` reports all three flags as `true` before
resuming traffic. If `idx_comment_content_bigm` is repaired later, keep the
fallback until the bigram index also reports all three flags as `true` **and**
has the exact migration 036 shape: a non-unique, non-partial GIN index on
`LOWER(content)` using the `pg_bigm`-owned `gin_bigm_ops` operator class. Only
then can the fallback be dropped with `DROP INDEX CONCURRENTLY` during a
maintenance window.

## Build the issue properties bigram index after installing pg_bigm

Migration 446 builds `idx_issue_properties_bigm`, the index behind the prefilter
that scalar `contains` property filtering puts in front of its per-key ILIKE.
The runner only executes it where the `gin_bigm_ops` operator class is
installed; everywhere else the version is recorded with its SQL skipped, and the
filter keeps working without index acceleration.

That record is permanent, so a database that gains `pg_bigm` later never builds
the index on its own. Check first:

```sql
SELECT indexrelid::regclass AS index_name, indisvalid, indisready, indislive
FROM pg_index
WHERE indexrelid = to_regclass('idx_issue_properties_bigm');
```

If the index is missing, create it out of band. Run each statement separately
and outside a transaction so the concurrent build is valid:

```sql
CREATE EXTENSION IF NOT EXISTS pg_bigm;
DROP INDEX CONCURRENTLY IF EXISTS idx_issue_properties_bigm;
CREATE INDEX CONCURRENTLY idx_issue_properties_bigm
    ON issue USING gin (LOWER(properties::text) gin_bigm_ops);
ANALYZE issue;
```

The `ANALYZE` is not optional and not a formality. Building an expression index
does not collect statistics for its expression, and until they exist the
planner has nothing to judge the index by: it falls back to a pattern-length
heuristic that estimates a single-character needle at 5% of the table and
leaves `contains` on a sequential scan, with the index built, valid and unused.
Migration 447 does this after 446; an out-of-band build has to do it itself.

Keep the expression exactly as written: the predicate is
`LOWER(properties::text) LIKE LOWER(...)`, and an `ILIKE`-shaped or
non-lowered index would never be used (pg_bigm 1.2 on RDS has no ILIKE index
scan — the constraint migration 036 hit).

Confirm the statistics landed:

```sql
SELECT count(*) FROM pg_statistic WHERE starelid = 'idx_issue_properties_bigm'::regclass;
```
