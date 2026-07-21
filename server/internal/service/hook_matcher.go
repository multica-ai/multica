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

// The durable matcher (MUL-4332 PR3). It consumes pending domain_event rows and,
// for each enabled hook whose active revision listens to that event, runs the SAME
// automation.Evaluate as dry-run/explain, then completes the fire decision the
// read-only evaluator deliberately left open: the depth guard, the rising-edge
// latch, and per_event/rising_edge fire/skip. Each decision is persisted as one
// hook_execution row carrying the evaluator's structured snapshots and the pinned
// revision, idempotent per (hook, event).
//
// CLAIM AND PIN ARE THE SAME INSTANT. One transaction claims the event, materializes
// the complete (hook, revision) candidate set, decides every candidate, and
// finalizes the event. Two properties make the pin real rather than nominal:
//
//   - The claim runs INSIDE this transaction, so there is no window between leasing
//     the event and choosing its revisions in which an edit could land (§5.1:
//     "使用 matcher claim 时的当前 enabled revision").
//   - The candidate set comes from ONE statement. Under READ COMMITTED each
//     statement gets a fresh snapshot, so reading candidate ids and then re-reading
//     each hook's active_revision_id would let two candidates in the same
//     transaction be decided against revisions from different instants. The matcher
//     never re-reads active_revision_id after the pin.
//
// Ownership is asserted with one predicate — right token, still 'dispatching', and
// not expired under DATABASE clock time — used identically at entry and at the
// finalize CAS. A worker whose lease expired mid-decision therefore commits nothing.
//
// Inside the transaction each candidate runs in its own SAVEPOINT, so a revision
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

	// MatcherBatchSize bounds how many events one matcher tick claims and decides.
	MatcherBatchSize = 100

	// matcherFailureBackoff delays an event that failed transiently, so it cannot sit
	// at the head of the queue re-failing and starving everything behind it.
	matcherFailureBackoff = 30 * time.Second
)

// MatcherLeaseTTL bounds how long one event's decision may take. It is a var so
// tests can drive the expired-lease path deterministically.
var MatcherLeaseTTL = 2 * time.Minute

// errLeaseLost means this worker is not (or is no longer) the event's owner —
// wrong token, already finalized, or an expired lease. It always aborts the
// transaction, so a non-owner commits nothing. It is an expected outcome, not a
// failure, and never escapes ClaimAndMatch as an error.
var errLeaseLost = errors.New("hook matcher: event lease lost")

// errNoClaimableEvent ends a tick: the queue has nothing available right now.
var errNoClaimableEvent = errors.New("hook matcher: no claimable event")

// pinnedCandidate is one (hook, revision) pair fixed at claim time, together with
// the revision configuration the evaluator needs. The matcher decides against this
// and never re-reads hook.active_revision_id, which is what keeps a revision edit
// committed after the claim from changing this event's decision.
type pinnedCandidate struct {
	HookID     pgtype.UUID
	RevisionID pgtype.UUID
	Match      []byte
	Conditions []byte
	FireMode   string
}

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

// ClaimAndMatch claims and decides up to batchSize events, one authoritative
// transaction each, and returns how many were finalized as dispatched.
func (s *HookService) ClaimAndMatch(ctx context.Context, batchSize int32) (int, error) {
	dispatched := 0
	for i := int32(0); i < batchSize; i++ {
		claimed, ok, err := s.claimAndDecideOne(ctx)
		if err != nil {
			// The event rolled back to pending and has been backed off; stop this
			// tick rather than immediately re-claiming the same head-of-queue row.
			slog.Warn("hook matcher: event failed, deferred for retry", "error", err)
			return dispatched, nil
		}
		if !claimed {
			break // queue drained
		}
		if ok {
			dispatched++
		}
	}
	return dispatched, nil
}

// claimAndDecideOne is the matcher's authoritative unit of work: one transaction
// that claims an event, pins its candidate revisions, writes every decision and
// latch, and finalizes the event. It reports whether an event was claimed at all
// and whether it was finalized as dispatched.
func (s *HookService) claimAndDecideOne(ctx context.Context) (claimed bool, dispatched bool, err error) {
	lease := util.NewUUID()
	var eventID pgtype.UUID
	err = s.inTxWith(ctx, func(tx pgx.Tx, qtx *db.Queries) error {
		rows, err := qtx.ClaimOneEventWithCandidates(ctx, db.ClaimOneEventWithCandidatesParams{
			LeaseToken:      lease,
			LeaseTtlSeconds: MatcherLeaseTTL.Seconds(),
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return errNoClaimableEvent
		}
		event := claimedEvent(rows[0])
		claimed, eventID = true, event.ID
		ok, err := s.decideAndFinalize(ctx, tx, qtx, event, pinnedCandidates(rows), lease)
		dispatched = ok
		return err
	})
	switch {
	case errors.Is(err, errNoClaimableEvent):
		return false, false, nil
	case errors.Is(err, errLeaseLost):
		if !claimed {
			// We never took the event, so it is not ours to back off.
			return false, false, nil
		}
		// We DID claim it in this transaction and then lost ownership, which — with
		// the claim and the decision in one transaction and the event row locked
		// throughout — means our own fresh lease expired mid-decision. The rollback
		// also undid the claim, restoring the original available_at, so without a
		// backoff the next tick selects this same oldest event again and everything
		// behind it starves. Back it off exactly like any other failed decision.
		if derr := s.deferFailedEvent(ctx, eventID); derr != nil {
			slog.Warn("hook matcher: could not back off an expired-lease event",
				"event_id", util.UUIDToString(eventID), "error", derr)
			return false, false, nil // end the tick rather than spin on the same row
		}
		slog.Warn("hook matcher: lease expired mid-decision, event backed off",
			"event_id", util.UUIDToString(eventID))
		return true, false, nil // deferred — keep draining the rest of the queue
	case err != nil:
		if derr := s.deferFailedEvent(ctx, eventID); derr != nil {
			slog.Warn("hook matcher: could not back off a failed event",
				"event_id", util.UUIDToString(eventID), "error", derr)
		}
		return claimed, false, err
	}
	return claimed, dispatched, nil
}

// claimedEvent rebuilds the event envelope the decision needs from a claim row.
func claimedEvent(r db.ClaimOneEventWithCandidatesRow) db.DomainEvent {
	return db.DomainEvent{
		ID:            r.EventID,
		WorkspaceID:   r.EventWorkspaceID,
		Type:          r.EventType,
		SubjectID:     r.EventSubjectID,
		ActorType:     r.EventActorType,
		ActorID:       r.EventActorID,
		Payload:       r.EventPayload,
		CorrelationID: r.EventCorrelationID,
		HopCount:      r.EventHopCount,
	}
}

// pinnedCandidates extracts the candidate set from a claim result. An event with no
// candidates comes back as a single row with no hook, which yields an empty set.
func pinnedCandidates(rows []db.ClaimOneEventWithCandidatesRow) []pinnedCandidate {
	out := make([]pinnedCandidate, 0, len(rows))
	for _, r := range rows {
		if !r.HookID.Valid {
			continue
		}
		out = append(out, pinnedCandidate{
			HookID:     r.HookID,
			RevisionID: r.RevisionID,
			Match:      r.Match,
			Conditions: r.Conditions,
			FireMode:   r.FireMode,
		})
	}
	return out
}

// decideAndFinalize asserts ownership, decides every pinned candidate, and
// finalizes — all within the caller's claim transaction.
func (s *HookService) decideAndFinalize(ctx context.Context, tx pgx.Tx, qtx *db.Queries, event db.DomainEvent, candidates []pinnedCandidate, lease pgtype.UUID) (bool, error) {
	// Ownership, fail-closed, BEFORE any write. Same predicate as the finalize CAS,
	// including the not-expired condition under database clock time.
	owned, err := qtx.GetOwnedDomainEventForDispatch(ctx, db.GetOwnedDomainEventForDispatchParams{
		ID: event.ID, LeaseToken: lease,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, errLeaseLost
		}
		return false, err
	}

	// Project the event once, so every candidate sees the same view. A payload the
	// matcher can never decode fails identically on every retry, so it is terminal
	// rather than re-leased forever.
	view, err := eventToView(owned)
	if err != nil {
		rows, ferr := qtx.MarkDomainEventFailed(ctx, db.MarkDomainEventFailedParams{ID: owned.ID, LeaseToken: lease})
		if ferr != nil {
			return false, ferr
		}
		if rows != 1 {
			return false, errLeaseLost
		}
		slog.Warn("hook matcher: undecodable event payload, marked failed", "event_id", util.UUIDToString(owned.ID), "error", err)
		return false, nil
	}

	if err := s.decideCandidates(ctx, tx, qtx, owned, view, candidates); err != nil {
		return false, err
	}

	rows, err := qtx.MarkDomainEventDispatched(ctx, db.MarkDomainEventDispatchedParams{ID: owned.ID, LeaseToken: lease})
	if err != nil {
		return false, err
	}
	if rows != 1 {
		return false, errLeaseLost
	}
	return true, nil
}

// MatchEvent decides every candidate hook for one event, without claiming or
// finalizing it. The production path is ClaimAndMatch, which pins the candidate set
// in the SAME statement that claims the event; this entry point resolves candidates
// separately and exists for direct decision tests.
func (s *HookService) MatchEvent(ctx context.Context, event db.DomainEvent) error {
	view, err := eventToView(event)
	if err != nil {
		return err
	}
	return s.inTxWith(ctx, func(tx pgx.Tx, qtx *db.Queries) error {
		rows, err := qtx.ListActiveHookRevisionsForEvent(ctx, db.ListActiveHookRevisionsForEventParams{
			WorkspaceID: event.WorkspaceID,
			EventType:   event.Type,
		})
		if err != nil {
			return err
		}
		candidates := make([]pinnedCandidate, 0, len(rows))
		for _, r := range rows {
			candidates = append(candidates, pinnedCandidate{
				HookID:     r.HookID,
				RevisionID: r.RevisionID,
				Match:      r.Match,
				Conditions: r.Conditions,
				FireMode:   r.FireMode,
			})
		}
		return s.decideCandidates(ctx, tx, qtx, event, view, candidates)
	})
}

// decideCandidates decides each already-pinned candidate within the caller's
// transaction. Each candidate runs in its own SAVEPOINT so one unusable revision can
// neither abort the transaction nor block the rest: a deterministic config failure is
// recorded as a terminal `failed` row and the loop continues, while a transient
// (database) failure aborts the whole event so it retries intact.
func (s *HookService) decideCandidates(ctx context.Context, tx pgx.Tx, qtx *db.Queries, event db.DomainEvent, view automation.EventView, candidates []pinnedCandidate) error {
	for _, candidate := range candidates {
		sp, err := tx.Begin(ctx) // SAVEPOINT
		if err != nil {
			return err
		}
		err = processHookForEvent(ctx, s.Queries.WithTx(sp), event, view, candidate)
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
		if err := writeHookExecutionFailure(ctx, qtx, event, candidate.HookID, cfgErr); err != nil {
			return err
		}
		slog.Warn("hook matcher: candidate isolated, unusable revision",
			"event_id", util.UUIDToString(event.ID), "hook_id", util.UUIDToString(candidate.HookID), "error", cfgErr)
	}
	return nil
}

// processHookForEvent makes and persists the fire/skip decision for one (hook,
// event) pair against the revision pinned when the event was claimed.
func processHookForEvent(ctx context.Context, qtx *db.Queries, event db.DomainEvent, view automation.EventView, candidate pinnedCandidate) error {
	// Serialize this hook's latch read-modify-write against other matchers. The
	// revision is NOT re-read here — the pinned one below is authoritative.
	if _, err := qtx.LockHookForDecision(ctx, db.LockHookForDecisionParams{
		ID: candidate.HookID, WorkspaceID: event.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // hook vanished since the pin
		}
		return err
	}

	rev, err := pinnedRevisionToEval(candidate, event.Type)
	if err != nil {
		return &hookConfigError{
			revisionID: candidate.RevisionID,
			err:        fmt.Errorf("%w: parse stored revision: %v", automation.ErrInvalidConfig, err),
		}
	}
	ev, err := automation.Evaluate(ctx, view, rev, &issueStateReader{q: qtx, workspaceID: event.WorkspaceID})
	if err != nil {
		if errors.Is(err, automation.ErrInvalidConfig) {
			return &hookConfigError{revisionID: candidate.RevisionID, err: err}
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

	if candidate.FireMode != automation.FireRisingEdge {
		// per_event: fire whenever the conditions currently hold.
		status, reason := hookExecQueued, ""
		switch {
		case overDepth:
			status, reason = hookExecSkipped, skipMaxDepth
		case !ev.ConditionsMet:
			status, reason = hookExecSkipped, skipConditionFalse
		}
		_, err := writeHookExecution(ctx, qtx, event, candidate, status, reason, matchSnap, condSnap)
		return err
	}

	// rising_edge: fire only on a false→true transition of the latch.
	prev, err := readLatch(ctx, qtx, event.WorkspaceID, candidate.HookID, candidate.RevisionID)
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

	inserted, err := writeHookExecution(ctx, qtx, event, candidate, status, reason, matchSnap, condSnap)
	if err != nil {
		return err
	}
	// Record the condition state this matched event observed, exactly once per
	// (hook, event) — INCLUDING when the depth guard rejected the fire. Skipping the
	// advance there would strand the latch and swallow the next legitimate false→true
	// edge. Gating on `inserted` keeps a re-leased retry from double-advancing it.
	if inserted {
		return upsertLatch(ctx, qtx, event.WorkspaceID, candidate.HookID, candidate.RevisionID, nowSatisfied)
	}
	return nil
}

// pinnedRevisionToEval builds the evaluator's view of the revision pinned at claim
// time. eventType is the type the candidate query already matched on.
func pinnedRevisionToEval(candidate pinnedCandidate, eventType string) (automation.EvalRevision, error) {
	var conds []automation.ConditionSpec
	if len(candidate.Conditions) > 0 {
		if err := json.Unmarshal(candidate.Conditions, &conds); err != nil {
			return automation.EvalRevision{}, err
		}
	}
	return automation.EvalRevision{
		EventType:  eventType,
		Match:      candidate.Match,
		Conditions: conds,
		FireMode:   candidate.FireMode,
	}, nil
}

// deferFailedEvent backs an event off after a failed decision so it cannot hold the
// head of the queue. It runs on its own connection, outside the rolled-back decision
// transaction, and detached from ctx so a cancelled tick still records the backoff.
func (s *HookService) deferFailedEvent(ctx context.Context, eventID pgtype.UUID) error {
	if !eventID.Valid {
		return nil
	}
	_, err := s.Queries.DeferDomainEventDispatch(context.WithoutCancel(ctx), db.DeferDomainEventDispatchParams{
		ID:             eventID,
		BackoffSeconds: int32(matcherFailureBackoff.Seconds()),
	})
	return err
}

// writeHookExecution inserts one decision row idempotently, pinned to the revision
// chosen at claim time. It reports whether a new row was created (false means it
// already existed — a re-processed event).
func writeHookExecution(ctx context.Context, qtx *db.Queries, event db.DomainEvent, candidate pinnedCandidate, status, skipReason string, matchSnap, condSnap []byte) (bool, error) {
	reason := pgtype.Text{}
	if skipReason != "" {
		reason = pgtype.Text{String: skipReason, Valid: true}
	}
	_, err := qtx.CreateHookExecution(ctx, db.CreateHookExecutionParams{
		ID:                util.NewUUID(),
		WorkspaceID:       event.WorkspaceID,
		HookID:            candidate.HookID,
		HookRevisionID:    candidate.RevisionID,
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
