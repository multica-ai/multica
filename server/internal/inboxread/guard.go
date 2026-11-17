// Package inboxread bounds expensive inbox reads before they acquire a database
// connection. Budgets are per process; replicas must be sized together.
package inboxread

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/metrics"
)

var ErrBusy = errors.New("inbox read capacity unavailable")

const (
	queueLimit     = 20
	queueWait      = 200 * time.Millisecond
	setupTimeout   = 200 * time.Millisecond
	cleanupTimeout = 250 * time.Millisecond
)

type Config struct {
	Concurrency      int
	StatementTimeout time.Duration
	RequestTimeout   time.Duration
}

func ConfigFromEnv() Config {
	cfg := Config{
		Concurrency:      envInt("INBOX_READ_CONCURRENCY", 2, 1, 20),
		StatementTimeout: time.Duration(envInt("INBOX_READ_STATEMENT_TIMEOUT_MS", 3000, 1, 60000)) * time.Millisecond,
		RequestTimeout:   time.Duration(envInt("INBOX_READ_TIMEOUT_MS", 5000, 1000, 120000)) * time.Millisecond,
	}
	if cfg.RequestTimeout < cfg.StatementTimeout+time.Second {
		slog.Warn("inbox read deadline needs headroom above statement timeout; increasing deadline")
		cfg.RequestTimeout = cfg.StatementTimeout + 2*time.Second
	}
	return cfg
}

func envInt(name string, fallback, min, max int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		slog.Warn("invalid inbox read setting; using default", "name", name, "default", fallback)
		return fallback
	}
	return n
}

type Guard struct {
	active           chan struct{}
	queued           chan struct{}
	summaryWaiters   chan struct{}
	summaryMu        sync.Mutex
	summaryCalls     map[string]*summaryQueue
	statementTimeout time.Duration
	requestTimeout   time.Duration
	Metrics          *metrics.InboxReadMetrics
}

type summaryQueue struct {
	current *summaryGeneration
	next    *summaryGeneration
}

type summaryGeneration struct {
	ctx     context.Context
	query   func(context.Context) (any, error)
	started bool // protected by summaryMu
	done    chan struct{}
	value   any
	err     error
}

func New(cfg Config) *Guard {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.StatementTimeout <= 0 {
		cfg.StatementTimeout = 3 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 5 * time.Second
	}
	return &Guard{
		active:           make(chan struct{}, cfg.Concurrency),
		queued:           make(chan struct{}, queueLimit),
		summaryWaiters:   make(chan struct{}, cfg.Concurrency+queueLimit),
		summaryCalls:     make(map[string]*summaryQueue),
		statementTimeout: cfg.StatementTimeout,
		requestTimeout:   cfg.RequestTimeout,
	}
}

func (g *Guard) acquire(ctx context.Context, operation string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case g.active <- struct{}{}:
		return g.release, nil
	default:
	}
	select {
	case g.queued <- struct{}{}:
	default:
		g.Metrics.Reject(operation, "queue_full")
		return nil, ErrBusy
	}
	g.Metrics.Queue(1)
	defer func() { <-g.queued; g.Metrics.Queue(-1) }()
	timer := time.NewTimer(queueWait)
	defer timer.Stop()
	select {
	case g.active <- struct{}{}:
		return g.release, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		g.Metrics.Reject(operation, "queue_timeout")
		return nil, ErrBusy
	}
}

func (g *Guard) release() { <-g.active }

// Context starts the complete handler budget, including permission lookups.
func (g *Guard) Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, g.requestTimeout)
}

type TxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

// Run keeps the permit through SQL completion and rollback. Once SQL starts,
// a disconnected HTTP caller must not release capacity while Postgres still
// runs its statement. The database timeout fires before the detached Go budget.
// Queueing and pool acquisition remain cancellable and acquire no unbounded work.
func (g *Guard) Run(ctx context.Context, operation string, starter TxStarter, query func(context.Context, pgx.Tx) error) (err error) {
	ctx, cancel := context.WithTimeout(ctx, g.requestTimeout)
	defer cancel()
	release, err := g.acquire(ctx, operation)
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	start := time.Now()
	g.Metrics.Active(1)
	defer func() { g.Metrics.Active(-1); g.Metrics.Observe(operation, time.Since(start), err) }()

	setupCtx, setupCancel := context.WithTimeout(ctx, setupTimeout)
	defer setupCancel()
	tx, err := starter.Begin(setupCtx)
	if err != nil {
		return fmt.Errorf("begin inbox read: %w", err)
	}
	var uncertainUntil time.Time
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cleanupCancel()
		_ = tx.Rollback(cleanupCtx)
		// A transport failure can hide a still-running backend. Keep its
		// permit until the server-side timeout could have stopped the SQL,
		// even when rollback cannot communicate with that connection.
		if wait := time.Until(uncertainUntil); wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			<-timer.C
		}
	}()
	if _, err = tx.Exec(setupCtx, fmt.Sprintf("SET LOCAL statement_timeout = %d", g.statementTimeout.Milliseconds())); err != nil {
		return err
	}
	if _, err = tx.Exec(setupCtx, "SET LOCAL transaction_read_only = on"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Include cleanup in the remaining request budget before detaching SQL.
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < g.statementTimeout+500*time.Millisecond+cleanupTimeout {
		return context.DeadlineExceeded
	}
	// Include server timeout and network headroom, independently of the caller.
	// This is one sqlc query, fully scanned before commit, not a transaction
	// containing an arbitrary number of separately timed statements.
	queryCtx, queryCancel := context.WithTimeout(context.WithoutCancel(ctx), g.statementTimeout+500*time.Millisecond)
	defer queryCancel()
	queryDeadline := time.Now().Add(g.statementTimeout + 500*time.Millisecond)
	queryStart := time.Now()
	err = query(queryCtx, tx)
	g.Metrics.ObserveQuery(operation, time.Since(queryStart))
	if err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) && !errors.Is(err, pgx.ErrNoRows) {
			uncertainUntil = queryDeadline
		}
		return err
	}
	return tx.Commit(queryCtx)
}

// Summary coalesces requests into generations per authenticated account. A
// caller arriving after execution starts waits for the next generation, whose
// independent budget begins after the current generation finishes. Successful
// earlier callers never wait for a later refresh. Canceled callers retain their
// admission slot until their generation completes, bounding retained work.
func (g *Guard) Summary(ctx context.Context, userID string, query func(context.Context) (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case g.summaryWaiters <- struct{}{}:
	default:
		g.Metrics.Reject("summary", "waiters_full")
		return nil, ErrBusy
	}
	g.summaryMu.Lock()
	queue := g.summaryCalls[userID]
	var generation *summaryGeneration
	if queue == nil {
		generation = newSummaryGeneration(ctx, query)
		queue = &summaryQueue{current: generation}
		g.summaryCalls[userID] = queue
		go g.runSummary(userID, queue)
	} else if !queue.current.started {
		generation = queue.current
	} else {
		if queue.next == nil {
			queue.next = newSummaryGeneration(ctx, query)
		}
		generation = queue.next
	}
	g.summaryMu.Unlock()
	select {
	case <-generation.done:
		<-g.summaryWaiters
		return generation.value, generation.err
	case <-ctx.Done():
		go func() { <-generation.done; <-g.summaryWaiters }()
		return nil, ctx.Err()
	}
}

func newSummaryGeneration(ctx context.Context, query func(context.Context) (any, error)) *summaryGeneration {
	return &summaryGeneration{ctx: context.WithoutCancel(ctx), query: query, done: make(chan struct{})}
}

func (g *Guard) runSummary(userID string, queue *summaryQueue) {
	for {
		g.summaryMu.Lock()
		generation := queue.current
		generation.started = true
		g.summaryMu.Unlock()

		sharedCtx, cancel := context.WithTimeout(generation.ctx, g.requestTimeout)
		value, err := generation.query(sharedCtx)
		if err == nil {
			err = sharedCtx.Err()
		}
		cancel()

		g.summaryMu.Lock()
		generation.value, generation.err = value, err
		close(generation.done)
		if queue.next == nil {
			delete(g.summaryCalls, userID)
			g.summaryMu.Unlock()
			return
		}
		queue.current, queue.next = queue.next, nil
		g.summaryMu.Unlock()
	}
}

func IsUnavailable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.Is(err, ErrBusy) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &pgErr) && pgErr.Code == "57014")
}
