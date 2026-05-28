package service

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FeishuProjectSyncLocker is the minimal subset of *pgxpool.Pool needed to
// acquire a session-level advisory lock. Defined here so the worker and the
// manual-sync handler can share the same lock helper without leaking
// pgxpool-specific types through the rest of the service surface.
type FeishuProjectSyncLocker interface {
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
}

// TryAcquireFeishuProjectSyncLock attempts a session-level pg advisory lock
// keyed on the integration ID. The returned unlock function releases the
// advisory lock and the underlying pool connection; it is safe to call even
// when locked is false (no-op).
//
// Manual sync and the scheduled worker share this lock so the two paths
// cannot run concurrently against the same integration — that overlap is the
// root cause of the create-create race on new work items and of attachment
// double-inserts under multi-replica syncs of the same issue.
func TryAcquireFeishuProjectSyncLock(ctx context.Context, locker FeishuProjectSyncLocker, integrationID pgtype.UUID) (bool, func(), error) {
	key := "feishu-project-sync:" + UUIDString(integrationID)
	conn, err := locker.Acquire(ctx)
	if err != nil {
		return false, func() {}, err
	}

	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1, 0))", key).Scan(&locked); err != nil {
		conn.Release()
		return false, func() {}, err
	}
	if !locked {
		conn.Release()
		return false, func() {}, nil
	}

	unlock := func() {
		// Unlock with a fresh context — the caller's ctx may already be
		// cancelled (manual sync detaches into a 2h-timeout goroutine, and
		// the worker's outer ctx may be tearing down on shutdown).
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock(hashtextextended($1, 0))", key); err != nil {
			slog.Warn("Feishu Project sync unlock failed", "integration_id", UUIDString(integrationID), "error", err)
		}
		conn.Release()
	}
	return true, unlock, nil
}
