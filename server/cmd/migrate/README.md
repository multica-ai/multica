# Migration runner operations

## Comment content search index retirement

Migrations 454 and 455 retire both historical comment-content search indexes:
`idx_comment_content_bigm` on pg_bigm deployments and the portable
`idx_comment_content_trgm` fallback. `SearchIssues` now scans comments through
`idx_comment_workspace` and evaluates content matches during aggregation, so it
no longer reads either global content GIN. Fresh installs also skip migration
140's historical fallback build. Do not repair or recreate these indexes after
applying migrations 454 and 455.

The down migrations restore exactly one historical index with
`CREATE INDEX CONCURRENTLY`: migration 454 restores the pg_bigm index where
`gin_bigm_ops` is available, while migration 455 restores the pg_trgm fallback
everywhere else.
Both restore the original `LOWER(content)` expression and retry safely after an
interrupted concurrent build.

These migrations run during backend startup. A concurrent drop waits for old
transactions, and the Helm startup probe allows ten minutes before restarting
the pod. For the multi-gigabyte production index, prefer a low-traffic window:
check for long-running transactions, then run each statement separately and
outside a transaction before deploying:

```sql
DROP INDEX CONCURRENTLY IF EXISTS idx_comment_content_bigm;
DROP INDEX CONCURRENTLY IF EXISTS idx_comment_content_trgm;
```

The subsequent migrations become fast no-ops. If startup performs the drop and
is interrupted instead, `IF EXISTS` makes the next run retry safely; one index
may remain until that retry completes. Dropping the large relation can also
produce a short I/O spike as storage is reclaimed.

An application rollback remains functionally correct without either index, but
legacy search can be much slower. Database rollback must rebuild the selected
GIN and is not immediate; verify the restored index is live, ready, and valid
before relying on the old query's performance:

```sql
SELECT indexrelid::regclass AS index_name, indisvalid, indisready, indislive
FROM pg_index
WHERE indexrelid IN (
    to_regclass('idx_comment_content_bigm'),
    to_regclass('idx_comment_content_trgm')
);
```

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
