package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Bulk rollback work never takes an explicit table lock. Each transaction
// discards at most 128 projections and holds row locks for at most 2s of SQL
// execution. The same keep rule and atomic, monotone watermark are used by the
// bounded residual pass in 620 down. A failed batch or runner crash is retryable.
func pruneProjectMoveSearchDuplicates(ctx context.Context, pool *pgxpool.Pool) error {
	for {
		deleted, err := pruneProjectMoveSearchDuplicateBatch(ctx, pool)
		if err != nil {
			return fmt.Errorf("prune project move search duplicates: %w", err)
		}
		if deleted == 0 {
			return nil
		}
	}
}

func pruneProjectMoveSearchDuplicateBatch(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout='2s'; SET LOCAL lock_timeout='2s'"); err != nil {
		return 0, err
	}
	var deleted int
	err = tx.QueryRow(ctx, `WITH candidates AS MATERIALIZED (
 SELECT a.ctid FROM search_index_change a
 WHERE EXISTS (
  SELECT 1 FROM search_index_change b
  WHERE a.entity_type=b.entity_type AND a.entity_id=b.entity_id
  AND (a.change_xid,a.workspace_id)<(b.change_xid,b.workspace_id)
 ) LIMIT 128 FOR UPDATE OF a
), deleted AS (
 DELETE FROM search_index_change a USING candidates c WHERE a.ctid=c.ctid
 RETURNING a.change_xid
), mark AS (
 INSERT INTO search_index_prune_mark(singleton,pruned_through_xid)
 SELECT true,change_xid FROM deleted ORDER BY change_xid DESC LIMIT 1
 ON CONFLICT(singleton) DO UPDATE SET pruned_through_xid=
 GREATEST(search_index_prune_mark.pruned_through_xid,EXCLUDED.pruned_through_xid)
)
SELECT count(*) FROM deleted`).Scan(&deleted)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return deleted, nil
}
