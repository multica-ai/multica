package seatcapacity

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const workspaceLockReleaseTimeout = 3 * time.Second
const defaultWorkspaceCooldown = 75 * time.Millisecond
const workspaceLockPollInterval = 25 * time.Millisecond
const maxWorkspaceLockConnections = 4
const workspaceLockPoolFraction = 4

type WorkspaceLocker interface {
	Lock(context.Context, uuid.UUID) (db.DBTX, func(), error)
}

// postgresWorkspaceLocker serializes Cloud capacity calls for one workspace
// across API replicas. It pins a PostgreSQL session because advisory locks are
// session-scoped; returning the connection before unlock would leak the lock
// into an unrelated pool borrower.
type postgresWorkspaceLocker struct {
	pool *pgxpool.Pool

	mu         sync.Mutex
	localLocks map[uuid.UUID]*localWorkspaceLock
	// slots bounds connections pinned across Cloud calls so capacity traffic
	// cannot consume the primary pool. New callers wait here without a DB
	// connection, using at most one quarter of the pool and never more than 4.
	slots chan struct{}
}

type localWorkspaceLock struct {
	gate chan struct{}
	refs int
}

func NewWorkspaceLocker(pool *pgxpool.Pool) WorkspaceLocker {
	if pool == nil {
		return nil
	}
	slotCount := int(pool.Config().MaxConns / workspaceLockPoolFraction)
	if slotCount < 1 {
		slotCount = 1
	}
	if slotCount > maxWorkspaceLockConnections {
		slotCount = maxWorkspaceLockConnections
	}
	return &postgresWorkspaceLocker{
		pool:       pool,
		localLocks: make(map[uuid.UUID]*localWorkspaceLock),
		slots:      make(chan struct{}, slotCount),
	}
}

func (l *postgresWorkspaceLocker) Lock(ctx context.Context, workspaceID uuid.UUID) (db.DBTX, func(), error) {
	releaseLocal, err := l.acquireLocal(ctx, workspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("queue workspace capacity lock: %w", err)
	}
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		releaseLocal()
		return nil, nil, fmt.Errorf("queue workspace capacity lock connection: %w", ctx.Err())
	}
	acquired := false
	defer func() {
		if !acquired {
			<-l.slots
			releaseLocal()
		}
	}()

	key := workspaceAdvisoryLockKey(workspaceID)
	var conn *pgxpool.Conn
	for {
		conn, err = l.pool.Acquire(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("acquire workspace capacity lock connection: %w", err)
		}
		var locked bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&locked); err != nil {
			conn.Release()
			return nil, nil, fmt.Errorf("try workspace capacity lock: %w", err)
		}
		if locked {
			break
		}
		conn.Release()
		conn = nil
		timer := time.NewTimer(workspaceLockPollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, nil, fmt.Errorf("wait for workspace capacity lock: %w", ctx.Err())
		case <-timer.C:
		}
	}
	acquired = true
	var once sync.Once
	return conn, func() {
		once.Do(func() {
			defer releaseLocal()
			defer func() { <-l.slots }()
			timer := time.NewTimer(defaultWorkspaceCooldown)
			<-timer.C
			unlockCtx, cancel := context.WithTimeout(context.Background(), workspaceLockReleaseTimeout)
			defer cancel()
			var unlocked bool
			err := conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock($1)`, key).Scan(&unlocked)
			if err != nil || !unlocked {
				// A session-level lock must never return to the pool if explicit
				// unlock failed. Closing the hijacked connection releases it in
				// PostgreSQL and keeps the next pool borrower isolated.
				_ = conn.Hijack().Close(unlockCtx)
				return
			}
			conn.Release()
		})
	}, nil
}

// acquireLocal makes same-process callers wait in Go instead of each pinning
// one pool connection while another request owns the workspace lock. One
// caller per process polls PostgreSQL so locks remain coordinated across pods.
func (l *postgresWorkspaceLocker) acquireLocal(ctx context.Context, workspaceID uuid.UUID) (func(), error) {
	l.mu.Lock()
	local := l.localLocks[workspaceID]
	if local == nil {
		local = &localWorkspaceLock{gate: make(chan struct{}, 1)}
		local.gate <- struct{}{}
		l.localLocks[workspaceID] = local
	}
	local.refs++
	l.mu.Unlock()

	select {
	case <-ctx.Done():
		l.dropLocalRef(workspaceID, local)
		return nil, ctx.Err()
	case <-local.gate:
		if err := ctx.Err(); err != nil {
			local.gate <- struct{}{}
			l.dropLocalRef(workspaceID, local)
			return nil, err
		}
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			local.gate <- struct{}{}
			l.dropLocalRef(workspaceID, local)
		})
	}, nil
}

func (l *postgresWorkspaceLocker) dropLocalRef(workspaceID uuid.UUID, local *localWorkspaceLock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	local.refs--
	if local.refs == 0 && l.localLocks[workspaceID] == local {
		delete(l.localLocks, workspaceID)
	}
}

func workspaceAdvisoryLockKey(workspaceID uuid.UUID) int64 {
	sum := sha256.Sum256(append([]byte("multica-seat-capacity:"), workspaceID[:]...))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}
