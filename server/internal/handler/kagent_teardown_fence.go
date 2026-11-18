package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This namespace is distinct from Qoder's configuration lock (seed 19).
const teardownFenceKey = `hashtextextended($1::uuid::text || ':kagent_teardown', 0)`

var errTeardownFenceBusy = errors.New("workspace teardown is in progress; retry shortly")

// NewTeardownFencePool uses the writable database's connection configuration,
// with separate capacity so remote cleanup cannot exhaust application slots.
// The deployment must provide session affinity (not transaction pooling).
func NewTeardownFencePool(ctx context.Context, primary *pgxpool.Config) (*pgxpool.Pool, error) {
	cfg := primary.Copy()
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.MinIdleConns = 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

type teardownFence struct {
	conn      *pgxpool.Conn
	workspace string
	once      sync.Once
}

func discardFenceConn(conn *pgxpool.Conn) {
	raw := conn.Hijack()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = raw.Close(ctx)
}

func (h *Handler) acquireTeardownFence(ctx context.Context, workspace string) (*teardownFence, error) {
	if h.TeardownFencePool == nil {
		return nil, errors.New("teardown fence pool is unavailable")
	}
	acquireCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := h.TeardownFencePool.Acquire(acquireCtx)
	if err != nil {
		return nil, err
	}
	var locked bool
	err = conn.QueryRow(acquireCtx, `SELECT pg_try_advisory_lock(`+teardownFenceKey+`)`, workspace).Scan(&locked)
	if err != nil {
		discardFenceConn(conn)
		return nil, err
	}
	if !locked {
		conn.Release()
		return nil, errTeardownFenceBusy
	}
	return &teardownFence{conn: conn, workspace: workspace}, nil
}

func (f *teardownFence) Close() {
	f.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// A cancelled request may have left a transaction aborted. Roll it back
		// before unlocking; never release uncertain session ownership to the pool.
		if f.conn.Conn().PgConn().TxStatus() != 'I' {
			if _, err := f.conn.Exec(ctx, "ROLLBACK"); err != nil {
				discardFenceConn(f.conn)
				return
			}
		}
		var unlocked bool
		if err := f.conn.QueryRow(ctx, `SELECT pg_advisory_unlock(`+teardownFenceKey+`)`, f.workspace).Scan(&unlocked); err != nil || !unlocked {
			discardFenceConn(f.conn)
			return
		}
		f.conn.Release()
	})
}

func (h *Handler) requireTeardownFence(w http.ResponseWriter, ctx context.Context, workspace string) (*teardownFence, bool) {
	f, err := h.acquireTeardownFence(ctx, workspace)
	if err != nil {
		if errors.Is(err, errTeardownFenceBusy) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			slog.Error("workspace teardown fence acquire failed", "workspace", workspace, "error", err)
			writeError(w, http.StatusServiceUnavailable, "cannot acquire workspace teardown fence; retry shortly")
		}
		return nil, false
	}
	return f, true
}

func tryEnrollmentFence(w http.ResponseWriter, ctx context.Context, tx pgx.Tx, workspace string) bool {
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(`+teardownFenceKey+`)`, workspace).Scan(&locked); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot acquire workspace enrollment fence")
		return false
	}
	if !locked {
		writeError(w, http.StatusConflict, errTeardownFenceBusy.Error())
		return false
	}
	return true
}

// Request cancellation must not leave an open transaction ahead of unlock.
func rollbackTeardownTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
