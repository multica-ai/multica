package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The durable matcher (MUL-4332 PR3). It consumes pending domain_event rows via a
// lease, and for each enabled hook whose active revision listens to that event it
// runs the SAME automation.Evaluate as dry-run/explain, then completes the fire
// decision the read-only evaluator deliberately left open: the depth guard, the
// rising-edge latch, and per_event/rising_edge fire/skip. Each decision is
// persisted as one hook_execution row carrying the evaluator's structured
// snapshots and the pinned revision, idempotent per (hook, event).
//
// This slice does NOT run actions — a fired hook lands `queued` for the executor
// (a later slice). With no executor and the automation_event_hooks flag off
// (the matcher loop is gated on it), production behaviour is unchanged.

const (
	hookExecQueued  = "queued"
	hookExecSkipped = "skipped"

	skipHopExceeded    = "hop_exceeded"
	skipConditionFalse = "condition_false"
	skipEdgeNotRising  = "edge_not_rising"

	// latchStateKind keys rising-edge latches in automation_state; one row per hook.
	latchStateKind = "hook_edge"

	// maxHopCount is the loop-depth guard (§15.3): an event this deep in a
	// causal chain never fires a hook.
	maxHopCount = 8

	// MatcherBatchSize / MatcherLeaseTTL bound one matcher tick.
	MatcherBatchSize = 100
	MatcherLeaseTTL  = 2 * time.Minute
)

// latchState is the persisted rising-edge latch. RevisionID pins it to a
// revision so a config change starts a fresh edge rather than inheriting a stale
// satisfied flag.
type latchState struct {
	Satisfied  bool   `json:"satisfied"`
	RevisionID string `json:"revision_id"`
}

// ClaimAndMatch leases up to batchSize pending events and matches each, marking
// only the successfully-matched ones dispatched. It returns the number
// dispatched. A per-event failure leaves that event un-dispatched so a later tick
// (after its lease expires) retries it; re-processing is idempotent.
func (s *HookService) ClaimAndMatch(ctx context.Context, batchSize int32) (int, error) {
	lease := util.NewUUID()
	events, err := s.Queries.ClaimPendingDomainEvents(ctx, db.ClaimPendingDomainEventsParams{
		LeaseToken:     lease,
		LeaseExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(MatcherLeaseTTL), Valid: true},
		MaxEvents:      batchSize,
	})
	if err != nil {
		return 0, err
	}
	dispatched := 0
	for _, e := range events {
		if err := s.MatchEvent(ctx, e); err != nil {
			slog.Warn("hook matcher: event failed, leaving for retry", "event_id", util.UUIDToString(e.ID), "error", err)
			continue
		}
		if _, err := s.Queries.MarkDomainEventDispatched(ctx, db.MarkDomainEventDispatchedParams{ID: e.ID, LeaseToken: lease}); err != nil {
			slog.Warn("hook matcher: mark dispatched failed", "event_id", util.UUIDToString(e.ID), "error", err)
			continue
		}
		dispatched++
	}
	return dispatched, nil
}

// MatchEvent evaluates every candidate hook for one event. Each candidate is
// processed in its own transaction so a poison hook cannot block the rest.
func (s *HookService) MatchEvent(ctx context.Context, event db.DomainEvent) error {
	hookIDs, err := s.Queries.ListActiveHookIDsForEvent(ctx, db.ListActiveHookIDsForEventParams{
		WorkspaceID: event.WorkspaceID,
		EventType:   event.Type,
	})
	if err != nil {
		return err
	}
	for _, hookID := range hookIDs {
		if err := s.processHookForEvent(ctx, event, hookID); err != nil {
			return err
		}
	}
	return nil
}

// processHookForEvent makes and persists the fire/skip decision for one (hook,
// event) pair in a single transaction. It locks the hook row first, which both
// pins the active revision against a concurrent edit and serializes the latch
// read-modify-write against other matchers.
func (s *HookService) processHookForEvent(ctx context.Context, event db.DomainEvent, hookID pgtype.UUID) error {
	return s.inTx(ctx, func(qtx *db.Queries) error {
		hook, err := qtx.GetHookForUpdate(ctx, db.GetHookForUpdateParams{ID: hookID, WorkspaceID: event.WorkspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // hook vanished between the scan and now
			}
			return err
		}
		if !hook.Enabled || hook.ArchivedAt.Valid {
			return nil // disabled / archived since the scan
		}
		rawRev, err := qtx.GetHookRevision(ctx, hook.ActiveRevisionID)
		if err != nil {
			return err
		}
		if rawRev.EventType != event.Type {
			return nil // active revision was re-pointed to a different event type
		}

		rev, err := revisionToEval(rawRev)
		if err != nil {
			return err
		}
		view, err := eventToView(event)
		if err != nil {
			return err
		}
		ev, err := automation.Evaluate(ctx, view, rev, &issueStateReader{q: qtx, workspaceID: event.WorkspaceID})
		if err != nil {
			return err
		}

		// A non-matching hook is not a candidate — write nothing.
		if !ev.Matched {
			return nil
		}

		matchSnap, err := ev.MatchSnapshot()
		if err != nil {
			return err
		}
		condSnap, err := ev.ConditionSnapshot()
		if err != nil {
			return err
		}
		write := func(status, skipReason string) (bool, error) {
			return writeHookExecution(ctx, qtx, event, hook, status, skipReason, matchSnap, condSnap)
		}

		// Depth guard: an over-deep event never fires anything.
		if event.HopCount >= maxHopCount {
			_, err := write(hookExecSkipped, skipHopExceeded)
			return err
		}

		if rawRev.FireMode != automation.FireRisingEdge {
			// per_event: fire whenever the conditions currently hold.
			if ev.ConditionsMet {
				_, err := write(hookExecQueued, "")
				return err
			}
			_, err := write(hookExecSkipped, skipConditionFalse)
			return err
		}

		// rising_edge: fire only on a false→true transition of the latch.
		prev, err := readLatch(ctx, qtx, event.WorkspaceID, hookID, hook.ActiveRevisionID)
		if err != nil {
			return err
		}
		nowSatisfied := ev.ConditionsMet
		fire := nowSatisfied && !prev

		status, skipReason := hookExecQueued, ""
		if !fire {
			status = hookExecSkipped
			if nowSatisfied {
				skipReason = skipEdgeNotRising
			} else {
				skipReason = skipConditionFalse
			}
		}
		inserted, err := write(status, skipReason)
		if err != nil {
			return err
		}
		// Advance the latch exactly once per (hook, event): only when THIS call
		// created the execution row, so a re-leased retry never double-advances.
		if inserted {
			return upsertLatch(ctx, qtx, event.WorkspaceID, hookID, hook.ActiveRevisionID, nowSatisfied)
		}
		return nil
	})
}

// writeHookExecution inserts one decision row idempotently. It reports whether a
// new row was created (false means it already existed — a re-processed event).
func writeHookExecution(ctx context.Context, qtx *db.Queries, event db.DomainEvent, hook db.Hook, status, skipReason string, matchSnap, condSnap []byte) (bool, error) {
	reason := pgtype.Text{}
	if skipReason != "" {
		reason = pgtype.Text{String: skipReason, Valid: true}
	}
	_, err := qtx.CreateHookExecution(ctx, db.CreateHookExecutionParams{
		ID:                util.NewUUID(),
		WorkspaceID:       event.WorkspaceID,
		HookID:            hook.ID,
		HookRevisionID:    hook.ActiveRevisionID,
		EventID:           event.ID,
		CorrelationID:     event.CorrelationID,
		Status:            status,
		SkipReason:        reason,
		MatchSnapshot:     matchSnap,
		ConditionSnapshot: condSnap,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // ON CONFLICT DO NOTHING → already processed
		}
		return false, err
	}
	return true, nil
}

// readLatch returns the previous satisfied state of a rising-edge latch, treating
// a latch pinned to a different revision as fresh (not satisfied).
func readLatch(ctx context.Context, qtx *db.Queries, workspaceID, hookID, revisionID pgtype.UUID) (bool, error) {
	row, err := qtx.GetAutomationStateForUpdate(ctx, db.GetAutomationStateForUpdateParams{
		WorkspaceID: workspaceID, StateKind: latchStateKind, StateKey: util.UUIDToString(hookID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	var st latchState
	if len(row.State) > 0 {
		if err := json.Unmarshal(row.State, &st); err != nil {
			return false, err
		}
	}
	if st.RevisionID != util.UUIDToString(revisionID) {
		return false, nil // stale latch from a superseded revision
	}
	return st.Satisfied, nil
}

func upsertLatch(ctx context.Context, qtx *db.Queries, workspaceID, hookID, revisionID pgtype.UUID, satisfied bool) error {
	state, err := json.Marshal(latchState{Satisfied: satisfied, RevisionID: util.UUIDToString(revisionID)})
	if err != nil {
		return err
	}
	_, err = qtx.UpsertAutomationState(ctx, db.UpsertAutomationStateParams{
		WorkspaceID: workspaceID, StateKind: latchStateKind, StateKey: util.UUIDToString(hookID), State: state,
	})
	return err
}
