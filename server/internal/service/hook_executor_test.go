package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// seedQueuedExecution creates the hook + revision + source event + `queued`
// execution the matcher would have produced, so the executor has something to claim.
func (f matcherFixture) seedQueuedExecution(t *testing.T, actionsJSON string) (hookID, execID string, event db.DomainEvent) {
	t.Helper()
	ctx := context.Background()
	hookID, revID := uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook (id, workspace_id, name, enabled, active_revision_id, scope_type, origin,
			creator_actor_type, creator_actor_id, authorization_principal_user_id)
		VALUES ($1, $2, 'x hook', true, $3, 'workspace', 'user', 'member', $4, $4)`,
		hookID, f.ws, revID, f.userID); err != nil {
		t.Fatalf("seed hook: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook_revision (id, hook_id, revision, event_type, match, conditions, fire_mode, actions, created_by_type, created_by_id)
		VALUES ($1, $2, 1, 'issue.status_changed', '{}'::jsonb, '[]'::jsonb, $3, $4::jsonb, 'member', $5)`,
		revID, hookID, automation.FirePerEvent, actionsJSON, f.userID); err != nil {
		t.Fatalf("seed revision: %v", err)
	}
	event = f.seedEvent(t, "done", 0)
	execID = uuid.NewString()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO hook_execution (id, workspace_id, hook_id, hook_revision_id, event_id, correlation_id, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'queued')`,
		execID, f.ws, hookID, revID, util.UUIDToString(event.ID), util.UUIDToString(event.CorrelationID)); err != nil {
		t.Fatalf("seed execution: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		f.pool.Exec(bg, `DELETE FROM hook_action_effect WHERE execution_id = $1`, execID)
		f.pool.Exec(bg, `DELETE FROM hook_execution WHERE hook_id = $1`, hookID)
		f.pool.Exec(bg, `DELETE FROM hook_revision WHERE hook_id = $1`, hookID)
		f.pool.Exec(bg, `DELETE FROM hook WHERE id = $1`, hookID)
	})
	return hookID, execID, event
}

type execState struct {
	status      string
	skipReason  string
	errorCode   string
	actionIndex int
	attempts    int
	lease       string
	retryQueued bool
}

func (f matcherFixture) execState(t *testing.T, execID string) execState {
	t.Helper()
	var s execState
	if err := f.pool.QueryRow(context.Background(), `
		SELECT status, COALESCE(skip_reason,''), COALESCE(error_code,''), current_action_index, attempts,
		       COALESCE(lease_token::text,''), COALESCE(next_attempt_at > now(), false)
		FROM hook_execution WHERE id = $1`, execID).
		Scan(&s.status, &s.skipReason, &s.errorCode, &s.actionIndex, &s.attempts, &s.lease, &s.retryQueued); err != nil {
		t.Fatalf("load execution state: %v", err)
	}
	return s
}

func (f matcherFixture) issueStatus(t *testing.T) string {
	t.Helper()
	var status string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status FROM issue WHERE id = $1`, f.issueID).Scan(&status); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	return status
}

func (f matcherFixture) effectCount(t *testing.T, execID string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM hook_action_effect WHERE execution_id = $1`, execID).Scan(&n); err != nil {
		t.Fatalf("count effects: %v", err)
	}
	return n
}

// effectRows returns each action's durable effect as (action_index, status).
func (f matcherFixture) effectRows(t *testing.T, execID string) map[int]string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT action_index, status FROM hook_action_effect WHERE execution_id = $1`, execID)
	if err != nil {
		t.Fatalf("load effects: %v", err)
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var idx int
		var status string
		if err := rows.Scan(&idx, &status); err != nil {
			t.Fatalf("scan effect: %v", err)
		}
		out[idx] = status
	}
	return out
}

func (f matcherFixture) setIssueStatusAction(status string) string {
	return `[{"type":"set_issue_status","issue_id":"` + f.issueID + `","status":"` + status + `"}]`
}

// The happy path: the action writes the target, emits the causal event, records its
// effect, and the execution reaches succeeded.
func TestExecutorRunsSetIssueStatus(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")
	hookID, execID, event := f.seedQueuedExecution(t, f.setIssueStatusAction("done"))

	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("claim and run: %v", err)
	}

	if got := f.issueStatus(t); got != "done" {
		t.Errorf("issue status = %q, want done", got)
	}
	s := f.execState(t, execID)
	if s.status != hookExecSucceeded {
		t.Errorf("execution status = %q (skip=%q err=%q), want succeeded", s.status, s.skipReason, s.errorCode)
	}
	if s.actionIndex != 1 {
		t.Errorf("action cursor = %d, want 1", s.actionIndex)
	}
	if s.lease != "" {
		t.Errorf("terminal execution still holds lease %s", s.lease)
	}
	if n := f.effectCount(t, execID); n != 1 {
		t.Errorf("effect rows = %d, want 1", n)
	}

	// The emitted event stays in the originating chain, records what caused it, and
	// sits one hop deeper so the depth guard can see the chain grow.
	var hop int
	var corr, causeExec string
	var causeIdx int
	if err := f.pool.QueryRow(ctx, `
		SELECT hop_count, correlation_id::text, causation_execution_id::text, causation_action_index
		FROM domain_event
		WHERE causation_execution_id = $1`, execID).Scan(&hop, &corr, &causeExec, &causeIdx); err != nil {
		t.Fatalf("load emitted event: %v", err)
	}
	if hop != int(event.HopCount)+1 {
		t.Errorf("emitted hop_count = %d, want %d", hop, event.HopCount+1)
	}
	if corr != util.UUIDToString(event.CorrelationID) {
		t.Errorf("emitted correlation = %s, want inherited %s", corr, util.UUIDToString(event.CorrelationID))
	}
	if causeExec != execID || causeIdx != 0 {
		t.Errorf("causation = (%s, %d), want (%s, 0)", causeExec, causeIdx, execID)
	}
	_ = hookID
}

// Re-running an execution whose action already committed must not repeat the action.
// This is the crash window "action succeeded but the execution cursor was not yet
// updated": the effect row is the durable anchor that closes it.
func TestExecutorEffectKeyMakesActionIdempotent(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")
	_, execID, _ := f.seedQueuedExecution(t, f.setIssueStatusAction("done"))

	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if n := f.effectCount(t, execID); n != 1 {
		t.Fatalf("effect rows after first run = %d, want 1", n)
	}

	// Simulate the crash window: the action committed, but the execution was left
	// running with its cursor rewound, exactly as a killed worker would leave it.
	if _, err := f.pool.Exec(ctx, `
		UPDATE hook_execution
		SET status = 'queued', current_action_index = 0, completed_at = NULL,
		    lease_token = NULL, lease_expires_at = NULL
		WHERE id = $1`, execID); err != nil {
		t.Fatalf("rewind execution: %v", err)
	}
	// Move the issue away so a repeated action would be visible.
	f.setIssueStatus(t, "todo")

	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if got := f.issueStatus(t); got != "todo" {
		t.Errorf("issue status = %q, want todo — the action ran a second time instead of "+
			"being skipped by its succeeded effect", got)
	}
	if n := f.effectCount(t, execID); n != 1 {
		t.Errorf("effect rows = %d, want 1 (the anchor must not be duplicated)", n)
	}
	if s := f.execState(t, execID); s.status != hookExecSucceeded {
		t.Errorf("execution status = %q, want succeeded", s.status)
	}
	// Exactly one causal event, from the single real execution of the action.
	var events int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM domain_event WHERE causation_execution_id = $1`, execID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("emitted %d causal events, want exactly 1", events)
	}
}

// A worker whose lease has expired must not write the target, the effect, or any
// terminal status (§7.3).
func TestExecutorExpiredLeaseWritesNothing(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")
	_, execID, _ := f.seedQueuedExecution(t, f.setIssueStatusAction("done"))

	original := ExecutorLeaseTTL
	t.Cleanup(func() { ExecutorLeaseTTL = original })

	// Every lease this tick acquires is already expired when granted. Ownership must
	// be asserted BEFORE any action work, not merely discovered at the closing CAS:
	// hold the target row, and a worker that had already started acting would block
	// here. Returning promptly is the observable proof that it failed closed up front.
	// This ordering is what will keep a future non-transactional action (an agent
	// enqueue) from doing work a rollback cannot undo.
	ExecutorLeaseTTL = -1 * time.Minute
	lockConn, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock conn: %v", err)
	}
	lockTx, err := lockConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	if _, err := lockTx.Exec(ctx, `SELECT id FROM issue WHERE id = $1 FOR UPDATE`, f.issueID); err != nil {
		t.Fatalf("lock issue: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := f.svc.ClaimAndRun(ctx, 5)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run with an expired lease: %v", err)
		}
	case <-time.After(1 * time.Second):
		lockTx.Rollback(ctx)
		lockConn.Release()
		t.Fatal("expired-lease worker blocked on the target row — it began acting before asserting ownership")
	}
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release issue lock: %v", err)
	}
	lockConn.Release()

	if got := f.issueStatus(t); got != "todo" {
		t.Errorf("issue status = %q, want todo — an expired-lease worker wrote the target", got)
	}
	if n := f.effectCount(t, execID); n != 0 {
		t.Errorf("expired-lease worker wrote %d effect(s), want 0", n)
	}
	s := f.execState(t, execID)
	if s.status == hookExecSucceeded || s.status == hookExecFailed || s.status == hookExecSkipped {
		t.Errorf("expired-lease worker wrote terminal status %q", s.status)
	}

	// Losing the lease now also backs the execution off (see
	// TestExecutorExpiredLeaseDoesNotStarveQueue), so it is not immediately
	// claimable. Advance past that backoff to reach the retry.
	if _, err := f.pool.Exec(ctx,
		`UPDATE hook_execution SET next_attempt_at = now() WHERE id = $1`, execID); err != nil {
		t.Fatalf("clear backoff: %v", err)
	}

	// With a live lease the same execution runs normally.
	ExecutorLeaseTTL = original
	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("run with a live lease: %v", err)
	}
	if got := f.issueStatus(t); got != "done" {
		t.Errorf("issue status = %q, want done after a valid retry", got)
	}
	if s := f.execState(t, execID); s.status != hookExecSucceeded {
		t.Errorf("execution status = %q, want succeeded", s.status)
	}
}

// A departed authorization principal is terminal, not retried, and pauses the hook so
// it stops producing work under authority nobody holds any more (§7.3).
func TestExecutorDepartedPrincipalSkipsAndPausesHook(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")
	hookID, execID, _ := f.seedQueuedExecution(t, f.setIssueStatusAction("done"))

	// The principal leaves the workspace between matching and execution.
	if _, err := f.pool.Exec(ctx,
		`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, f.ws, f.userID); err != nil {
		t.Fatalf("remove principal: %v", err)
	}

	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("claim and run: %v", err)
	}

	if got := f.issueStatus(t); got != "todo" {
		t.Errorf("issue status = %q, want todo — the action ran under a departed principal", got)
	}
	s := f.execState(t, execID)
	if s.status != hookExecSkipped || s.skipReason != skipPrincipalInvalid {
		t.Errorf("execution = (%q, %q), want skipped/%s", s.status, s.skipReason, skipPrincipalInvalid)
	}
	if s.retryQueued {
		t.Error("a departed principal must be terminal, not scheduled for retry")
	}

	var enabled bool
	var reason string
	if err := f.pool.QueryRow(ctx,
		`SELECT enabled, COALESCE(disabled_reason,'') FROM hook WHERE id = $1`, hookID).Scan(&enabled, &reason); err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Error("hook was left enabled under a departed principal")
	}
	if reason != hookDisabledPrincipalInvalid {
		t.Errorf("disabled_reason = %q, want %s", reason, hookDisabledPrincipalInvalid)
	}
}

// An action naming a target outside the workspace is terminal, not retried, and the
// tenant boundary holds.
func TestExecutorForeignTargetIsTerminalSkip(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	foreign := uuid.NewString()
	actions := `[{"type":"set_issue_status","issue_id":"` + foreign + `","status":"done"}]`
	_, execID, _ := f.seedQueuedExecution(t, actions)

	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("claim and run: %v", err)
	}

	s := f.execState(t, execID)
	if s.status != hookExecSkipped || s.skipReason != skipTargetUnavailable {
		t.Errorf("execution = (%q, %q), want skipped/%s", s.status, s.skipReason, skipTargetUnavailable)
	}
	if s.retryQueued {
		t.Error("an unavailable target must be terminal, not scheduled for retry")
	}
	// A terminal action still leaves its durable audit row, carrying the reason —
	// otherwise skipped actions are invisible in the effect trace.
	effects := f.effectRows(t, execID)
	if len(effects) != 1 || effects[0] != hookExecSkipped {
		t.Errorf("effects = %v, want exactly action 0 recorded as skipped", effects)
	}
	var code, resolved string
	if err := f.pool.QueryRow(ctx, `
		SELECT COALESCE(error_code,''), COALESCE(resolved_input::text,'')
		FROM hook_action_effect WHERE execution_id = $1`, execID).Scan(&code, &resolved); err != nil {
		t.Fatal(err)
	}
	if code != skipTargetUnavailable {
		t.Errorf("effect error_code = %q, want %s", code, skipTargetUnavailable)
	}
	if resolved == "" {
		t.Error("terminal effect recorded no resolved_input")
	}
}

// A partial execution is explicit: action 1 committed, action 2 fails terminally, and
// action 1 is NOT rolled back (§7.2).
func TestExecutorPartialExecutionKeepsCommittedAction(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")
	foreign := uuid.NewString()
	actions := `[{"type":"set_issue_status","issue_id":"` + f.issueID + `","status":"done"},` +
		`{"type":"set_issue_status","issue_id":"` + foreign + `","status":"done"}]`
	_, execID, _ := f.seedQueuedExecution(t, actions)

	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("claim and run: %v", err)
	}

	if got := f.issueStatus(t); got != "done" {
		t.Errorf("issue status = %q, want done — action 1 must not be rolled back when action 2 fails", got)
	}
	s := f.execState(t, execID)
	if s.status != hookExecSkipped || s.skipReason != skipTargetUnavailable {
		t.Errorf("execution = (%q, %q), want skipped/%s", s.status, s.skipReason, skipTargetUnavailable)
	}
	if s.actionIndex != 1 {
		t.Errorf("action cursor = %d, want 1 (action 0 committed, action 1 failed)", s.actionIndex)
	}
	// The partial trace is complete: the committed action AND the one that ended it.
	effects := f.effectRows(t, execID)
	if len(effects) != 2 || effects[0] != hookExecSucceeded || effects[1] != hookExecSkipped {
		t.Errorf("effects = %v, want action 0 succeeded and action 1 skipped — a partial "+
			"execution must show the action that failed, not only the ones that worked", effects)
	}
}

// The infrastructure retry ladder: a transient failure re-queues with backoff and
// resumes at the same action, and the execution only fails once the ladder is spent.
func TestExecutorInfraFailureBacksOffThenFails(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")
	_, execID, _ := f.seedQueuedExecution(t, f.setIssueStatusAction("done"))

	// Make the target write fail for real, the way an infrastructure fault would.
	trigger := "exec_fail_" + util.UUIDToString(util.NewUUID())[:8]
	fn := trigger + "_fn"
	if _, err := f.pool.Exec(ctx, `
		CREATE FUNCTION `+quoteIdent(fn)+`() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected infrastructure failure'; END; $$;`); err != nil {
		t.Fatalf("create failure function: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		CREATE TRIGGER `+quoteIdent(trigger)+` BEFORE UPDATE OF status ON issue
		FOR EACH ROW WHEN (NEW.id = `+quoteLiteral(f.issueID)+`::uuid)
		EXECUTE FUNCTION `+quoteIdent(fn)+`();`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	dropTrigger := func() {
		f.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+quoteIdent(trigger)+` ON issue`)
		f.pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS `+quoteIdent(fn)+`()`)
	}
	t.Cleanup(dropTrigger)

	// Each attempt: claim, fail, back off. Clear the backoff to reach the next rung.
	for attempt := 1; attempt <= len(executorBackoff); attempt++ {
		if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		s := f.execState(t, execID)
		if s.status != "queued" {
			t.Fatalf("attempt %d: status = %q (skip=%q err=%q), want queued for retry",
				attempt, s.status, s.skipReason, s.errorCode)
		}
		if !s.retryQueued {
			t.Errorf("attempt %d: next_attempt_at was not moved into the future", attempt)
		}
		if s.actionIndex != 0 {
			t.Errorf("attempt %d: action cursor = %d, want 0 (retry resumes at the failed action)", attempt, s.actionIndex)
		}
		if s.attempts != attempt {
			t.Errorf("attempt %d: attempts = %d", attempt, s.attempts)
		}
		if _, err := f.pool.Exec(ctx,
			`UPDATE hook_execution SET next_attempt_at = now() WHERE id = $1`, execID); err != nil {
			t.Fatalf("clear backoff: %v", err)
		}
	}

	// The ladder is spent: the next attempt is terminal.
	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("final attempt: %v", err)
	}
	s := f.execState(t, execID)
	if s.status != hookExecFailed {
		t.Errorf("status = %q, want failed once the retry ladder is exhausted", s.status)
	}
	if s.errorCode != errCodeInfra {
		t.Errorf("error_code = %q, want %s", s.errorCode, errCodeInfra)
	}

	// And the target was never written by any of those attempts.
	dropTrigger()
	if got := f.issueStatus(t); got != "todo" {
		t.Errorf("issue status = %q, want todo — a failing action must not partially apply", got)
	}
}

var _ = pgtype.UUID{}

// ---------------------------------------------------------------------------
// Regressions for the executor review round: independent execution gate, lease
// forward progress, principal-invalid fail-closed, terminal effect audit.
// ---------------------------------------------------------------------------

// Blocker 1 — execution has its OWN default-off switch, so enabling the engine for
// shadow evaluation cannot start performing real side effects.
func TestExecutionGateIsIndependentOfEventHooksFlag(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hooks     bool
		execution bool
		want      bool
	}{
		{"both off", false, false, false},
		{"hooks on, execution off — shadow mode", true, false, false},
		{"execution on without the engine", false, true, false},
		{"both on", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := featureflag.NewStaticProvider()
			provider.Set(featureflags.EventHooks, featureflag.Rule{Default: tc.hooks})
			provider.Set(featureflags.EventHookExecution, featureflag.Rule{Default: tc.execution})
			flags := featureflag.NewService(provider)
			if got := featureflags.EventHookExecutionEnabled(context.Background(), flags); got != tc.want {
				t.Errorf("EventHookExecutionEnabled = %v, want %v", got, tc.want)
			}
		})
	}
}

// Blocker 2 — an execution whose lease keeps elapsing must not hold the head of the
// queue. Elon's repro: the oldest execution reliably outlives its TTL, a healthy one
// sits behind it, and a bounded ClaimAndRun must still reach the healthy one.
func TestExecutorExpiredLeaseDoesNotStarveQueue(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")

	// The head execution's action always takes longer than the lease below.
	_, stuckExec, _ := f.seedQueuedExecution(t, f.setIssueStatusAction("done"))
	f.sleepBeforeEffectInsert(t, stuckExec, 0.4)

	// A healthy execution queued behind it: no actions, so it completes instantly.
	_, healthyExec, _ := f.seedQueuedExecution(t, `[]`)
	if _, err := f.pool.Exec(ctx,
		`UPDATE hook_execution SET created_at = now() + interval '1 second' WHERE id = $1`, healthyExec); err != nil {
		t.Fatalf("order executions: %v", err)
	}

	original := ExecutorLeaseTTL
	t.Cleanup(func() { ExecutorLeaseTTL = original })
	ExecutorLeaseTTL = 100 * time.Millisecond

	// Two slots, exactly as the reviewer drove it: without a backoff the stuck row
	// consumes both and the healthy one is never reached.
	if _, err := f.svc.ClaimAndRun(ctx, 2); err != nil {
		t.Fatalf("claim and run: %v", err)
	}

	stuck := f.execState(t, stuckExec)
	if stuck.status == hookExecSucceeded {
		t.Fatalf("the stuck execution unexpectedly succeeded; the lease did not expire")
	}
	if !stuck.retryQueued {
		t.Error("the expired-lease execution was not backed off — it stays the oldest " +
			"claimable row and will be re-selected on every tick")
	}
	if got := f.issueStatus(t); got != "todo" {
		t.Errorf("issue status = %q, want todo — an expired-lease worker committed its action", got)
	}
	if s := f.execState(t, healthyExec); s.status != hookExecSucceeded {
		t.Errorf("execution behind the stuck one = %q, want succeeded — one execution whose "+
			"lease keeps expiring is starving the whole queue", s.status)
	}
}

// Blocker 3 — if the hook cannot be paused, the skip must NOT be committed. Recording
// the execution as handled while the rule stays enabled is fail-open: it would keep
// producing executions under authority nobody holds.
func TestExecutorPrincipalInvalidFailsClosedWhenPauseFails(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	f.setIssueStatus(t, "todo")
	hookID, execID, _ := f.seedQueuedExecution(t, f.setIssueStatusAction("done"))

	if _, err := f.pool.Exec(ctx,
		`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, f.ws, f.userID); err != nil {
		t.Fatalf("remove principal: %v", err)
	}

	// Make the pause write fail the way an infrastructure fault would.
	trigger := "pause_fail_" + util.UUIDToString(util.NewUUID())[:8]
	fn := trigger + "_fn"
	if _, err := f.pool.Exec(ctx, `
		CREATE FUNCTION `+quoteIdent(fn)+`() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected pause failure'; END; $$;`); err != nil {
		t.Fatalf("create pause-failure function: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		CREATE TRIGGER `+quoteIdent(trigger)+` BEFORE UPDATE OF enabled ON hook
		FOR EACH ROW WHEN (NEW.id = `+quoteLiteral(hookID)+`::uuid)
		EXECUTE FUNCTION `+quoteIdent(fn)+`();`); err != nil {
		t.Fatalf("create pause-failure trigger: %v", err)
	}
	dropTrigger := func() {
		f.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+quoteIdent(trigger)+` ON hook`)
		f.pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS `+quoteIdent(fn)+`()`)
	}
	t.Cleanup(dropTrigger)

	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("claim and run: %v", err)
	}

	// The hook is still enabled because the pause failed — so the execution must NOT
	// read as terminally handled.
	var enabled bool
	if err := f.pool.QueryRow(ctx, `SELECT enabled FROM hook WHERE id = $1`, hookID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("the pause unexpectedly succeeded; the failure injection did not take")
	}
	if s := f.execState(t, execID); s.status == hookExecSkipped {
		t.Error("execution was finalized as skipped while its hook stayed enabled — " +
			"fail-open: the rule keeps producing executions under a departed principal")
	}

	// Once the pause can be written, the outcome converges: hook paused, execution
	// terminally skipped, and the admins who can re-arm it are notified.
	dropTrigger()
	if _, err := f.pool.Exec(ctx,
		`UPDATE hook_execution SET next_attempt_at = now() WHERE id = $1`, execID); err != nil {
		t.Fatalf("clear backoff: %v", err)
	}
	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("second run: %v", err)
	}

	var reason string
	if err := f.pool.QueryRow(ctx,
		`SELECT enabled, COALESCE(disabled_reason,'') FROM hook WHERE id = $1`, hookID).Scan(&enabled, &reason); err != nil {
		t.Fatal(err)
	}
	if enabled || reason != hookDisabledPrincipalInvalid {
		t.Errorf("hook = (enabled=%v, reason=%q), want paused with %s", enabled, reason, hookDisabledPrincipalInvalid)
	}
	if s := f.execState(t, execID); s.status != hookExecSkipped || s.skipReason != skipPrincipalInvalid {
		t.Errorf("execution = (%q, %q), want skipped/%s", s.status, s.skipReason, skipPrincipalInvalid)
	}
	if got := f.issueStatus(t); got != "todo" {
		t.Errorf("issue status = %q, want todo — the action ran under a departed principal", got)
	}
}

// Blocker 4 — a terminal effect survives a restart as exactly ONE row per
// (execution, action index): re-running must update the audit row, never duplicate it.
func TestExecutorTerminalEffectIsSingleRowAcrossRetries(t *testing.T) {
	f := newMatcherFixture(t)
	ctx := context.Background()
	foreign := uuid.NewString()
	actions := `[{"type":"set_issue_status","issue_id":"` + foreign + `","status":"done"}]`
	_, execID, _ := f.seedQueuedExecution(t, actions)

	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if effects := f.effectRows(t, execID); len(effects) != 1 || effects[0] != hookExecSkipped {
		t.Fatalf("effects after first run = %v, want one skipped row", effects)
	}

	// Replay the same execution, as a restart that lost the terminal write would.
	if _, err := f.pool.Exec(ctx, `
		UPDATE hook_execution
		SET status = 'queued', skip_reason = NULL, completed_at = NULL,
		    lease_token = NULL, lease_expires_at = NULL, next_attempt_at = now()
		WHERE id = $1`, execID); err != nil {
		t.Fatalf("replay execution: %v", err)
	}
	if _, err := f.svc.ClaimAndRun(ctx, 5); err != nil {
		t.Fatalf("second run: %v", err)
	}

	effects := f.effectRows(t, execID)
	if len(effects) != 1 || effects[0] != hookExecSkipped {
		t.Errorf("effects after replay = %v, want still exactly one skipped row for "+
			"(execution, action 0)", effects)
	}
	var attempts int
	if err := f.pool.QueryRow(ctx,
		`SELECT attempts FROM hook_action_effect WHERE execution_id = $1`, execID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts < 2 {
		t.Errorf("effect attempts = %d, want the replay counted on the same row", attempts)
	}
}

// sleepBeforeEffectInsert makes one execution's action effect insert pause, so the
// action reliably outlives a short lease TTL.
func (f matcherFixture) sleepBeforeEffectInsert(t *testing.T, execID string, seconds float64) {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("hook_effect_sleep_%d", time.Now().UnixNano())
	fn := name + "_fn"
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(%f); RETURN NEW; END; $$;`, quoteIdent(fn), seconds)); err != nil {
		t.Fatalf("create effect sleep function: %v", err)
	}
	if _, err := f.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s BEFORE INSERT ON hook_action_effect
		FOR EACH ROW WHEN (NEW.execution_id = %s::uuid)
		EXECUTE FUNCTION %s();`, quoteIdent(name), quoteLiteral(execID), quoteIdent(fn))); err != nil {
		t.Fatalf("create effect sleep trigger: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		f.pool.Exec(bg, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON hook_action_effect", quoteIdent(name)))
		f.pool.Exec(bg, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", quoteIdent(fn)))
	})
}
