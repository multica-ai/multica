package service

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
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

type execRow struct {
	status     string
	skipReason string
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
		SELECT status, COALESCE(skip_reason,''), hook_revision_id::text, match_snapshot, condition_snapshot
		FROM hook_execution WHERE hook_id = $1 ORDER BY created_at DESC LIMIT 1`, hookID).
		Scan(&r.status, &r.skipReason, &r.revisionID, &r.matchSnap, &r.condSnap); err != nil {
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

func TestMatcherHopGuard(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	hookID := f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, automation.FirePerEvent)
	event := f.seedEvent(t, "done", maxHopCount) // at the depth limit

	if err := f.svc.MatchEvent(ctx, event); err != nil {
		t.Fatalf("match: %v", err)
	}
	if r := f.execFor(t, hookID); r.status != hookExecSkipped || r.skipReason != skipHopExceeded {
		t.Fatalf("expected skipped hop_exceeded, got %+v", r)
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
	assertLast(hookExecSkipped, skipEdgeNotRising)

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
	if r := f.execFor(t, hookID); r.status != hookExecSkipped || r.skipReason != skipEdgeNotRising {
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
