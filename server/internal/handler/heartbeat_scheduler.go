package handler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// HeartbeatScheduler decides how a "this runtime is alive, bump its
// last_seen_at" request actually reaches the database.
//
// Two implementations exist:
//
//   - PassthroughHeartbeatScheduler runs the synchronous TouchAgentRuntimeLastSeen
//     followed by a MarkAgentRuntimeOnline fallback when the touch matches zero rows
//     (sweeper-race recovery). It is the default Handler wiring so unit tests
//     observe the bump immediately.
//
//   - BatchedHeartbeatScheduler queues runtime IDs in memory and flushes them as a
//     single bulk UPDATE every tick. Production wires this so a fleet of N runtimes
//     beating every 15s costs ~1 DB transaction per tick instead of N. IDs
//     omitted by UPDATE ... RETURNING are reconciled after each flush: rows
//     raced offline are restored, while deleted rows invalidate their connection.
//     See cmd/server/main.go for the goroutine wiring and shutdown drain.
type HeartbeatScheduler interface {
	// Schedule is called from the heartbeat hot path after the per-row flush
	// window check has decided a DB write is warranted. Runtime ownership and
	// status live in the connection lease, so only the ID enters this layer.
	Schedule(ctx context.Context, runtimeID pgtype.UUID) error
}

// PassthroughHeartbeatScheduler is the synchronous scheduler used as the
// default in handler.New so tests observe DB writes immediately.
type PassthroughHeartbeatScheduler struct {
	queries *db.Queries
}

func NewPassthroughHeartbeatScheduler(queries *db.Queries) *PassthroughHeartbeatScheduler {
	return &PassthroughHeartbeatScheduler{queries: queries}
}

func (p *PassthroughHeartbeatScheduler) Schedule(ctx context.Context, runtimeID pgtype.UUID) error {
	rows, err := p.queries.TouchAgentRuntimeLastSeen(ctx, runtimeID)
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil
	}
	// The row either raced offline or was deleted. MarkAgentRuntimeOnline
	// restores the former and preserves pgx.ErrNoRows for the latter.
	_, err = p.queries.MarkAgentRuntimeOnline(ctx, runtimeID)
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
// Failed flushes are re-queued because the connection lease has already
// advanced its local flush watermark; the map remains fleet-bounded during a
// persistent outage and retries once per tick.
type BatchedHeartbeatScheduler struct {
	queries      *db.Queries
	runtimeGone  RuntimeGoneNotifier
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
// holds with a comfortable buffer (60 + 15 + 30 = 105 < 150). One failed
// flush retry adds another tick (135 < 150); additional failures keep the ID
// fleet-bounded in pending until DB writes recover. Lengthening either interval
// requires bumping RuntimeClaimFreshnessSeconds in lockstep.
const DefaultHeartbeatBatchInterval = 30 * time.Second

// Only a row old enough for the sweeper to demote can be restored from a batch
// receipt. A freshly-offline row was explicitly deregistered and must remain
// offline even if an older heartbeat was already pending.
const heartbeatReceiptRecoveryThreshold = time.Duration(service.RuntimeClaimFreshnessSeconds) * time.Second

func NewBatchedHeartbeatScheduler(queries *db.Queries, tickInterval time.Duration, runtimeGone RuntimeGoneNotifier) *BatchedHeartbeatScheduler {
	if tickInterval <= 0 {
		tickInterval = DefaultHeartbeatBatchInterval
	}
	return &BatchedHeartbeatScheduler{
		queries:      queries,
		runtimeGone:  runtimeGone,
		tickInterval: tickInterval,
		pending:      make(map[pgtype.UUID]struct{}),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

func (b *BatchedHeartbeatScheduler) Schedule(_ context.Context, runtimeID pgtype.UUID) error {
	b.mu.Lock()
	b.pending[runtimeID] = struct{}{}
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

	touched, err := b.queries.TouchAgentRuntimesLastSeenBatch(ctx, ids)
	if err != nil {
		// The connection lease advances its flush watermark when Schedule
		// accepts the ID, so retain failed IDs here instead of waiting another
		// full lease interval before retrying.
		b.requeue(ids)
		slog.Warn("heartbeat batch flush failed",
			"scheduled", len(ids), "error", err)
		return
	}
	if len(touched) == len(ids) {
		return
	}

	touchedSet := make(map[pgtype.UUID]struct{}, len(touched))
	for _, id := range touched {
		touchedSet[id] = struct{}{}
	}
	omitted := make([]pgtype.UUID, 0, len(ids)-len(touched))
	for _, id := range ids {
		if _, ok := touchedSet[id]; !ok {
			omitted = append(omitted, id)
		}
	}

	states, err := b.queries.GetAgentRuntimeHeartbeatLeases(ctx, omitted)
	if err != nil {
		b.requeue(omitted)
		slog.Warn("heartbeat batch reconciliation query failed",
			"omitted", len(omitted), "error", err)
		return
	}
	existing := make(map[pgtype.UUID]struct{}, len(states))
	recovered := 0
	preservedOffline := 0
	missing := 0
	now := time.Now()
	for _, state := range states {
		existing[state.ID] = struct{}{}
		if state.Status != "offline" || !state.LastSeenAt.Valid || now.Sub(state.LastSeenAt.Time) < heartbeatReceiptRecoveryThreshold {
			// The omission was not a stale-sweeper race. In particular, preserve
			// recent explicit deregistration and its offline_reason metadata.
			preservedOffline++
			continue
		}
		if _, err := b.queries.MarkAgentRuntimeOnline(ctx, state.ID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				missing++
				b.notifyRuntimeGone(state.ID)
				continue
			}
			b.requeue([]pgtype.UUID{state.ID})
			slog.Warn("heartbeat batch offline recovery failed",
				"runtime_id", uuidToString(state.ID), "error", err)
			continue
		}
		recovered++
	}

	for _, id := range omitted {
		if _, ok := existing[id]; ok {
			continue
		}
		missing++
		b.notifyRuntimeGone(id)
	}
	slog.Info("heartbeat batch flush reconciled omitted runtimes",
		"scheduled", len(ids),
		"touched", len(touched),
		"recovered", recovered,
		"preserved_offline", preservedOffline,
		"missing", missing,
	)
}

func (b *BatchedHeartbeatScheduler) requeue(ids []pgtype.UUID) {
	// A runtime can disconnect while its ID waits here, so a recovered DB may
	// receive one final delayed last_seen_at refresh. That delay is bounded by
	// one retry tick after recovery; keeping the ID is required because the
	// connection lease already advanced its local flush watermark.
	b.mu.Lock()
	for _, id := range ids {
		b.pending[id] = struct{}{}
	}
	b.mu.Unlock()
}

func (b *BatchedHeartbeatScheduler) notifyRuntimeGone(id pgtype.UUID) {
	if b.runtimeGone != nil {
		b.runtimeGone.NotifyRuntimeGone(uuidToString(id))
	}
}
