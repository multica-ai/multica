package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// PositionRebalanceService re-spaces the float8 `position` column inside a
// single (workspace_id, status) bucket when the neighbour gap shrinks below
// the configured threshold. Without this, repeated mid-bucket drag-drops
// eventually hit the 2^-52 precision floor of float8 fractional indexing and
// (prev+next)/2 collapses back to an unmovable value.
//
// Design choices:
//
//   - Triggered, not periodic. The UpdateIssue handler calls
//     MaybeEnqueueRebalance synchronously after committing a drag, and the
//     worker runs on a goroutine queue. There is no background cron, so
//     idle workspaces consume zero CPU.
//
//   - Inline dedupe. A pending (workspace_id, status) pair short-circuits if a
//     duplicate is enqueued before the worker drains it — bursts of drags in
//     the same bucket collapse to a single rebalance pass.
//
//   - 1e-9 threshold. The plan picks 1e-9 (vs. the 2^-52 ≈ 2.22e-16 hardware
//     floor) so we rebalance long before precision actually runs out. With a
//     uniform respacing of 1.0 the bucket can absorb ~50 consecutive mid-pair
//     insertions before triggering again.
//
//   - Commit-then-publish. The worker writes the new positions inside a tx and
//     only emits `issue:rebalanced` after Commit returns. If clients refetched
//     before commit they'd see stale rows and re-fire the same delta forever.
type PositionRebalanceService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Bus       *events.Bus

	// GapThreshold is the minimum acceptable gap between neighbour positions.
	// Any drop below this triggers a bucket rebalance. Defaults to 1e-9 if
	// left zero; tests override it.
	GapThreshold float64

	mu      sync.Mutex
	pending map[bucketKey]struct{}
	queue   chan bucketKey
	once    sync.Once
}

type bucketKey struct {
	WorkspaceID string
	Status      string
}

// NewPositionRebalanceService wires the service. Call Start once on boot.
func NewPositionRebalanceService(q *db.Queries, tx TxStarter, bus *events.Bus) *PositionRebalanceService {
	return &PositionRebalanceService{
		Queries:      q,
		TxStarter:    tx,
		Bus:          bus,
		GapThreshold: 1e-9,
		pending:      make(map[bucketKey]struct{}),
		queue:        make(chan bucketKey, 256),
	}
}

// Start launches the background worker goroutine. Safe to call multiple times;
// extras are no-ops.
func (s *PositionRebalanceService) Start(ctx context.Context) {
	s.once.Do(func() {
		go s.run(ctx)
	})
}

// MaybeEnqueueRebalance compares the dragged issue's position against its two
// immediate neighbours and enqueues a rebalance if either gap fell below
// GapThreshold. Called from the UpdateIssue handler after the position write
// has committed. Never blocks; if the queue is saturated we drop the request
// — the next drag in the bucket will retry, and saturated queues only happen
// during contended drag bursts where the next rebalance is imminent anyway.
//
// The hot path queries ONLY the prev/next neighbours of `position` (two
// indexed-seek subqueries), never the whole bucket. The full-bucket scan
// stays inside the worker, where it runs off the user's drag latency budget
// and only when a rebalance is actually scheduled. Before MUL-2314 reviewer
// note #3 this method scanned the entire bucket on every drag — O(N) per
// drag in a bucket of N issues, killed responsiveness on large columns.
//
// We compute neighbours from the persisted bucket rather than trusting the
// client-supplied prev/next ids: the active drag's `position` is now in the
// table and any earlier in-flight update has either committed (and is visible)
// or holds the row lock (and will serialize after us).
func (s *PositionRebalanceService) MaybeEnqueueRebalance(
	ctx context.Context,
	workspaceID pgtype.UUID,
	status string,
	position float64,
) {
	if s == nil || s.Bus == nil || s.Queries == nil {
		return
	}
	gap, err := s.Queries.GetIssueNeighborGap(ctx, db.GetIssueNeighborGapParams{
		WorkspaceID:    workspaceID,
		Status:         status,
		TargetPosition: position,
	})
	if err != nil {
		slog.Warn("rebalance gap check: neighbor query failed", "error", err)
		return
	}
	threshold := s.GapThreshold
	if threshold == 0 {
		threshold = 1e-9
	}
	// Edge-of-bucket case is handled inside SQL: missing prev/next gets a
	// sentinel of (position ± 1.0), so the gap is always ≥ 1.0 (well above
	// any plausible threshold) and never triggers a rebalance — correct,
	// because there is no precision pressure on a side without a neighbour.
	prevGap := position - gap.PrevPosition
	nextGap := gap.NextPosition - position
	minGap := prevGap
	if nextGap < minGap {
		minGap = nextGap
	}
	if minGap >= threshold {
		return
	}

	key := bucketKey{WorkspaceID: util.UUIDToString(workspaceID), Status: status}
	s.mu.Lock()
	if _, dup := s.pending[key]; dup {
		s.mu.Unlock()
		return
	}
	s.pending[key] = struct{}{}
	s.mu.Unlock()

	select {
	case s.queue <- key:
	default:
		// Queue is full — drop and clear the pending marker so a later drag
		// gets a chance to re-enqueue.
		s.mu.Lock()
		delete(s.pending, key)
		s.mu.Unlock()
		slog.Warn("rebalance queue full, dropping enqueue",
			"workspace_id", key.WorkspaceID, "status", key.Status)
	}
}

func (s *PositionRebalanceService) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case key := <-s.queue:
			s.runOnce(ctx, key)
		}
	}
}

func (s *PositionRebalanceService) runOnce(ctx context.Context, key bucketKey) {
	defer func() {
		s.mu.Lock()
		delete(s.pending, key)
		s.mu.Unlock()
	}()

	// Bound the per-bucket work in case a runaway bucket gets enqueued under
	// load — five minutes is more than enough to rewrite ~10⁵ rows.
	workCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	wsUUID, err := util.ParseUUID(key.WorkspaceID)
	if err != nil {
		slog.Warn("rebalance: bad workspace_id", "error", err, "workspace_id", key.WorkspaceID)
		return
	}

	tx, err := s.TxStarter.Begin(workCtx)
	if err != nil {
		slog.Warn("rebalance: begin tx failed", "error", err)
		return
	}
	defer tx.Rollback(workCtx)
	qtx := s.Queries.WithTx(tx)

	rows, err := qtx.ListIssuePositionsByBucket(workCtx, db.ListIssuePositionsByBucketParams{
		WorkspaceID: wsUUID,
		Status:      key.Status,
	})
	if err != nil {
		slog.Warn("rebalance: list bucket positions failed", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	// Re-space at integer intervals starting at 1.0. Existing ordering is
	// preserved (rows are already ASC), so the user-visible order does not
	// change — only the underlying floats are spread back out.
	items := make([]map[string]any, 0, len(rows))
	for i, row := range rows {
		newPos := float64(i + 1)
		if err := qtx.UpdateIssuePositionOnly(workCtx, db.UpdateIssuePositionOnlyParams{
			ID:       row.ID,
			Position: newPos,
		}); err != nil {
			slog.Warn("rebalance: update position failed", "error", err, "id", util.UUIDToString(row.ID))
			return
		}
		items = append(items, map[string]any{
			"id":       util.UUIDToString(row.ID),
			"position": newPos,
		})
	}

	if err := tx.Commit(workCtx); err != nil {
		slog.Warn("rebalance: commit failed", "error", err)
		return
	}

	// Publish only after commit — clients that refetch on this event must see
	// the rewritten positions, otherwise they'd race the worker.
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueRebalanced,
		WorkspaceID: key.WorkspaceID,
		Payload: map[string]any{
			"workspace_id": key.WorkspaceID,
			"status":       key.Status,
			"items":        items,
		},
	})
	slog.Info("issue position bucket rebalanced",
		"workspace_id", key.WorkspaceID, "status", key.Status, "count", len(rows))
}
