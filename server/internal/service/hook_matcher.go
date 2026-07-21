package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// ONE AUTHORITATIVE TRANSACTION PER EVENT (§5.1, "matcher 在单事务快照内选择
// revision"). processEvent re-asserts lease ownership under a row lock, selects and
// pins every candidate revision, writes every decision and latch, and finalizes the
// event — all in a single transaction. A decision can therefore never be assembled
// from revisions read at different instants, and a matcher whose lease was already
// reclaimed writes nothing at all rather than racing the real owner.
//
// Inside that transaction each candidate runs in its own SAVEPOINT, so a revision
// whose stored config cannot be parsed is isolated as one terminal `failed` row
// instead of starving the healthy rules on the same event.
//
// This slice does NOT run actions — a fired hook lands `queued` for the executor
// (a later slice). With no executor and the automation_event_hooks flag off
// (the matcher loop is gated on it), production behaviour is unchanged.

const (
	hookExecQueued  = "queued"
	hookExecSkipped = "skipped"
	hookExecFailed  = "failed"

	// Stable skip_reason vocabulary, matching the ratified contract (§5.2 / §15.3).
	skipMaxDepth             = "max_depth"
	skipConditionFalse       = "condition_false"
	skipConditionAlreadyTrue = "condition_already_true"

	// errCodeInvalidConfig marks the isolation row written for a candidate whose
	// stored revision cannot be evaluated.
	errCodeInvalidConfig = "invalid_config"

	// latchStateKind keys rising-edge latches in automation_state; one row per hook.
	latchStateKind = "hook_edge"

	// maxHopCount is the loop-depth guard (§15.3: "hop_count 上限 8；超过上限的候选记
	// skipped(max_depth)"). The bound is INCLUSIVE: an event at hop_count == 8 is AT
	// the limit and still fires; only hop_count > 8 exceeds it and is skipped.
	maxHopCount = 8

	// MatcherBatchSize / MatcherLeaseTTL bound one matcher tick.
	MatcherBatchSize = 100
	MatcherLeaseTTL  = 2 * time.Minute
)

// errLeaseLost means the event's lease is not (or is no longer) ours. It is a
// normal, expected outcome — the current owner will process the event — not a
// failure, so it never propagates out of processEvent as an error.
var errLeaseLost = errors.New("hook matcher: event lease lost")

// latchState is the persisted rising-edge latch. RevisionID pins it to a
// revision so a config change starts a fresh edge rather than inheriting a stale
// satisfied flag.
type latchState struct {
	Satisfied  bool   `json:"satisfied"`
	RevisionID string `json:"revision_id"`
}

// hookConfigError wraps a DETERMINISTIC per-candidate config failure. It carries the
// revision it was pinned to so the caller can record the isolation row after rolling
// that candidate's savepoint back. Transient (database) failures are never wrapped
// in it, so the two can never be confused.
type hookConfigError struct {
	revisionID pgtype.UUID
	err        error
}

func (e *hookConfigError) Error() string { return e.err.Error() }
func (e *hookConfigError) Unwrap() error { return e.err }

// ClaimAndMatch leases up to batchSize pending events and processes each in its own
// authoritative transaction. It returns the number of events actually finalized as
// dispatched. An event whose processing fails is left un-dispatched so a later tick
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
		ok, err := s.processEvent(ctx, e, lease)
		if err != nil {
			slog.Warn("hook matcher: event failed, leaving for retry", "event_id", util.UUIDToString(e.ID), "error", err)
			continue
		}
		if ok {
			dispatched++
		}
	}
	return dispatched, nil
}

// processEvent is the matcher's authoritative transaction for one claimed event: it
// confirms lease ownership, selects and pins the candidate revisions, writes every
// decision and latch, and finalizes the event — atomically. It reports whether the
// event was finalized as dispatched.
//
// Ownership is asserted BEFORE any write, so a matcher whose lease was reclaimed
// (its own lease expired and another node took over) leaves behind no execution and
// no latch advance. And because the event row stays locked for the rest of the
// transaction, the lease cannot be stolen midway through the decision either.
func (s *HookService) processEvent(ctx context.Context, event db.DomainEvent, lease pgtype.UUID) (bool, error) {
	dispatched := false
	err := s.inTxWith(ctx, func(tx pgx.Tx, qtx *db.Queries) error {
		// 1. Re-assert lease ownership under a row lock, fail-closed.
		locked, err := qtx.GetDomainEventForDispatch(ctx, event.ID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errLeaseLost // event vanished (retention) — nothing to decide
			}
			return err
		}
		if locked.DispatchStatus != "dispatching" || !principalMatches(locked.LeaseToken, lease) {
			return errLeaseLost
		}

		// 2. Project the event once, so every candidate sees the same view. A payload
		// the matcher can never decode fails identically on every retry, so it is
		// terminal rather than re-leased forever.
		view, err := eventToView(locked)
		if err != nil {
			rows, ferr := qtx.MarkDomainEventFailed(ctx, db.MarkDomainEventFailedParams{ID: locked.ID, LeaseToken: lease})
			if ferr != nil {
				return ferr
			}
			if rows != 1 {
				return errLeaseLost
			}
			slog.Warn("hook matcher: undecodable event payload, marked failed", "event_id", util.UUIDToString(locked.ID), "error", err)
			return nil
		}

		// 3. Decide every candidate against this one snapshot.
		if err := s.decideCandidates(ctx, tx, qtx, locked, view); err != nil {
			return err
		}

		// 4. Finalize under the same lease CAS. Exactly one row must move.
		rows, err := qtx.MarkDomainEventDispatched(ctx, db.MarkDomainEventDispatchedParams{ID: locked.ID, LeaseToken: lease})
		if err != nil {
			return err
		}
		if rows != 1 {
			return errLeaseLost
		}
		dispatched = true
		return nil
	})
	if errors.Is(err, errLeaseLost) {
		return false, nil // another matcher owns this event; not an error
	}
	if err != nil {
		return false, err
	}
	return dispatched, nil
}

// MatchEvent decides every candidate hook for one event in a single transaction
// snapshot, without lease ownership or finalization. The production path is
// ClaimAndMatch, which runs this same decision INSIDE the authoritative transaction
// that owns the lease and finalizes the event; this entry point exists for direct,
// lease-free decision tests.
func (s *HookService) MatchEvent(ctx context.Context, event db.DomainEvent) error {
	view, err := eventToView(event)
	if err != nil {
		return err
	}
	return s.inTxWith(ctx, func(tx pgx.Tx, qtx *db.Queries) error {
		return s.decideCandidates(ctx, tx, qtx, event, view)
	})
}

// decideCandidates evaluates every candidate hook for one event within the caller's
// transaction. Each candidate runs in its own SAVEPOINT so one unusable revision can
// neither abort the transaction nor block the rest: a deterministic config failure is
// recorded as a terminal `failed` row and the loop continues, while a transient
// (database) failure aborts the whole event so it retries intact.
func (s *HookService) decideCandidates(ctx context.Context, tx pgx.Tx, qtx *db.Queries, event db.DomainEvent, view automation.EventView) error {
	hookIDs, err := qtx.ListActiveHookIDsForEvent(ctx, db.ListActiveHookIDsForEventParams{
		WorkspaceID: event.WorkspaceID,
		EventType:   event.Type,
	})
	if err != nil {
		return err
	}
	for _, hookID := range hookIDs {
		sp, err := tx.Begin(ctx) // SAVEPOINT
		if err != nil {
			return err
		}
		err = processHookForEvent(ctx, s.Queries.WithTx(sp), event, view, hookID)
		if err == nil {
			if err := sp.Commit(ctx); err != nil { // RELEASE SAVEPOINT
				return err
			}
			continue
		}
		// Undo only this candidate's partial work; the event's transaction lives on.
		if rberr := sp.Rollback(ctx); rberr != nil {
			return rberr
		}
		var cfgErr *hookConfigError
		if !errors.As(err, &cfgErr) {
			return err // transient — retry the whole event rather than lose a decision
		}
		if err := writeHookExecutionFailure(ctx, qtx, event, hookID, cfgErr); err != nil {
			return err
		}
		slog.Warn("hook matcher: candidate isolated, unusable revision",
			"event_id", util.UUIDToString(event.ID), "hook_id", util.UUIDToString(hookID), "error", cfgErr)
	}
	return nil
}

// processHookForEvent makes and persists the fire/skip decision for one (hook,
// event) pair. It locks the hook row first, which both pins the active revision
// against a concurrent edit and serializes the latch read-modify-write against
// other matchers.
func processHookForEvent(ctx context.Context, qtx *db.Queries, event db.DomainEvent, view automation.EventView, hookID pgtype.UUID) error {
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
		return &hookConfigError{
			revisionID: hook.ActiveRevisionID,
			err:        fmt.Errorf("%w: parse stored revision: %v", automation.ErrInvalidConfig, err),
		}
	}
	ev, err := automation.Evaluate(ctx, view, rev, &issueStateReader{q: qtx, workspaceID: event.WorkspaceID})
	if err != nil {
		if errors.Is(err, automation.ErrInvalidConfig) {
			return &hookConfigError{revisionID: hook.ActiveRevisionID, err: err}
		}
		return err // transient state-read failure
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

	// The depth guard decides only whether THIS event may fire. It never suppresses
	// the condition state a matched event observed (review point 3).
	overDepth := event.HopCount > maxHopCount

	if rawRev.FireMode != automation.FireRisingEdge {
		// per_event: fire whenever the conditions currently hold.
		status, reason := hookExecQueued, ""
		switch {
		case overDepth:
			status, reason = hookExecSkipped, skipMaxDepth
		case !ev.ConditionsMet:
			status, reason = hookExecSkipped, skipConditionFalse
		}
		_, err := writeHookExecution(ctx, qtx, event, hook, status, reason, matchSnap, condSnap)
		return err
	}

	// rising_edge: fire only on a false→true transition of the latch.
	prev, err := readLatch(ctx, qtx, event.WorkspaceID, hookID, hook.ActiveRevisionID)
	if err != nil {
		return err
	}
	nowSatisfied := ev.ConditionsMet

	status, reason := hookExecQueued, ""
	switch {
	case overDepth:
		status, reason = hookExecSkipped, skipMaxDepth
	case !nowSatisfied:
		status, reason = hookExecSkipped, skipConditionFalse
	case prev:
		status, reason = hookExecSkipped, skipConditionAlreadyTrue
	}

	inserted, err := writeHookExecution(ctx, qtx, event, hook, status, reason, matchSnap, condSnap)
	if err != nil {
		return err
	}
	// Record the condition state this matched event observed, exactly once per
	// (hook, event) — INCLUDING when the depth guard rejected the fire. Skipping the
	// advance there would strand the latch and swallow the next legitimate false→true
	// edge. Gating on `inserted` keeps a re-leased retry from double-advancing it.
	if inserted {
		return upsertLatch(ctx, qtx, event.WorkspaceID, hookID, hook.ActiveRevisionID, nowSatisfied)
	}
	return nil
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

// writeHookExecutionFailure records the terminal isolation row for a candidate whose
// stored revision could not be evaluated, so the event can finalize and the healthy
// candidates keep their decisions.
func writeHookExecutionFailure(ctx context.Context, qtx *db.Queries, event db.DomainEvent, hookID pgtype.UUID, cfgErr *hookConfigError) error {
	_, err := qtx.CreateHookExecutionFailure(ctx, db.CreateHookExecutionFailureParams{
		ID:             util.NewUUID(),
		WorkspaceID:    event.WorkspaceID,
		HookID:         hookID,
		HookRevisionID: cfgErr.revisionID,
		EventID:        event.ID,
		CorrelationID:  event.CorrelationID,
		ErrorCode:      pgtype.Text{String: errCodeInvalidConfig, Valid: true},
		Error:          pgtype.Text{String: cfgErr.Error(), Valid: true},
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err // ErrNoRows → the isolation row already exists
	}
	return nil
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
