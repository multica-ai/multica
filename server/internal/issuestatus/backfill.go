package issuestatus

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// backfillAdvisoryLockKey serializes the boot-time reconcile across replicas.
// During a rolling deploy several new pods start at once; the winner walks
// every workspace while the losers observe the held lock and skip. The exact
// value is arbitrary — it just has to be stable and not collide with other
// advisory-lock users (e.g. taskusagebackfill's 4246). It encodes MUL-4809.
const backfillAdvisoryLockKey int64 = 4809

// Backfill ensures every existing workspace carries its built-in issue
// statuses. It is a one-shot, idempotent reconcile meant to run once at server
// boot so workspaces created before this feature shipped get seeded; new
// workspaces are seeded at creation time by Ensure.
//
// A Postgres session-level advisory lock keeps a single replica doing the walk;
// other replicas return (0, nil) immediately. The lock is released on the same
// pinned connection before it returns to the pool. Returns the number of
// workspaces reconciled (0 when another replica held the lock).
func Backfill(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire conn: %w", err)
	}
	// LIFO: unlock runs before Release, so the advisory lock is dropped on this
	// same session before the connection goes back to the pool. A background
	// context keeps the unlock alive even if ctx was cancelled mid-walk.
	defer conn.Release()

	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", backfillAdvisoryLockKey).Scan(&locked); err != nil {
		return 0, fmt.Errorf("try advisory lock: %w", err)
	}
	if !locked {
		return 0, nil
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", backfillAdvisoryLockKey)
	}()

	q := db.New(conn)
	ids, err := q.ListAllWorkspaceIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	// The id snapshot may go stale (a workspace deleted between the list and its
	// Ensure), but that cannot orphan statuses: Ensure takes FOR KEY SHARE on the
	// workspace row and inserts nothing when the row is already gone. So each
	// per-workspace seed is self-guarding; no workspace-level lock is needed here.
	for _, id := range ids {
		if err := Ensure(ctx, q, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
