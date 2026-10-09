package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	sharedPoolMu sync.Mutex
	sharedPool   *pgxpool.Pool
)

func TestMain(m *testing.M) {
	code := m.Run()
	sharedPoolMu.Lock()
	pool := sharedPool
	sharedPoolMu.Unlock()
	if pool != nil {
		// Close waits for every acquired connection to come back, and m.Run has
		// already disarmed -timeout, so a leaked connection would hang the
		// binary here. Bound the wait and fail loudly instead.
		closed := make(chan struct{})
		go func() {
			pool.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			fmt.Fprintln(os.Stderr, "internal/service: the shared test pool did not close within 5s; a test leaked an acquired database connection")
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}

// sharedTestPool returns the package's one database pool, opened on first use,
// and skips the test when Postgres is unreachable so `go test ./...` stays
// usable without a database. A failed open is not remembered: the next test
// tries again, so one transient failure skips one test, not the package.
//
// DB tests in this package run serially and own their rows, so they share the
// pool rather than dialling a fresh one each: a new pool costs a connection
// handshake plus a cold statement cache and cold backend caches, which made
// small tests here several times slower than their handler-package twins.
func sharedTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := openSharedTestPool()
	if err != nil {
		t.Skip(err)
	}
	// A per-test pool hung in Close on a connection its test never released.
	// With one shared pool a leak would starve later tests instead, so report
	// it here. Releasing a busy connection destroys it asynchronously, hence
	// the short settle.
	held := pool.Stat().AcquiredConns()
	t.Cleanup(func() {
		deadline := time.Now().Add(time.Second)
		for pool.Stat().AcquiredConns() > held {
			if time.Now().After(deadline) {
				t.Errorf("test left %d database connection(s) acquired", pool.Stat().AcquiredConns()-held)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
	return pool
}

func openSharedTestPool() (*pgxpool.Pool, error) {
	sharedPoolMu.Lock()
	defer sharedPoolMu.Unlock()
	if sharedPool != nil {
		return sharedPool, nil
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("database unavailable: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database unreachable: %w", err)
	}
	sharedPool = pool
	return pool, nil
}
