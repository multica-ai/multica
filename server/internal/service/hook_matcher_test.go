package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type matcherFixture struct {
	svc     *HookService
	pool    *pgxpool.Pool
	ws      string
	userID  string
	issueID string
}

func newMatcherFixture(t *testing.T) matcherFixture {
	t.Helper()
	pool := newTaskClaimRacePool(t) // skips if no DB
	ws, userID, _, issueID := seedAttributionFixture(t, pool)
	return matcherFixture{
		svc:     NewHookService(db.New(pool), pool),
		pool:    pool,
		ws:      ws,
		userID:  userID,
		issueID: issueID,
	}
}

// seedHook inserts a hook + immutable revision #1 directly (the matcher only
// reads them). Returns the hook id.
func (f matcherFixture) seedHook(t *testing.T, eventType, matchJSON, condJSON, fireMode string) string {
	t.Helper()
	ctx := context.Background()
	hookID, revID := uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook (id, workspace_id, name, enabled, active_revision_id, scope_type, origin,
			creator_actor_type, creator_actor_id, authorization_principal_user_id)
		VALUES ($1, $2, 'm hook', true, $3, 'workspace', 'user', 'member', $4, $4)`,
		hookID, f.ws, revID, f.userID); err != nil {
		t.Fatalf("seed hook: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook_revision (id, hook_id, revision, event_type, match, conditions, fire_mode, actions, created_by_type, created_by_id)
		VALUES ($1, $2, 1, $3, $4::jsonb, $5::jsonb, $6, '[]'::jsonb, 'member', $7)`,
		revID, hookID, eventType, matchJSON, condJSON, fireMode, f.userID); err != nil {
		t.Fatalf("seed hook_revision: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		f.pool.Exec(bg, `DELETE FROM hook_execution WHERE hook_id = $1`, hookID)
		f.pool.Exec(bg, `DELETE FROM automation_state WHERE workspace_id = $1 AND state_key = $2`, f.ws, hookID)
		f.pool.Exec(bg, `DELETE FROM hook_revision WHERE hook_id = $1`, hookID)
		f.pool.Exec(bg, `DELETE FROM hook WHERE id = $1`, hookID)
	})
	return hookID
}

// seedEvent inserts a pending issue.status_changed domain event and returns it.
func (f matcherFixture) seedEvent(t *testing.T, to string, hopCount int) db.DomainEvent {
	t.Helper()
	var id string
	payload := `{"from":"in_progress","to":"` + to + `"}`
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO domain_event (workspace_id, type, schema_version, subject_type, subject_id, actor_type, actor_id, payload, correlation_id, hop_count)
		VALUES ($1, 'issue.status_changed', 1, 'issue', $2, 'member', $3, $4::jsonb, gen_random_uuid(), $5)
		RETURNING id`, f.ws, f.issueID, f.userID, payload, hopCount).Scan(&id); err != nil {
		t.Fatalf("seed domain_event: %v", err)
	}
	t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM domain_event WHERE id = $1`, id) })
	ev, err := f.svc.Queries.GetDomainEvent(context.Background(), util.MustParseUUID(id))
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	return ev
}

func (f matcherFixture) setIssueStatus(t *testing.T, status string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE issue SET status = $1 WHERE id = $2`, status, f.issueID); err != nil {
		t.Fatalf("set issue status: %v", err)
	}
}

// seedHookAged is seedHook with an explicit age, so candidate order
// (the candidate query sorts by created_at ASC) is deterministic. An older
// hook is evaluated first.
func (f matcherFixture) seedHookAged(t *testing.T, eventType, matchJSON, condJSON, fireMode, age string) string {
	t.Helper()
	hookID := f.seedHook(t, eventType, matchJSON, condJSON, fireMode)
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE hook SET created_at = now() - $2::interval WHERE id = $1`, hookID, age); err != nil {
		t.Fatalf("age hook: %v", err)
	}
	return hookID
}

// claimWithLease puts an event into the state the matcher's claim leaves it in and
// returns the lease that owns it, so a test can drive the decision as either the real
// owner or a stale holder.
func (f matcherFixture) claimWithLease(t *testing.T, eventID pgtype.UUID) pgtype.UUID {
	t.Helper()
	lease := util.NewUUID()
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE domain_event
		SET dispatch_status = 'dispatching', lease_token = $2,
		    lease_expires_at = now() + interval '5 minutes', attempts = attempts + 1
		WHERE id = $1`, util.UUIDToString(eventID), util.UUIDToString(lease)); err != nil {
		t.Fatalf("claim event: %v", err)
	}
	return lease
}

// decideClaimed drives ownership + decision + finalize for one already-claimed
// event. Production claims, pins and decides in a single transaction
// (claimAndDecideOne); this resolves candidates separately so a test can target a
// specific event, but runs the very same decideAndFinalize.
func (f matcherFixture) decideClaimed(ctx context.Context, event db.DomainEvent, lease pgtype.UUID) (bool, error) {
	var dispatched bool
	var err error
	err = f.svc.inTxWith(ctx, func(tx pgx.Tx, qtx *db.Queries) error {
		rows, err := qtx.ListActiveHookRevisionsForEvent(ctx, db.ListActiveHookRevisionsForEventParams{
			WorkspaceID: event.WorkspaceID, EventType: event.Type,
		})
		if err != nil {
			return err
		}
		candidates := make([]pinnedCandidate, 0, len(rows))
		for _, r := range rows {
			candidates = append(candidates, pinnedCandidate{
				HookID: r.HookID, RevisionID: r.RevisionID,
				Match: r.Match, Conditions: r.Conditions, FireMode: r.FireMode,
			})
		}
		ok, err := f.svc.decideAndFinalize(ctx, tx, qtx, event, candidates, lease)
		dispatched = ok
		return err
	})
	if errors.Is(err, errLeaseLost) {
		return false, nil
	}
	return dispatched, err
}

type eventState struct {
	status          string
	lease           string // "" when NULL
	hasDispatchedAt bool
}

func (f matcherFixture) eventState(t *testing.T, eventID pgtype.UUID) eventState {
	t.Helper()
	var s eventState
	if err := f.pool.QueryRow(context.Background(), `
		SELECT dispatch_status, COALESCE(lease_token::text, ''), dispatched_at IS NOT NULL
		FROM domain_event WHERE id = $1`, util.UUIDToString(eventID)).
		Scan(&s.status, &s.lease, &s.hasDispatchedAt); err != nil {
		t.Fatalf("load event state: %v", err)
	}
	return s
}

// latchFor reads the persisted rising-edge latch for a hook.
func (f matcherFixture) latchFor(t *testing.T, hookID string) (satisfied, found bool) {
	t.Helper()
	err := f.pool.QueryRow(context.Background(),
		`SELECT (state->>'satisfied')::bool FROM automation_state
		 WHERE workspace_id = $1 AND state_kind = $2 AND state_key = $3`,
		f.ws, latchStateKind, hookID).Scan(&satisfied)
	if err != nil {
		return false, false
	}
	return satisfied, true
}

type execRow struct {
	status     string
	skipReason string
	errorCode  string
	revisionID string
	matchSnap  []byte
	condSnap   []byte
	count      int
}

func (f matcherFixture) execFor(t *testing.T, hookID string) execRow {
	t.Helper()
	var r execRow
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM hook_execution WHERE hook_id = $1`, hookID).Scan(&r.count); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if r.count == 0 {
		return r
	}
	if err := f.pool.QueryRow(context.Background(), `
		SELECT status, COALESCE(skip_reason,''), COALESCE(error_code,''), hook_revision_id::text, match_snapshot, condition_snapshot
		FROM hook_execution WHERE hook_id = $1 ORDER BY created_at DESC LIMIT 1`, hookID).
		Scan(&r.status, &r.skipReason, &r.errorCode, &r.revisionID, &r.matchSnap, &r.condSnap); err != nil {
		t.Fatalf("load execution: %v", err)
	}
	return r
}

func TestMatcherPerEventFires(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	hookID := f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent)
	event := f.seedEvent(t, "done", 0)

	if err := f.svc.MatchEvent(ctx, event); err != nil {
		t.Fatalf("match: %v", err)
	}
	r := f.execFor(t, hookID)
	if r.count != 1 || r.status != hookExecQueued {
		t.Fatalf("expected 1 queued execution, got count=%d status=%q", r.count, r.status)
	}
	if len(r.matchSnap) == 0 || len(r.condSnap) == 0 {
		t.Errorf("execution is missing snapshots")
	}
}

func TestMatcherConditionFalseSkips(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")
	cond := `[{"issues_status":{"ids":["` + f.issueID + `"],"all":"done"}}]`
	hookID := f.seedHook(t, "issue.status_changed", `{}`, cond, automation.FirePerEvent)
	event := f.seedEvent(t, "in_progress", 0)

	if err := f.svc.MatchEvent(ctx, event); err != nil {
		t.Fatalf("match: %v", err)
	}
	if r := f.execFor(t, hookID); r.status != hookExecSkipped || r.skipReason != skipConditionFalse {
		t.Fatalf("expected skipped condition_false, got %+v", r)
	}
}

// The depth bound is INCLUSIVE (§15.3: "上限 8；超过上限的候选记 skipped(max_depth)").
// hop_count == 8 is AT the limit and still fires; only hop_count > 8 exceeds it.
// Pinning both sides makes the boundary explicit rather than an artifact of >= vs >.
func TestMatcherHopGuardBoundary(t *testing.T) {
	for _, tc := range []struct {
		name       string
		hop        int
		wantStatus string
		wantReason string
	}{
		{"at the limit fires", maxHopCount, hookExecQueued, ""},
		{"over the limit is skipped", maxHopCount + 1, hookExecSkipped, skipMaxDepth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMatcherFixture(t)
			ctx := context.Background()
			hookID := f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent)

			if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "done", tc.hop)); err != nil {
				t.Fatalf("match: %v", err)
			}
			if r := f.execFor(t, hookID); r.status != tc.wantStatus || r.skipReason != tc.wantReason {
				t.Fatalf("hop_count=%d: want status=%q reason=%q, got %+v", tc.hop, tc.wantStatus, tc.wantReason, r)
			}
		})
	}
}

// The rising-edge latch: fires on false→true, holds while true, resets on false,
// then fires again on the next false→true.
func TestMatcherRisingEdgeLatch(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	cond := `[{"issues_status":{"ids":["` + f.issueID + `"],"all":"done"}}]`
	hookID := f.seedHook(t, "issue.status_changed", `{}`, cond, automation.FireRisingEdge)

	assertLast := func(want, reason string) {
		t.Helper()
		if r := f.execFor(t, hookID); r.status != want || r.skipReason != reason {
			t.Fatalf("want status=%q reason=%q, got %+v", want, reason, r)
		}
	}

	// false→true: condition satisfied for the first time → fires.
	f.setIssueStatus(t, "done")
	if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "done", 0)); err != nil {
		t.Fatal(err)
	}
	assertLast(hookExecQueued, "")

	// still true: another matching event → no new edge.
	if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "done", 0)); err != nil {
		t.Fatal(err)
	}
	assertLast(hookExecSkipped, skipConditionAlreadyTrue)

	// condition drops to false → latch resets.
	f.setIssueStatus(t, "todo")
	if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "todo", 0)); err != nil {
		t.Fatal(err)
	}
	assertLast(hookExecSkipped, skipConditionFalse)

	// false→true again → fires again.
	f.setIssueStatus(t, "done")
	if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "done", 0)); err != nil {
		t.Fatal(err)
	}
	assertLast(hookExecQueued, "")
}

// Re-processing the same event must not double-create the execution or double-
// advance the latch.
func TestMatcherIdempotent(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	cond := `[{"issues_status":{"ids":["` + f.issueID + `"],"all":"done"}}]`
	hookID := f.seedHook(t, "issue.status_changed", `{}`, cond, automation.FireRisingEdge)
	f.setIssueStatus(t, "done")
	event := f.seedEvent(t, "done", 0)

	for i := 0; i < 3; i++ {
		if err := f.svc.MatchEvent(ctx, event); err != nil {
			t.Fatalf("match %d: %v", i, err)
		}
	}
	if r := f.execFor(t, hookID); r.count != 1 || r.status != hookExecQueued {
		t.Fatalf("re-processing created %d rows (want 1), status=%q", r.count, r.status)
	}
	// The latch advanced exactly once: a further, distinct event must NOT re-fire.
	if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "done", 0)); err != nil {
		t.Fatal(err)
	}
	if r := f.execFor(t, hookID); r.status != hookExecSkipped || r.skipReason != skipConditionAlreadyTrue {
		t.Fatalf("latch double-advanced: next event fired again (%+v)", r)
	}
}

// Hard acceptance: the matcher's stored snapshots are byte-identical to the shared
// evaluator's output for the same (event, revision, state) — the same result
// explain returns, so an explanation can never drift from a real decision.
func TestMatcherSnapshotConsistency(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")
	cond := `[{"issues_status":{"ids":["` + f.issueID + `"],"all":"done"}}]`
	hookID := f.seedHook(t, "issue.status_changed", `{"to":"done"}`, cond, automation.FirePerEvent)
	event := f.seedEvent(t, "done", 0)

	// Compute the expected snapshots through the SAME evaluator path explain uses.
	hook, err := f.svc.Queries.GetHookInWorkspace(ctx, db.GetHookInWorkspaceParams{ID: util.MustParseUUID(hookID), WorkspaceID: util.MustParseUUID(f.ws)})
	if err != nil {
		t.Fatal(err)
	}
	rawRev, err := f.svc.Queries.GetHookRevision(ctx, hook.ActiveRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := revisionToEval(rawRev)
	view, _ := eventToView(event)
	ev, err := automation.Evaluate(ctx, view, rev, &issueStateReader{q: f.svc.Queries, workspaceID: util.MustParseUUID(f.ws)})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.MatchEvent(ctx, event); err != nil {
		t.Fatalf("match: %v", err)
	}
	r := f.execFor(t, hookID)

	// The snapshot column is jsonb, so Postgres re-serializes it (key order /
	// whitespace); compare the PARSED structured content against the evaluator's
	// output — that is the drift-free contract that matters.
	var gotMatch []automation.ClauseResult
	if err := json.Unmarshal(r.matchSnap, &gotMatch); err != nil {
		t.Fatalf("parse stored match_snapshot: %v", err)
	}
	if !reflect.DeepEqual(gotMatch, ev.MatchClauses) {
		t.Errorf("match_snapshot content differs from evaluator:\n stored=%+v\n eval  =%+v", gotMatch, ev.MatchClauses)
	}
	var gotCond []automation.ConditionResult
	if err := json.Unmarshal(r.condSnap, &gotCond); err != nil {
		t.Fatalf("parse stored condition_snapshot: %v", err)
	}
	if !reflect.DeepEqual(gotCond, ev.Conditions) {
		t.Errorf("condition_snapshot content differs from evaluator:\n stored=%+v\n eval  =%+v", gotCond, ev.Conditions)
	}
	if r.revisionID != util.UUIDToString(hook.ActiveRevisionID) {
		t.Errorf("revision not pinned: stored=%s active=%s", r.revisionID, util.UUIDToString(hook.ActiveRevisionID))
	}
}

// ClaimAndMatch leases a pending event, matches it, and marks it dispatched.
func TestMatcherClaimAndMatchDispatches(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	hookID := f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent)
	event := f.seedEvent(t, "done", 0)

	// ClaimAndMatch is a GLOBAL outbox consumer (not workspace-scoped) and the
	// shared CI test DB carries a backlog of undispatched events from other tests.
	// Our event has the newest (highest) seq, so drain in bounded batches until it
	// is reached — its seq is fixed, and concurrently-added events have higher seq,
	// so this converges.
	var status string
	for i := 0; i < 60; i++ {
		if _, err := f.svc.ClaimAndMatch(ctx, 500); err != nil {
			t.Fatalf("claim and match: %v", err)
		}
		f.pool.QueryRow(ctx, `SELECT dispatch_status FROM domain_event WHERE id = $1`, util.UUIDToString(event.ID)).Scan(&status)
		if status == "dispatched" {
			break
		}
	}
	if status != "dispatched" {
		t.Fatalf("event dispatch_status = %q after draining, want dispatched", status)
	}
	if r := f.execFor(t, hookID); r.status != hookExecQueued {
		t.Errorf("expected queued execution after claim+match, got %+v", r)
	}
}

// A disabled hook is not a candidate.
func TestMatcherDisabledHookNotCandidate(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	hookID := f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent)
	f.pool.Exec(ctx, `UPDATE hook SET enabled = false WHERE id = $1`, hookID)

	if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "done", 0)); err != nil {
		t.Fatalf("match: %v", err)
	}
	if r := f.execFor(t, hookID); r.count != 0 {
		t.Errorf("disabled hook produced %d executions, want 0", r.count)
	}
}

// ---------------------------------------------------------------------------
// Deterministic race regressions for the four matcher must-fixes
// (MUL-4332 PR3 review round @ c4cfdcac8).
// ---------------------------------------------------------------------------

// Must-fix 1 — the decision is ONE authoritative transaction, so candidate
// revisions are pinned from a single snapshot together with the finalize.
//
// Previously each candidate ran in its own transaction, committing as it went while
// the event stayed un-finalized. That is what let a decision be assembled from
// revisions read at different instants (and, across a retry, mixed). This locks the
// SECOND candidate's hook row and proves the FIRST candidate's decision is still
// invisible while the matcher blocks on it — nothing is committed until the whole
// event is. Under the old per-candidate transactions hook A's execution was already
// durable at this point and this test fails.
func TestMatcherDecisionIsAtomicAcrossCandidates(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()

	hookA := f.seedHookAged(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent, "2 minutes")
	hookB := f.seedHookAged(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent, "1 minute")
	event := f.seedEvent(t, "done", 0)
	lease := f.claimWithLease(t, event.ID)

	// Hold hook B's row exactly as a concurrent PATCH (which locks the hook to
	// allocate a revision) would.
	lockConn, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock conn: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			lockConn.Release()
		}
	}
	defer release()
	lockTx, err := lockConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	if _, err := lockTx.Exec(ctx, `SELECT id FROM hook WHERE id = $1 FOR UPDATE`, hookB); err != nil {
		t.Fatalf("lock hook B: %v", err)
	}

	type result struct {
		dispatched bool
		err        error
	}
	done := make(chan result, 1)
	go func() {
		ok, err := f.decideClaimed(ctx, event, lease)
		done <- result{dispatched: ok, err: err}
	}()

	// While blocked on hook B, NOTHING may be visible yet — not hook A's decision,
	// not the finalize. This is the single-transaction guarantee.
	select {
	case res := <-done:
		t.Fatalf("decision completed (dispatched=%v err=%v) while hook B was locked", res.dispatched, res.err)
	case <-time.After(750 * time.Millisecond):
	}
	if r := f.execFor(t, hookA); r.count != 0 {
		t.Fatalf("hook A already has %d execution(s) while the event transaction is still open — "+
			"a candidate decision committed on its own instead of within the event's single snapshot", r.count)
	}
	if s := f.eventState(t, event.ID); s.status != "dispatching" || s.hasDispatchedAt {
		t.Fatalf("event finalized early: %+v", s)
	}

	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release hook B lock: %v", err)
	}
	release()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("decide: %v", res.err)
		}
		if !res.dispatched {
			t.Fatal("decision did not dispatch the event after the lock released")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("decision never unblocked after the hook lock released")
	}

	// Both candidates landed together, each pinned to the revision the snapshot saw.
	for _, hookID := range []string{hookA, hookB} {
		r := f.execFor(t, hookID)
		if r.count != 1 || r.status != hookExecQueued {
			t.Errorf("hook %s: count=%d status=%q, want exactly 1 queued", hookID, r.count, r.status)
		}
		var activeRev string
		if err := f.pool.QueryRow(ctx, `SELECT active_revision_id::text FROM hook WHERE id = $1`, hookID).Scan(&activeRev); err != nil {
			t.Fatal(err)
		}
		if r.revisionID != activeRev {
			t.Errorf("hook %s: execution pinned revision %s, want %s", hookID, r.revisionID, activeRev)
		}
	}
	if s := f.eventState(t, event.ID); s.status != "dispatched" || !s.hasDispatchedAt {
		t.Errorf("event state = %+v, want dispatched with dispatched_at set", s)
	}
}

// Must-fix 2 — a stale lease holder must fail closed BEFORE writing anything, and
// the finalize CAS result must be checked.
//
// Previously the lease was only CAS'd on the final UPDATE: a matcher whose lease had
// been reclaimed still wrote its hook_execution and advanced the latch, the CAS then
// affected 0 rows, and the event was still counted as dispatched. Now ownership is
// re-asserted under a row lock first, so the stale holder writes nothing at all.
// This also pins the dispatched_at gap: 'dispatched' must never mean a NULL timestamp.
func TestMatcherStaleLeaseHolderWritesNothing(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()

	// rising_edge + a satisfied condition, so a stale write would leave BOTH an
	// execution row and an advanced latch behind.
	cond := `[{"issues_status":{"ids":["` + f.issueID + `"],"all":"done"}}]`
	hookID := f.seedHook(t, "issue.status_changed", `{}`, cond, automation.FireRisingEdge)
	f.setIssueStatus(t, "done")
	event := f.seedEvent(t, "done", 0)

	owner := f.claimWithLease(t, event.ID) // the lease that currently owns the event
	stale := util.NewUUID()                // a matcher whose lease was already reclaimed

	// Ownership must be re-asserted BEFORE any decision work, not merely discovered
	// at the finalize CAS. Hold the hook row as a concurrent edit would: a stale
	// holder that had already started deciding would block here, so returning
	// promptly is the observable proof that it failed closed up front.
	lockConn, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock conn: %v", err)
	}
	lockTx, err := lockConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	if _, err := lockTx.Exec(ctx, `SELECT id FROM hook WHERE id = $1 FOR UPDATE`, hookID); err != nil {
		t.Fatalf("lock hook: %v", err)
	}

	type staleResult struct {
		dispatched bool
		err        error
	}
	staleDone := make(chan staleResult, 1)
	go func() {
		ok, err := f.decideClaimed(ctx, event, stale)
		staleDone <- staleResult{dispatched: ok, err: err}
	}()

	var res staleResult
	select {
	case res = <-staleDone:
	case <-time.After(750 * time.Millisecond):
		lockTx.Rollback(ctx)
		lockConn.Release()
		t.Fatal("stale lease holder blocked on the hook row — it began deciding before re-asserting lease ownership")
	}
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release hook lock: %v", err)
	}
	lockConn.Release()

	if res.err != nil {
		t.Fatalf("stale holder returned an error: %v", res.err)
	}
	if res.dispatched {
		t.Error("stale lease holder reported the event dispatched")
	}
	if r := f.execFor(t, hookID); r.count != 0 {
		t.Errorf("stale lease holder wrote %d execution(s), want 0", r.count)
	}
	if _, found := f.latchFor(t, hookID); found {
		t.Error("stale lease holder advanced the rising-edge latch")
	}
	if s := f.eventState(t, event.ID); s.status != "dispatching" || s.lease != util.UUIDToString(owner) || s.hasDispatchedAt {
		t.Errorf("stale holder disturbed the event: %+v", s)
	}

	// The real owner still decides it normally.
	ok, err := f.decideClaimed(ctx, event, owner)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	if !ok {
		t.Fatal("owner did not dispatch the event")
	}
	if r := f.execFor(t, hookID); r.count != 1 || r.status != hookExecQueued {
		t.Errorf("owner decision: count=%d status=%q, want 1 queued", r.count, r.status)
	}
	s := f.eventState(t, event.ID)
	if s.status != "dispatched" {
		t.Errorf("event status = %q, want dispatched", s.status)
	}
	if !s.hasDispatchedAt {
		t.Error("dispatched event has a NULL dispatched_at — the retention/audit boundary cannot rely on it")
	}
	if s.lease != "" {
		t.Errorf("dispatched event still holds lease %s", s.lease)
	}
}

// Must-fix 3 — the depth guard decides only whether THIS event fires; it must not
// skip the rising-edge latch advance.
//
// Elon's sequence: true → (an over-deep event observes false) → true. Previously the
// guard returned before touching the latch, so the middle event never reset it and
// the next legitimate false→true edge was swallowed as condition_already_true.
func TestMatcherDepthGuardStillAdvancesLatch(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	cond := `[{"issues_status":{"ids":["` + f.issueID + `"],"all":"done"}}]`
	hookID := f.seedHook(t, "issue.status_changed", `{}`, cond, automation.FireRisingEdge)

	assertLast := func(step, wantStatus, wantReason string) {
		t.Helper()
		if r := f.execFor(t, hookID); r.status != wantStatus || r.skipReason != wantReason {
			t.Fatalf("%s: want status=%q reason=%q, got %+v", step, wantStatus, wantReason, r)
		}
	}

	// 1. false→true at a normal depth: fires, latch true.
	f.setIssueStatus(t, "done")
	if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "done", 0)); err != nil {
		t.Fatal(err)
	}
	assertLast("initial edge", hookExecQueued, "")
	if satisfied, found := f.latchFor(t, hookID); !found || !satisfied {
		t.Fatalf("latch after initial edge: satisfied=%v found=%v, want true/true", satisfied, found)
	}

	// 2. The condition drops to false and is observed by an OVER-DEEP event. The
	// guard skips the fire, but the observation must still be recorded.
	f.setIssueStatus(t, "todo")
	if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "todo", maxHopCount+1)); err != nil {
		t.Fatal(err)
	}
	assertLast("over-deep event", hookExecSkipped, skipMaxDepth)
	if satisfied, found := f.latchFor(t, hookID); !found || satisfied {
		t.Fatalf("the depth guard skipped the latch advance: satisfied=%v found=%v, want false/true — "+
			"a matched event must record its condition state even when the guard rejects the fire", satisfied, found)
	}

	// 3. The next legitimate false→true edge must fire, not be swallowed.
	f.setIssueStatus(t, "done")
	if err := f.svc.MatchEvent(ctx, f.seedEvent(t, "done", 0)); err != nil {
		t.Fatal(err)
	}
	assertLast("edge after the guarded event", hookExecQueued, "")
}

// Must-fix 4 — one unusable revision must not starve the healthy rules on the same
// event, and must not make the event retry forever.
//
// Previously MatchEvent returned at the first error, so a poison candidate early in
// the fixed candidate order meant later healthy hooks got no execution and the event
// was never finalized — it re-leased and re-hit the same poison on every tick.
func TestMatcherPoisonHookDoesNotStarveHealthy(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()

	// `conditions` must be a JSON array; an object is valid jsonb the evaluator can
	// never interpret — a deterministic config failure, not a transient one.
	poison := f.seedHookAged(t, "issue.status_changed", `{"to":"done"}`, `{"broken":true}`, automation.FirePerEvent, "2 minutes")
	healthy := f.seedHookAged(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent, "1 minute")
	event := f.seedEvent(t, "done", 0)
	lease := f.claimWithLease(t, event.ID)

	ok, err := f.decideClaimed(ctx, event, lease)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !ok {
		t.Fatal("event was not dispatched — one poison candidate blocked the whole event")
	}

	if r := f.execFor(t, healthy); r.count != 1 || r.status != hookExecQueued {
		t.Errorf("healthy hook: count=%d status=%q, want 1 queued — a poison candidate starved it", r.count, r.status)
	}
	r := f.execFor(t, poison)
	if r.count != 1 || r.status != hookExecFailed {
		t.Errorf("poison hook: count=%d status=%q, want exactly 1 failed isolation row", r.count, r.status)
	}
	if r.errorCode != errCodeInvalidConfig {
		t.Errorf("poison hook error_code = %q, want %q", r.errorCode, errCodeInvalidConfig)
	}
	if s := f.eventState(t, event.ID); s.status != "dispatched" || !s.hasDispatchedAt {
		t.Errorf("event state = %+v, want dispatched with dispatched_at set", s)
	}

	// Re-deciding the same event stays idempotent for both candidates.
	if err := f.svc.MatchEvent(ctx, event); err != nil {
		t.Fatalf("re-decide: %v", err)
	}
	if r := f.execFor(t, healthy); r.count != 1 {
		t.Errorf("healthy hook has %d executions after re-decide, want 1", r.count)
	}
	if r := f.execFor(t, poison); r.count != 1 {
		t.Errorf("poison hook has %d executions after re-decide, want 1", r.count)
	}
}

// ---------------------------------------------------------------------------
// Regressions for the second matcher review round: claim-time revision pin under
// a single snapshot, and zero writes from an expired lease.
// ---------------------------------------------------------------------------

// drainMatcherQueue empties the shared outbox so a following controlled claim lands
// on this test's own event.
func (f matcherFixture) drainMatcherQueue(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 60; i++ {
		n, err := f.svc.ClaimAndMatch(ctx, 500)
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("outbox never drained")
}

// sleepDuringClaim makes the matcher's claim UPDATE on one specific event pause, so
// a test can commit a concurrent edit inside the claim window.
func (f matcherFixture) sleepDuringClaim(t *testing.T, eventID pgtype.UUID, seconds float64) {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("hook_claim_sleep_%d", time.Now().UnixNano())
	fn := name + "_fn"
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(%f); RETURN NEW; END; $$;`, quoteIdent(fn), seconds)); err != nil {
		t.Fatalf("create claim sleep function: %v", err)
	}
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s BEFORE UPDATE ON domain_event
		FOR EACH ROW WHEN (NEW.id = %s::uuid AND NEW.dispatch_status = 'dispatching')
		EXECUTE FUNCTION %s();`, quoteIdent(name), quoteLiteral(util.UUIDToString(eventID)), quoteIdent(fn))); err != nil {
		t.Fatalf("create claim sleep trigger: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		f.pool.Exec(bg, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON domain_event", quoteIdent(name)))
		f.pool.Exec(bg, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", quoteIdent(fn)))
	})
}

// Must-fix 1 (round 2) — the revision set is pinned by the CLAIM statement itself.
//
// Wrapping the decision in a transaction is not enough: PostgreSQL's default READ
// COMMITTED gives every statement a fresh snapshot, so claiming in one statement and
// resolving candidates in another lets an edit committed in between change the
// decision — and lets two candidates in one transaction be decided against revisions
// from different instants. This drives the real ClaimAndMatch, pauses inside the
// claim, and commits a revision swap to a DIFFERENT event type in that window. The
// claim and the candidate set share one snapshot, so the decision must still bind
// revision 1. Anything that resolved candidates after the claim would see revision 2,
// find no candidate, and write nothing.
func TestMatcherPinsRevisionAtClaimTime(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.drainMatcherQueue(t)

	hookID := f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent)
	var rev1 string
	if err := f.pool.QueryRow(ctx, `SELECT active_revision_id::text FROM hook WHERE id = $1`, hookID).Scan(&rev1); err != nil {
		t.Fatal(err)
	}
	event := f.seedEvent(t, "done", 0)
	f.sleepDuringClaim(t, event.ID, 0.8)

	done := make(chan error, 1)
	go func() {
		_, err := f.svc.ClaimAndMatch(ctx, 5)
		done <- err
	}()

	// Land the edit inside the claim window: a new revision listening to a different
	// event type, made active while the claim is still executing.
	time.Sleep(250 * time.Millisecond)
	rev2 := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook_revision (id, hook_id, revision, event_type, match, conditions, fire_mode, actions, created_by_type, created_by_id)
		VALUES ($1, $2, 2, 'issue.created', '{}'::jsonb, '[]'::jsonb, $3, '[]'::jsonb, 'member', $4)`,
		rev2, hookID, automation.FirePerEvent, f.userID); err != nil {
		t.Fatalf("seed revision 2: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE hook SET active_revision_id = $2 WHERE id = $1`, hookID, rev2); err != nil {
		t.Fatalf("repoint active revision: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("claim and match: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("claim and match never returned")
	}

	r := f.execFor(t, hookID)
	if r.count != 1 || r.status != hookExecQueued {
		t.Fatalf("want exactly 1 queued execution from the claim-time revision, got count=%d status=%q "+
			"(a revision edit landing after the claim changed the decision)", r.count, r.status)
	}
	if r.revisionID != rev1 {
		t.Errorf("execution pinned revision %s, want the claim-time revision %s", r.revisionID, rev1)
	}
	if s := f.eventState(t, event.ID); s.status != "dispatched" || !s.hasDispatchedAt {
		t.Errorf("event state = %+v, want dispatched with dispatched_at set", s)
	}
}

// Must-fix 2 (round 2) — a worker whose lease has EXPIRED writes nothing.
//
// The ownership predicate previously checked only status + token, so a worker still
// holding the right token past its expiry could decide and finalize. Ownership now
// requires the lease to be unexpired under database clock time, at entry and at the
// finalize CAS alike. Driving the real ClaimAndMatch with an already-elapsed TTL
// makes the claimed lease expired on arrival, so the whole transaction must roll back.
func TestMatcherExpiredLeaseWritesNothing(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.drainMatcherQueue(t)

	// rising_edge with a satisfied condition, so a leaked write would leave BOTH an
	// execution row and an advanced latch behind.
	cond := `[{"issues_status":{"ids":["` + f.issueID + `"],"all":"done"}}]`
	hookID := f.seedHook(t, "issue.status_changed", `{}`, cond, automation.FireRisingEdge)
	f.setIssueStatus(t, "done")
	event := f.seedEvent(t, "done", 0)

	original := MatcherLeaseTTL
	t.Cleanup(func() { MatcherLeaseTTL = original })

	// Every lease this tick acquires is already expired when it is granted. Hold the
	// hook row as a concurrent edit would: an expired worker must fail closed BEFORE
	// any decision work, so it never reaches (and never blocks on) this lock.
	MatcherLeaseTTL = -1 * time.Minute
	lockConn, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock conn: %v", err)
	}
	lockTx, err := lockConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	if _, err := lockTx.Exec(ctx, `SELECT id FROM hook WHERE id = $1 FOR UPDATE`, hookID); err != nil {
		t.Fatalf("lock hook: %v", err)
	}

	expired := make(chan error, 1)
	go func() {
		_, err := f.svc.ClaimAndMatch(ctx, 10)
		expired <- err
	}()
	select {
	case err := <-expired:
		if err != nil {
			t.Fatalf("claim and match with an expired lease: %v", err)
		}
	case <-time.After(1 * time.Second):
		lockTx.Rollback(ctx)
		lockConn.Release()
		t.Fatal("expired-lease worker blocked on the hook row — it began deciding before asserting ownership")
	}
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release hook lock: %v", err)
	}
	lockConn.Release()

	if r := f.execFor(t, hookID); r.count != 0 {
		t.Errorf("expired-lease worker wrote %d execution(s), want 0", r.count)
	}
	if _, found := f.latchFor(t, hookID); found {
		t.Error("expired-lease worker advanced the rising-edge latch")
	}
	if s := f.eventState(t, event.ID); s.status == "dispatched" || s.hasDispatchedAt {
		t.Errorf("expired-lease worker finalized the event: %+v", s)
	}

	// Losing ownership now also backs the event off (see
	// TestMatcherExpiredLeaseEventDoesNotStarveQueue), so it is not immediately
	// claimable. Advance past that backoff to reach the retry.
	if _, err := f.pool.Exec(ctx,
		`UPDATE domain_event SET available_at = now() WHERE id = $1`,
		util.UUIDToString(event.ID)); err != nil {
		t.Fatalf("clear backoff: %v", err)
	}

	// With a live lease the same event decides normally.
	MatcherLeaseTTL = original
	if _, err := f.svc.ClaimAndMatch(ctx, 10); err != nil {
		t.Fatalf("claim and match: %v", err)
	}
	if r := f.execFor(t, hookID); r.count != 1 || r.status != hookExecQueued {
		t.Errorf("live-lease decision: count=%d status=%q, want 1 queued", r.count, r.status)
	}
	if s := f.eventState(t, event.ID); s.status != "dispatched" || !s.hasDispatchedAt {
		t.Errorf("event state = %+v, want dispatched with dispatched_at set", s)
	}
}

// The finalize CAS carries the same not-expired condition as the entry assertion, so
// a lease that elapses mid-decision cannot commit the decision it already wrote.
func TestMatcherFinalizeRejectsExpiredLease(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	event := f.seedEvent(t, "done", 0)
	lease := f.claimWithLease(t, event.ID)

	// The lease elapses while the decision is in flight.
	if _, err := f.pool.Exec(ctx,
		`UPDATE domain_event SET lease_expires_at = now() - interval '1 second' WHERE id = $1`,
		util.UUIDToString(event.ID)); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	rows, err := f.svc.Queries.MarkDomainEventDispatched(ctx, db.MarkDomainEventDispatchedParams{
		ID: event.ID, LeaseToken: lease,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if rows != 0 {
		t.Errorf("finalize affected %d row(s) for an expired lease, want 0", rows)
	}
	if s := f.eventState(t, event.ID); s.status == "dispatched" || s.hasDispatchedAt {
		t.Errorf("expired lease finalized the event: %+v", s)
	}
}

// sleepBeforeExecutionInsert makes every hook_execution INSERT for one hook pause,
// so a test can make a decision reliably outlive its lease TTL.
func (f matcherFixture) sleepBeforeExecutionInsert(t *testing.T, hookID string, seconds float64) {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("hook_exec_sleep_%d", time.Now().UnixNano())
	fn := name + "_fn"
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(%f); RETURN NEW; END; $$;`, quoteIdent(fn), seconds)); err != nil {
		t.Fatalf("create execution sleep function: %v", err)
	}
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s BEFORE INSERT ON hook_execution
		FOR EACH ROW WHEN (NEW.hook_id = %s::uuid)
		EXECUTE FUNCTION %s();`, quoteIdent(name), quoteLiteral(hookID), quoteIdent(fn))); err != nil {
		t.Fatalf("create execution sleep trigger: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		f.pool.Exec(bg, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON hook_execution", quoteIdent(name)))
		f.pool.Exec(bg, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", quoteIdent(fn)))
	})
}

// availableInFuture reports whether an event has been backed off past now.
func (f matcherFixture) availableInFuture(t *testing.T, eventID pgtype.UUID) bool {
	t.Helper()
	var future bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT available_at > now() FROM domain_event WHERE id = $1`,
		util.UUIDToString(eventID)).Scan(&future); err != nil {
		t.Fatalf("read available_at: %v", err)
	}
	return future
}

// An event whose OWN fresh lease expires mid-decision must be backed off like any
// other failed decision, so the queue moves past it.
//
// Zero writes alone is not enough. Claim and decision share one transaction and the
// event row stays locked throughout, so in production this branch is essentially
// always "the lease we just took expired while we were deciding". Rolling back also
// undoes the claim and restores the original available_at, so a decision that
// reliably outlives its TTL would be re-selected as the oldest event on every tick
// and starve everything behind it forever. This puts such an event at the head of
// the queue with a healthy event behind it and asserts the healthy one still runs.
func TestMatcherExpiredLeaseEventDoesNotStarveQueue(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.drainMatcherQueue(t)

	// The head event matches this hook, and writing its execution always takes
	// longer than the lease TTL below — so ownership is lost every single attempt.
	stuckHook := f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent)
	f.sleepBeforeExecutionInsert(t, stuckHook, 0.7)

	head := f.seedEvent(t, "done", 0)   // matches stuckHook → slow execution insert
	behind := f.seedEvent(t, "todo", 0) // no candidate matches → decides instantly
	if head.Seq >= behind.Seq {
		t.Fatalf("head event must be older: head seq %d, behind seq %d", head.Seq, behind.Seq)
	}

	original := MatcherLeaseTTL
	t.Cleanup(func() { MatcherLeaseTTL = original })
	MatcherLeaseTTL = 300 * time.Millisecond

	// Two ticks, mirroring a running matcher.
	for round := 1; round <= 2; round++ {
		if _, err := f.svc.ClaimAndMatch(ctx, 10); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}

	// The stuck event stayed safe: no partial decision survived.
	if r := f.execFor(t, stuckHook); r.count != 0 {
		t.Errorf("expired-lease event wrote %d execution(s), want 0", r.count)
	}
	if s := f.eventState(t, head.ID); s.status == "dispatched" || s.hasDispatchedAt {
		t.Errorf("expired-lease event was finalized: %+v", s)
	}

	// ...and it was backed off rather than left holding the head of the queue.
	if !f.availableInFuture(t, head.ID) {
		t.Error("expired-lease event was not backed off — it stays the oldest claimable " +
			"event and will be re-selected on every tick")
	}

	// The decisive assertion: the queue moved past it.
	if s := f.eventState(t, behind.ID); s.status != "dispatched" || !s.hasDispatchedAt {
		t.Errorf("event behind the stuck one = %+v, want dispatched — one event whose lease "+
			"keeps expiring is starving the whole queue", s)
	}
}
