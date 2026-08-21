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
