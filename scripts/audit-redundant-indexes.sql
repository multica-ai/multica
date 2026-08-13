-- Find valid, non-constraint-backed indexes whose complete key is identical to
-- or a strict left prefix of another index on the same public-schema table.
--
-- This deliberately compares access method, columns/expressions, opclasses,
-- collations, sort options and predicates. It does not decide that a prefix
-- candidate should be dropped: a narrower index can still be materially
-- smaller or have better production usage.
WITH idx AS (
    SELECT
        i.indexrelid,
        i.indrelid,
        c.relname AS idxname,
        t.relname AS tblname,
        c.relam AS am,
        i.indisunique,
        i.indisprimary,
        (
            SELECT con.conname
            FROM pg_constraint con
            WHERE con.conindid = i.indexrelid
            LIMIT 1
        ) AS conname,
        i.indnkeyatts AS nkey,
        i.indkey::int2[] AS keys,
        i.indcollation::oid[] AS colls,
        i.indclass::oid[] AS opcls,
        i.indoption::int2[] AS opts,
        pg_get_expr(i.indpred, i.indrelid) AS pred,
        pg_get_expr(i.indexprs, i.indrelid) AS exprs,
        pg_get_indexdef(i.indexrelid) AS def
    FROM pg_index i
    JOIN pg_class c ON c.oid = i.indexrelid
    JOIN pg_class t ON t.oid = i.indrelid
    JOIN pg_namespace n ON n.oid = t.relnamespace
    WHERE n.nspname = 'public'
      AND c.relkind = 'i'
      AND i.indisvalid
)
SELECT
    a.tblname,
    a.idxname AS redundant_index,
    CASE WHEN a.nkey = b.nkey THEN 'exact' ELSE 'prefix' END AS relation,
    b.idxname AS covered_by,
    CASE
        WHEN b.indisprimary THEN 'pkey'
        WHEN b.conname IS NOT NULL THEN 'constraint'
        WHEN b.indisunique THEN 'unique_idx'
        ELSE 'plain_idx'
    END AS covering_kind,
    a.def AS candidate_definition,
    b.def AS covering_definition
FROM idx a
JOIN idx b
  ON a.indrelid = b.indrelid
 AND a.indexrelid <> b.indexrelid
 AND a.am = b.am
WHERE NOT a.indisunique
  AND a.conname IS NULL
  AND a.nkey <= b.nkey
  AND a.keys[0:a.nkey - 1] = b.keys[0:a.nkey - 1]
  AND a.opcls[0:a.nkey - 1] = b.opcls[0:a.nkey - 1]
  AND a.colls[0:a.nkey - 1] = b.colls[0:a.nkey - 1]
  AND a.opts[0:a.nkey - 1] = b.opts[0:a.nkey - 1]
  AND COALESCE(a.pred, '') = COALESCE(b.pred, '')
  -- Expression columns have indkey=0, so compare the expressions too. Without
  -- this guard, unrelated expression indexes can be reported as duplicates.
  AND COALESCE(a.exprs, '') = COALESCE(b.exprs, '')
ORDER BY a.tblname, a.idxname, b.idxname;
