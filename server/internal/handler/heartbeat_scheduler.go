package handler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// HeartbeatScheduler decides how a "this runtime is alive, bump its
// last_seen_at" request actually reaches the database.
//
// Two implementations exist:
//
//   - PassthroughHeartbeatScheduler runs the legacy synchronous TouchAgentRuntimeLastSeen
//     followed by a MarkAgentRuntimeOnline fallback when the touch matches zero rows
//     (sweeper-race recovery). It is the default Handler wiring so unit tests
//     observe the bump immediately and the existing race-recovery test stays valid.
//
//   - BatchedHeartbeatScheduler queues runtime IDs in memory and flushes them as a
//     single bulk UPDATE every tick. Production wires this so a fleet of N runtimes
//     beating every 15s costs ~1 DB transaction per tick instead of N. Sync paths
//     (status flip, never-seen rows) still go through MarkAgentRuntimeOnline
//     immediately; only the hot "online row, just bumping last_seen_at" path is
//     batched. See cmd/server/main.go for the goroutine wiring and shutdown drain.
type HeartbeatScheduler interface {
	// Schedule is called from the heartbeat hot path after the per-row flush
	// window check has decided a DB write is warranted. Implementations must
	// preserve the sweeper-race semantics: if rt.Status was "online" at SELECT
	// time but the row is now offline, the scheduler must eventually flip it
	// back online (sync path immediately; batched path defers to the runtime's
	// next beat, which will see status="offline" and take the sync branch in
	// recordHeartbeat).
	Schedule(ctx context.Context, rt db.AgentRuntime) error
}

// PassthroughHeartbeatScheduler is the synchronous, legacy-behavior scheduler.
// Used as the default in handler.New so tests observe DB writes immediately,
// and as the inline fallback inside BatchedHeartbeatScheduler for cases that
// must commit before returning (offline→online flip, never-seen runtime).
type PassthroughHeartbeatScheduler struct {
	queries *db.Queries
}

func NewPassthroughHeartbeatScheduler(queries *db.Queries) *PassthroughHeartbeatScheduler {
	return &PassthroughHeartbeatScheduler{queries: queries}
}

func (p *PassthroughHeartbeatScheduler) Schedule(ctx context.Context, rt db.AgentRuntime) error {
	if rt.Status == "online" && rt.LastSeenAt.Valid {
		rows, err := p.queries.TouchAgentRuntimeLastSeen(ctx, rt.ID)
		if err != nil {
			return err
		}
		if rows > 0 {
			return nil
		}
		// Sweeper raced us to offline between the SELECT and this UPDATE.
		// Fall through to MarkAgentRuntimeOnline to flip the row back.
	}
	_, err := p.queries.MarkAgentRuntimeOnline(ctx, rt.ID)
	return err
}

// BatchedHeartbeatScheduler coalesces same-id Schedule calls within a tick
// window into a single bulk UPDATE.
//
// Concurrency model:
//   - Schedule grabs a short mutex, inserts into a map (deduped), releases.
//   - A single goroutine (Run) drains the map every tickInterval into a bulk
//     UPDATE.
//   - Stop signals the run loop, which performs one final drain so pending
//     IDs are not lost on graceful shutdown.
//
// Bounded growth: pending is keyed by runtime ID, so its size is bounded by
// the active runtime fleet (one entry per heartbeating runtime per tick).
// Persistent DB errors are logged but do NOT re-queue the failed IDs — the
// next beat from each runtime will reschedule naturally, and re-queuing on
// a hard outage would just balloon the map.
type BatchedHeartbeatScheduler struct {
	queries      *db.Queries
	fallback     *PassthroughHeartbeatScheduler
	tickInterval time.Duration

	mu      sync.Mutex
	pending map[pgtype.UUID]struct{}

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// DefaultHeartbeatBatchInterval is the production tick cadence for the
// BatchedHeartbeatScheduler. Chosen so the load-bearing chain
// `flushInterval + heartbeatInterval + tickInterval < staleThresholdSeconds`
// holds with a comfortable buffer (60 + 15 + 30 = 105 < 150). Lengthening
// this requires bumping staleThresholdSeconds in lockstep.
const DefaultHeartbeatBatchInterval = 30 * time.Second

func NewBatchedHeartbeatScheduler(queries *db.Queries, tickInterval time.Duration) *BatchedHeartbeatScheduler {
	if tickInterval <= 0 {
		tickInterval = DefaultHeartbeatBatchInterval
	}
	return &BatchedHeartbeatScheduler{
		queries:      queries,
		fallback:     NewPassthroughHeartbeatScheduler(queries),
		tickInterval: tickInterval,
		pending:      make(map[pgtype.UUID]struct{}),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

func (b *BatchedHeartbeatScheduler) Schedule(ctx context.Context, rt db.AgentRuntime) error {
	// Status flip (offline→online) and never-seen rows must commit before
	// returning so callers / dependent reads observe the new state. Only
	// the hot "already online, bumping last_seen_at" case is batched.
	if rt.Status != "online" || !rt.LastSeenAt.Valid {
		return b.fallback.Schedule(ctx, rt)
	}
	b.mu.Lock()
	b.pending[rt.ID] = struct{}{}
	b.mu.Unlock()
	return nil
}

// Run drives periodic bulk flushes. Returns after Stop is called and the
// final drain has completed. Intended to be invoked once in its own
// goroutine from main.go.
func (b *BatchedHeartbeatScheduler) Run(ctx context.Context) {
	defer close(b.doneCh)
	t := time.NewTicker(b.tickInterval)
	defer t.Stop()
	for {
		select {
		case <-b.stopCh:
			// Drain whatever is still queued. Use a fresh, short-bounded
			// context so a cancelled parent ctx doesn't drop the final flush.
			drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			b.flushOnce(drainCtx)
			cancel()
			return
		case <-ctx.Done():
			drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			b.flushOnce(drainCtx)
			cancel()
			return
		case <-t.C:
			b.flushOnce(ctx)
		}
	}
}

// Stop signals the Run goroutine to drain and exit. Blocks until the final
// flush completes so callers can sequence shutdown deterministically.
//
// As a defense-in-depth, Stop also performs one more flush after Run has
// exited. This catches the rare case where Run already returned via its
// ctx.Done() branch (e.g. parent ctx was cancelled before Stop was called)
// and a late Schedule call has since added entries to the pending map.
func (b *BatchedHeartbeatScheduler) Stop() {
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
	<-b.doneCh
	finalCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	b.flushOnce(finalCtx)
	cancel()
}

// FlushNow is exposed for tests that want to assert post-flush DB state
// without sleeping for tickInterval. Production code should rely on Run.
func (b *BatchedHeartbeatScheduler) FlushNow(ctx context.Context) {
	b.flushOnce(ctx)
}

// PendingCount reports the number of unique runtime IDs currently queued.
// Exposed for tests and potential metrics.
func (b *BatchedHeartbeatScheduler) PendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

func (b *BatchedHeartbeatScheduler) flushOnce(ctx context.Context) {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	ids := make([]pgtype.UUID, 0, len(b.pending))
	for id := range b.pending {
		ids = append(ids, id)
	}
	b.pending = make(map[pgtype.UUID]struct{})
	b.mu.Unlock()

	rows, err := b.queries.TouchAgentRuntimesLastSeenBatch(ctx, ids)
	if err != nil {
		// Don't requeue on persistent errors — see type comment.
		slog.Warn("heartbeat batch flush failed",
			"scheduled", len(ids), "error", err)
		return
	}
	if int(rows) < len(ids) {
		// Some runtimes raced into a non-online state between Schedule and
		// flush. Their next heartbeat sees status != "online" and falls
		// through to the sync MarkAgentRuntimeOnline path in recordHeartbeat,
		// so the divergence self-heals within one beat (~15s).
		slog.Info("heartbeat batch flush: some runtimes raced to offline",
			"scheduled", len(ids), "affected", rows)
	}
}
