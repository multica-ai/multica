package auth

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PATLastUsedRecorder records that a personal access token was used, so its
// last_used_at column can be refreshed off the request path. Record MUST be
// cheap, constant-time and never block: it runs inside the auth middleware on
// every PAT cache miss.
//
// Before this type, each cache miss did `go queries.UpdatePersonalAccessToken
// LastUsed(context.Background(), id)` at three call sites (Auth, DaemonAuth,
// patResolver). That forks an unbounded number of goroutines during a
// cache-flush stampede (Redis outage, mass TTL expiry, WS reconnect storm),
// each acquiring a pgxpool connection to write a droppable display timestamp —
// with no timeout and no panic recovery (fatal, since a goroutine's panic is
// never caught by chi's Recoverer). This recorder collapses all three onto a
// single bounded, deduplicating background writer.
type PATLastUsedRecorder interface {
	// Record marks tokenID for a best-effort last_used_at refresh. It is
	// non-blocking and may drop the mark under load — last_used_at is
	// approximate by design and is refreshed again on the token's next
	// cache miss. It deliberately takes no context: recording is a local,
	// in-memory operation and must not be tied to the request lifecycle.
	Record(tokenID pgtype.UUID)
}

// lastUsedBatchWriter is the subset of *db.Queries the recorder needs. Defined
// here (the middleware/resolver hold a concrete *db.Queries, there is no
// generated Querier interface) so tests can inject a fake without a database.
type lastUsedBatchWriter interface {
	UpdatePersonalAccessTokensLastUsed(ctx context.Context, ids []pgtype.UUID) error
}

// Tunables. Kept as code constants — not env vars — until metrics justify
// widening the config surface. See the package comment for the failure modes
// each one bounds.
const (
	// defaultFlushInterval is how often pending marks are flushed to PG.
	// Combined with the shared PAT cache (~10min TTL) it means a hot token
	// writes last_used_at at most a few times per window across a fleet.
	defaultFlushInterval = 30 * time.Second
	// defaultMaxPending caps in-memory pending IDs. A stampede beyond this
	// drops marks (counted) rather than growing unboundedly.
	defaultMaxPending = 4096
	// defaultMaxBatch caps IDs per UPDATE so a single flush cannot build an
	// unbounded statement / transaction.
	defaultMaxBatch = 500
	// defaultFlushBudget bounds an ENTIRE periodic flush (all chunks), not
	// each chunk — so a slow DB cannot stall the worker for chunks*timeout.
	defaultFlushBudget = 5 * time.Second
	// defaultShutdownBudget bounds the WHOLE of Stop (join + final flush). It
	// is independent of the worker ctx, which main() cancels immediately before
	// calling Stop, so the final flush still runs.
	defaultShutdownBudget = 10 * time.Second
)

// BatchedPATLastUsedRecorder buffers token ids in memory and flushes them to
// Postgres in deduplicated batches on a fixed interval. Safe for concurrent
// Record; a single background goroutine (Run) owns all DB I/O.
type BatchedPATLastUsedRecorder struct {
	queries       lastUsedBatchWriter
	flushInterval time.Duration
	maxPending    int
	maxBatch      int
	flushBudget   time.Duration
	shutdownDur   time.Duration

	mu      sync.Mutex
	pending map[pgtype.UUID]struct{}

	started  atomic.Bool // true once Run has begun
	stopOnce sync.Once
	stopped  chan struct{} // closed when Run has exited
	done     chan struct{} // closed by Stop to request shutdown

	metrics patLastUsedMetrics
}

// patLastUsedMetrics is the observability sink. A nil *PATLastUsedMetrics is
// tolerated (all increments no-op), so tests and metrics-off deployments work.
type patLastUsedMetrics interface {
	recorded()
	deduplicated()
	dropped()
	setPending(n int)
	flushBatch(n int)
	flushError()
	flushSkippedBatch()
	panicRecovered()
}

// NewBatchedPATLastUsedRecorder builds a recorder over queries. Returns a
// no-op recorder when queries is nil, matching the existing `if queries == nil`
// guards in the auth middleware (last-used tracking simply disabled).
func NewBatchedPATLastUsedRecorder(queries *db.Queries, metrics *PATLastUsedMetrics) PATLastUsedRecorder {
	if queries == nil {
		return NoopPATLastUsedRecorder{}
	}
	return &BatchedPATLastUsedRecorder{
		queries:       queries,
		flushInterval: defaultFlushInterval,
		maxPending:    defaultMaxPending,
		maxBatch:      defaultMaxBatch,
		flushBudget:   defaultFlushBudget,
		shutdownDur:   defaultShutdownBudget,
		pending:       make(map[pgtype.UUID]struct{}),
		stopped:       make(chan struct{}),
		done:          make(chan struct{}),
		metrics:       metricsOrNoop(metrics),
	}
}

// Record buffers tokenID. Order matters: an already-pending id counts as a
// dedup (even when the map is full), only a genuinely new id can be dropped.
func (b *BatchedPATLastUsedRecorder) Record(tokenID pgtype.UUID) {
	b.mu.Lock()
	if _, ok := b.pending[tokenID]; ok {
		b.mu.Unlock()
		b.metrics.deduplicated()
		return
	}
	if len(b.pending) >= b.maxPending {
		b.mu.Unlock()
		b.metrics.dropped()
		return
	}
	b.pending[tokenID] = struct{}{}
	// Gauge is set under the lock so it can never lose a race with flushOnce's
	// reset-to-0 and get stuck reporting a stale value while ids are pending.
	b.metrics.setPending(len(b.pending))
	b.mu.Unlock()
	b.metrics.recorded()
}

// Run drives periodic flushes until ctx is cancelled or Stop is called. Call
// once, in a goroutine. ctx (main.go's sweepCtx) governs SCHEDULING only —
// it decides when to stop ticking. It is deliberately NOT threaded into the DB
// write: once a periodic flush has swapped the pending set out, its write must
// run to completion under its own budget, otherwise a sweepCancel() landing
// mid-flush would abort the query and silently drop that already-detached
// batch (Stop's final flush could not recover it — pending is already empty).
func (b *BatchedPATLastUsedRecorder) Run(ctx context.Context) {
	b.started.Store(true)
	defer close(b.stopped)
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.done:
			return
		case <-ticker.C:
			b.flushOnce(b.flushBudget)
		}
	}
}

// Stop requests shutdown and, under a single total deadline, waits for Run to
// exit and then flushes whatever remains. Idempotent and safe to call even if
// Run was never started (no wait in that case). The whole method — join plus
// final flush — is bounded by shutdownDur, so it can never hold shutdown open
// longer than that.
func (b *BatchedPATLastUsedRecorder) Stop() {
	b.stopOnce.Do(func() {
		close(b.done)
		deadline := time.Now().Add(b.shutdownDur)
		// Only wait for Run to exit if it was actually started; otherwise
		// there is nothing to join and blocking would just burn the budget
		// (and log a spurious "worker did not exit" warning).
		if b.started.Load() {
			select {
			case <-b.stopped:
			case <-time.After(time.Until(deadline)):
				slog.Warn("pat last_used: worker did not exit before shutdown deadline; flushing anyway")
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		b.flushOnce(remaining)
	})
}

// flushOnce atomically swaps out the pending set, then writes it in bounded
// chunks under a single whole-flush deadline rooted in context.Background.
// Rooting in Background (not a caller ctx) is intentional: see Run. Marks
// recorded during the flush land in the next round — never lost, never blocked.
func (b *BatchedPATLastUsedRecorder) flushOnce(budget time.Duration) {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.pending
	b.pending = make(map[pgtype.UUID]struct{})
	b.metrics.setPending(0)
	b.mu.Unlock()

	ids := make([]pgtype.UUID, 0, len(batch))
	for id := range batch {
		ids = append(ids, id)
	}

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	for start := 0; start < len(ids); start += b.maxBatch {
		end := start + b.maxBatch
		if end > len(ids) {
			end = len(ids)
		}
		// Budget for the whole flush is exhausted: skip (drop) the rest.
		// Dropped marks self-heal on the token's next cache miss.
		if ctx.Err() != nil {
			b.metrics.flushSkippedBatch()
			continue
		}
		if !b.flushChunk(ctx, ids[start:end]) {
			// First DB error/panic ends this round; remaining chunks are
			// dropped rather than hammering a struggling database.
			for rest := end; rest < len(ids); rest += b.maxBatch {
				b.metrics.flushSkippedBatch()
			}
			return
		}
	}
}

// flushChunk writes one chunk. Returns false on error or recovered panic so
// the caller can stop the round. Panic is contained here (per-chunk boundary)
// so a driver-level failure can never take down the process and the worker
// keeps running for the next tick.
func (b *BatchedPATLastUsedRecorder) flushChunk(ctx context.Context, ids []pgtype.UUID) (ok bool) {
	defer func() {
		if rec := recover(); rec != nil {
			b.metrics.panicRecovered()
			slog.Error("pat last_used: recovered from panic during flush", "panic", rec)
			ok = false
		}
	}()
	if err := b.queries.UpdatePersonalAccessTokensLastUsed(ctx, ids); err != nil {
		b.metrics.flushError()
		// No token ids in the log line — they are credential-adjacent.
		slog.Warn("pat last_used: batch flush failed; dropping", "batch", len(ids), "error", err)
		return false
	}
	b.metrics.flushBatch(len(ids))
	return true
}

// NoopPATLastUsedRecorder discards every Record. Used when queries is nil and
// as the default when no recorder is injected into the router.
type NoopPATLastUsedRecorder struct{}

func (NoopPATLastUsedRecorder) Record(pgtype.UUID) {}

// atomicNoopMetrics is used when no PATLastUsedMetrics is supplied.
type atomicNoopMetrics struct{}

func (atomicNoopMetrics) recorded()          {}
func (atomicNoopMetrics) deduplicated()      {}
func (atomicNoopMetrics) dropped()           {}
func (atomicNoopMetrics) setPending(int)     {}
func (atomicNoopMetrics) flushBatch(int)     {}
func (atomicNoopMetrics) flushError()        {}
func (atomicNoopMetrics) flushSkippedBatch() {}
func (atomicNoopMetrics) panicRecovered()    {}

func metricsOrNoop(m *PATLastUsedMetrics) patLastUsedMetrics {
	if m == nil {
		return atomicNoopMetrics{}
	}
	return m
}
