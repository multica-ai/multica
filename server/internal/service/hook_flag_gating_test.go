package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// SHADOW MUST NEVER BECOME A RETROACTIVE EXECUTION.
//
// The executor's claim query selects any `queued` execution regardless of age, so if
// the matcher recorded shadow matches as `queued`, an entire observation period would
// fire the instant execution was switched on. That inverts the product promise of
// "watch it first, then enable it safely": the operator's evidence for enabling is
// itself the payload that detonates.
//
// This drives the real matcher with execution OFF, then flips execution ON and runs
// the real executor, asserting the shadow decision is terminal and the target issue
// is never touched.
func TestShadowMatchIsNeverExecutedAfterEnablingExecution(t *testing.T) {
	f := newMatcherFixture(t)

	// hooks ON, execution OFF: the shadow configuration an operator would run first.
	f.svc.Flags = hookFlags(true, false)

	f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, "per_event")
	f.setIssueStatus(t, "todo")
	ev := f.seedEvent(t, "done", 0)
	drainEventThroughMatcher(t, f, ev.ID)

	status, reason := singleExecutionStatus(t, f)
	if status != "skipped" || reason != "execution_disabled" {
		t.Fatalf("shadow decision = (%q, %q), want (skipped, execution_disabled) — a queued row would be claimed the moment execution is enabled", status, reason)
	}

	// The operator is now satisfied and enables execution. The historical shadow
	// record must stay inert.
	f.svc.Flags = hookFlags(true, true)
	drainExecutor(t, f)

	if s := issueStatusForTest(t, f.pool, f.issueID); s != "todo" {
		t.Errorf("issue status = %q, want todo — a decision observed during shadow was executed retroactively", s)
	}
	if status, _ := singleExecutionStatus(t, f); status != "skipped" {
		t.Errorf("execution status = %q, want it to stay skipped", status)
	}
}

// A rising_edge hook must not have its edge consumed while nothing can execute.
// Advancing the latch in shadow would leave `prev = true` when execution is enabled,
// so the very first live event is skipped as condition_already_true and the hook
// silently never fires until the condition happens to fall and rise again.
func TestShadowDoesNotConsumeTheRisingEdge(t *testing.T) {
	f := newMatcherFixture(t)

	f.svc.Flags = hookFlags(true, false) // shadow
	hookID := f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, "rising_edge")
	f.setIssueStatus(t, "todo")
	shadowEv := f.seedEvent(t, "done", 0)
	drainEventThroughMatcher(t, f, shadowEv.ID)
	if n := latchRowCount(t, f, hookID); n != 0 {
		t.Fatalf("shadow wrote %d latch row(s), want 0 — the rising edge was consumed while nothing could execute", n)
	}

	// Execution enabled; a fresh event on the same still-true condition must fire.
	f.svc.Flags = hookFlags(true, true)
	liveEv := f.seedEvent(t, "done", 0)
	drainEventThroughMatcher(t, f, liveEv.ID)

	if !hasExecutionWithStatus(t, f, "queued") {
		t.Error("no queued execution after enabling — the shadow period consumed the rising edge, so the hook can never fire")
	}
}

// WORKSPACE ROLLOUT MUST BE A REAL BOUNDARY.
//
// Both switches are evaluated per workspace, so a canary that allows only workspace A
// must leave workspace B completely untouched: B is neither matched nor executed, and
// A keeps working while B is disabled. The gate used to be read once per tick from the
// process root context, which carries no workspace — so an allow_by: workspace_id rule
// matched nothing and only a global override could turn the engine on, for everyone.
func TestWorkspaceRolloutMatchesOnlyTheAllowedWorkspace(t *testing.T) {
	a := newMatcherFixture(t)
	b := newMatcherFixture(t) // an independent workspace, seeded the same way

	// Canary: only workspace A is allowed, exactly as a YAML allow_by rule would.
	flags := hookFlagsAllowingWorkspaces(a.ws)
	a.svc.Flags, b.svc.Flags = flags, flags

	events := map[string]pgtype.UUID{}
	for _, f := range []matcherFixture{a, b} {
		f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, "per_event")
		f.setIssueStatus(t, "todo")
		events[f.ws] = f.seedEvent(t, "done", 0).ID
	}

	// The queue is global, so both workspaces' events are candidates for the same
	// matcher. Drain both so neither assertion can pass just by not being reached.
	drainEventThroughMatcher(t, a, events[a.ws])
	drainEventThroughMatcher(t, b, events[b.ws])

	if !hasExecutionWithStatus(t, a, "queued") {
		t.Error("workspace A produced no queued execution — the allowed workspace must still be matched")
	}
	if n := executionCount(t, b); n != 0 {
		t.Errorf("workspace B produced %d execution(s), want 0 — a workspace outside the canary must never be matched", n)
	}

	// Executing must likewise only touch A.
	drainExecutor(t, a)
	if s := issueStatusForTest(t, b.pool, b.issueID); s != "todo" {
		t.Errorf("workspace B issue = %q, want todo — a workspace outside the canary had its data changed", s)
	}
}

// A workspace outside the canary must not have its events left pending forever: the
// candidate window is ordered by seq, so a disabled workspace's backlog would sit at
// the head and starve the workspace actually under canary. They are dispatched with no
// decisions instead — which also means enabling that workspace later starts from
// "now" rather than replaying its whole history.
func TestDisabledWorkspaceEventsAreDrainedWithoutDecisions(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()

	f.svc.Flags = hookFlags(false, false) // hooks off for this workspace
	f.seedHook(t, "issue.status_changed", `{"to":"done"}`, `[]`, "per_event")
	f.setIssueStatus(t, "todo")
	ev := f.seedEvent(t, "done", 0)
	drainEventThroughMatcher(t, f, ev.ID)

	if n := executionCount(t, f); n != 0 {
		t.Errorf("disabled workspace produced %d execution(s), want 0", n)
	}
	var dispatchStatus string
	if err := f.pool.QueryRow(ctx,
		`SELECT dispatch_status FROM domain_event WHERE id = $1`, ev.ID).Scan(&dispatchStatus); err != nil {
		t.Fatalf("load event: %v", err)
	}
	if dispatchStatus != "dispatched" {
		t.Errorf("event dispatch_status = %q, want dispatched — a disabled workspace's backlog would otherwise head-of-line the candidate window and replay on enable", dispatchStatus)
	}

	// Enabling hooks afterwards must NOT resurrect the drained event.
	f.svc.Flags = hookFlags(true, true)
	if _, err := f.svc.ClaimAndMatch(ctx, 20); err != nil {
		t.Fatalf("match after enabling: %v", err)
	}
	if n := executionCount(t, f); n != 0 {
		t.Errorf("enabling hooks replayed %d historical event(s), want 0", n)
	}
}

// --- helpers -------------------------------------------------------------

// The matcher and executor both drain GLOBAL queues, so a fixed batch size can be
// consumed entirely by a neighbouring test's rows before it ever reaches ours. These
// helpers drive the real entry points repeatedly until THIS test's row has actually
// been processed, which makes the assertions independent of what else is in the queue.

func drainEventThroughMatcher(t *testing.T, f matcherFixture, eventID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		var status string
		if err := f.pool.QueryRow(ctx,
			`SELECT dispatch_status FROM domain_event WHERE id = $1`, eventID).Scan(&status); err != nil {
			t.Fatalf("load event: %v", err)
		}
		if status != "pending" {
			return
		}
		if _, err := f.svc.ClaimAndMatch(ctx, 20); err != nil {
			t.Fatalf("matcher tick: %v", err)
		}
	}
	t.Fatal("event was still pending after 50 matcher ticks")
}

// drainExecutor runs executor ticks until this workspace has no claimable execution
// left, so the assertion cannot pass merely because the batch never reached us.
func drainExecutor(t *testing.T, f matcherFixture) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		var n int
		if err := f.pool.QueryRow(ctx,
			`SELECT count(*) FROM hook_execution
			 WHERE workspace_id = $1 AND status IN ('queued', 'running')`, f.ws).Scan(&n); err != nil {
			t.Fatalf("count claimable: %v", err)
		}
		if n == 0 {
			return
		}
		if _, err := f.svc.ClaimAndRun(ctx, 20); err != nil {
			t.Fatalf("executor tick: %v", err)
		}
	}
}

func singleExecutionStatus(t *testing.T, f matcherFixture) (status, reason string) {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT status, COALESCE(skip_reason, '') FROM hook_execution WHERE workspace_id = $1`, f.ws)
	if err != nil {
		t.Fatalf("load executions: %v", err)
	}
	defer rows.Close()
	var n int
	for rows.Next() {
		if err := rows.Scan(&status, &reason); err != nil {
			t.Fatalf("scan execution: %v", err)
		}
		n++
	}
	if n != 1 {
		t.Fatalf("found %d executions, want exactly 1", n)
	}
	return status, reason
}

func executionCount(t *testing.T, f matcherFixture) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM hook_execution WHERE workspace_id = $1`, f.ws).Scan(&n); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	return n
}

func hasExecutionWithStatus(t *testing.T, f matcherFixture, status string) bool {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM hook_execution WHERE workspace_id = $1 AND status = $2`, f.ws, status).Scan(&n); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	return n > 0
}

func latchRowCount(t *testing.T, f matcherFixture, hookID string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM automation_state WHERE workspace_id = $1 AND state_key = $2`, f.ws, hookID).Scan(&n); err != nil {
		t.Fatalf("count latch rows: %v", err)
	}
	return n
}
