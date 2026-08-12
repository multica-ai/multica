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
// The matcher never runs actions: a fired hook lands `queued` for the executor. It
// only lands `queued` when automation_event_hook_execution is on for that workspace —
// otherwise the decision is recorded in a terminal status the executor can never
// claim, so an observation period can never fire retroactively.
//
// The loop itself is NOT gated: automation_event_hooks is evaluated per candidate
// row's workspace, and an event from a workspace that has it off is claimed and
// dispatched with no decisions.

const (
	hookExecQueued  = "queued"
	hookExecSkipped = "skipped"
	hookExecFailed  = "failed"

	// Stable skip_reason vocabulary, matching the ratified contract (§5.2 / §15.3).
	skipMaxDepth             = "max_depth"
	skipConditionFalse       = "condition_false"
	skipConditionAlreadyTrue = "condition_already_true"
	// skipExecutionDisabled marks a match observed while execution was off for the
	// workspace. Terminal on purpose — see the shadow note in decideOne.
	skipExecutionDisabled = "execution_disabled"

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

	// matcherCandidateWindow bounds how far a worker looks past candidates other
	// workers are already holding before giving up this attempt. It only needs to
	// exceed the number of concurrent matchers for a busy workspace to stop blocking
	// other workspaces.
	matcherCandidateWindow = 32

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

// errClaimRaceLost means another matcher claimed the peeked event first. Expected
// under concurrency; the tick simply moves on.
var errClaimRaceLost = errors.New("hook matcher: claim race lost")

// errOrphanedEvent means the peeked event's workspace no longer exists. It aborts
// the transaction so the caller can finalize the event terminally outside it.
var errOrphanedEvent = errors.New("hook matcher: event workspace is gone")

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
	// A bounded window of candidates, read WITHOUT locks. Walking a window (rather
	// than always retrying the single oldest row) is what preserves the
	// cross-workspace parallel drain the original FOR UPDATE SKIP LOCKED gave: a
	// candidate another worker already holds is skipped, not queued behind
	// (MUL-4332 review: convoy).
	candidates, err := s.Queries.PeekClaimableDomainEvents(ctx, matcherCandidateWindow)
	if err != nil {
		return false, false, err
	}
	for _, candidate := range candidates {
		took, ok, err := s.tryClaimAndDecide(ctx, candidate)
		if err != nil {
			return took, false, err
		}
		if took {
			return true, ok, nil
		}
		// Reserved by another worker, lost the claim race, or an orphan we
		// finalized: move to the next candidate WITHOUT consuming a batch slot.
	}
	return false, false, nil
}

// tryClaimAndDecide attempts one candidate. It reports whether this worker actually
// took the event (took=false means "skipped, try the next candidate", which must NOT
// consume a batch slot) and whether it was finalized as dispatched.
func (s *HookService) tryClaimAndDecide(ctx context.Context, candidate db.PeekClaimableDomainEventsRow) (took bool, dispatched bool, err error) {
	lease := util.NewUUID()
	var eventID, orphanEventID pgtype.UUID
	err = s.inTxWith(ctx, func(tx pgx.Tx, qtx *db.Queries) error {
		// WORKSPACE FIRST, then the event. This is the same order DeleteWorkspace
		// takes; claiming the event first inverted it and deadlocked (MUL-4332).
		if err := lockWorkspaceForAutomationWrite(ctx, qtx, candidate.WorkspaceID); err != nil {
			if errors.Is(err, ErrWorkspaceGone) {
				orphanEventID = candidate.ID
				return errOrphanedEvent
			}
			return err
		}

		// Reserve THIS candidate with SKIP LOCKED: if another worker already holds
		// it we skip to the next candidate instead of convoying behind it.
		if _, err := qtx.ReserveDomainEventForClaim(ctx, candidate.ID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errClaimRaceLost
			}
			return err
		}

		rows, err := qtx.ClaimOneEventWithCandidates(ctx, db.ClaimOneEventWithCandidatesParams{
			LeaseToken:      lease,
			LeaseTtlSeconds: MatcherLeaseTTL.Seconds(),
			EventID:         candidate.ID,
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return errClaimRaceLost
		}
		event := claimedEvent(rows[0])
		took, eventID = true, event.ID

		// PER-WORKSPACE gate (MUL-4332 review: workspace rollout). The loop used to
		// ask the flag once with the process root context, which carries no
		// workspace — so a workspace-targeted rule matched nothing and only a global
		// override could turn the engine on, for everyone at once.
		//
		// A disabled workspace's event is still CLAIMED and dispatched, with an empty
		// candidate set. Leaving it pending would be worse in both directions: the
		// oldest events of disabled workspaces would fill the ordered candidate
		// window and starve the one workspace under canary, and turning the flag on
		// later would replay the entire accumulated backlog at once. Dispatching it
		// undecided means the engine starts from "now" for whoever is enabled next.
		candidates := pinnedCandidates(rows)
		if !s.hooksEnabledFor(ctx, event.WorkspaceID) {
			candidates = nil
		}
		ok, err := s.decideAndFinalize(ctx, tx, qtx, event, candidates, lease)
		dispatched = ok
		return err
	})
	switch {
	case errors.Is(err, errClaimRaceLost):
		// Another worker owns this candidate. Not ours; try the next one.
		return false, false, nil
	case errors.Is(err, errOrphanedEvent):
		// Self-guarding: a no-op if the workspace turns out to still exist, so this
		// can never finalize a live event.
		if _, derr := s.Queries.MarkOrphanedDomainEventFailed(ctx, orphanEventID); derr != nil {
			slog.Warn("hook matcher: could not finalize an orphaned event",
				"event_id", util.UUIDToString(orphanEventID), "error", derr)
			return false, false, nil
		}
		slog.Warn("hook matcher: event workspace is gone, marked failed",
			"event_id", util.UUIDToString(orphanEventID))
		// Handled, but it was not a real decision — keep walking the window.
		return false, false, nil
	case errors.Is(err, errLeaseLost):
		if !took {
			return false, false, nil
		}
		// Our own fresh lease expired mid-decision. The rollback undid the claim, so
		// back the event off or it stays at the head of the queue and starves the
		// rest.
		if derr := s.deferFailedEvent(ctx, eventID); derr != nil {
			slog.Warn("hook matcher: could not back off an expired-lease event",
				"event_id", util.UUIDToString(eventID), "error", derr)
			return false, false, nil
		}
		slog.Warn("hook matcher: lease expired mid-decision, event backed off",
			"event_id", util.UUIDToString(eventID))
		return true, false, nil
	case err != nil:
		if derr := s.deferFailedEvent(ctx, eventID); derr != nil {
			slog.Warn("hook matcher: could not back off a failed event",
				"event_id", util.UUIDToString(eventID), "error", derr)
		}
		return took, false, err
	}
	return took, dispatched, nil
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

	// Join the workspace teardown protocol before deciding anything. An event whose
	// workspace is already gone is an orphan: it can never produce a valid decision,
	// so it is terminal here rather than an error that would halt this tick and leave
	// it at the head of the queue forever (MUL-4332 review: teardown race).
	if err := lockWorkspaceForAutomationWrite(ctx, qtx, owned.WorkspaceID); err != nil {
		if errors.Is(err, ErrWorkspaceGone) {
			rows, ferr := qtx.MarkDomainEventFailed(ctx, db.MarkDomainEventFailedParams{ID: owned.ID, LeaseToken: lease})
			if ferr != nil {
				return false, ferr
			}
			if rows != 1 {
				return false, errLeaseLost
			}
			slog.Warn("hook matcher: event workspace is gone, marked failed", "event_id", util.UUIDToString(owned.ID))
			return false, nil
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
		// Teardown protocol, before any hook row lock, so the order matches
		// DeleteWorkspace's and a decision cannot outlive its workspace.
		if err := lockWorkspaceForAutomationWrite(ctx, qtx, event.WorkspaceID); err != nil {
			return err
		}
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
	// SHADOW (MUL-4332 review: retroactive execution). Decided ONCE per event, for
	// this event's workspace. When execution is off, a match must NOT be recorded as
	// `queued`: the executor's claim query selects every queued row regardless of age,
	// so an entire observation period would fire the moment execution is switched on —
	// the opposite of "observe first, then enable safely". A shadow decision is
	// written in a TERMINAL status the claim can never select, while still recording
	// the full match/condition snapshot so the observation itself is preserved.
	shadow := !s.executionEnabledFor(ctx, event.WorkspaceID)
	for _, candidate := range candidates {
		sp, err := tx.Begin(ctx) // SAVEPOINT
		if err != nil {
			return err
		}
		err = processHookForEvent(ctx, s.Queries.WithTx(sp), event, view, candidate, shadow)
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
func processHookForEvent(ctx context.Context, qtx *db.Queries, event db.DomainEvent, view automation.EventView, candidate pinnedCandidate, shadow bool) error {
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
		case shadow:
			status, reason = hookExecSkipped, skipExecutionDisabled
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
	case shadow:
		status, reason = hookExecSkipped, skipExecutionDisabled
	}

	inserted, err := writeHookExecution(ctx, qtx, event, candidate, status, reason, matchSnap, condSnap)
	if err != nil {
		return err
	}
	// SHADOW DOES NOT LATCH (MUL-4332 review: shadow→live latch semantics). The latch
	// is durable state that decides future firing, so advancing it while nothing can
	// execute would consume the rising edge in shadow: after execution is enabled the
	// condition is already recorded as true, `prev` is true, and the hook would sit
	// skipped as `condition_already_true` until the condition happens to fall and rise
	// again — a silent never-fires. Leaving the latch untouched means shadow
	// over-reports "would fire" in the observation log (harmless, and arguably what
	// you want to see) and the first qualifying event AFTER enabling fires for real.
	if inserted && !shadow {
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
