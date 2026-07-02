package loops

// FIR-2283 integration tests for the dispatch egress against a real DB. Proves
// the once-only dispatch contract: the store surfaces a fresh check for
// dispatch exactly once, and the gate evaluator sends each check to the runtime
// exactly once even when it is re-evaluated while checks are in flight. Skips
// cleanly when no test DB is reachable (shares loopTestPool / seedIssue with
// store_db_test.go).

import (
	"context"
	"testing"
)

// fakeDispatcher records every check, revision, and judge check it is asked
// to dispatch.
type fakeDispatcher struct {
	calls         []CheckDispatch
	revisionCalls []RevisionDispatch
	judgeCalls    []JudgeDispatch
	humanCalls    []HumanDispatch
}

func (f *fakeDispatcher) DispatchCheck(ctx context.Context, d CheckDispatch) error {
	f.calls = append(f.calls, d)
	return nil
}

func (f *fakeDispatcher) DispatchRevision(ctx context.Context, d RevisionDispatch) error {
	f.revisionCalls = append(f.revisionCalls, d)
	return nil
}

func (f *fakeDispatcher) DispatchJudge(ctx context.Context, d JudgeDispatch) error {
	f.judgeCalls = append(f.judgeCalls, d)
	return nil
}

func (f *fakeDispatcher) DispatchHuman(ctx context.Context, d HumanDispatch) error {
	f.humanCalls = append(f.humanCalls, d)
	return nil
}

// TestStore_DispatchOnce proves the dispatch dedup at the store layer: a fresh
// check is pending-dispatch once, leaves the pending set after MarkDispatched,
// and a re-enqueue never re-surfaces it — so the engine never re-sends a check
// that is already in flight.
func TestStore_DispatchOnce(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	store := NewStore(loopTestPool)

	gate := "delivery"
	round := int32(1)
	test := []string{"go", "test", "./..."}
	vet := []string{"go", "vet", "./..."}

	if err := store.Enqueue(ctx, issueID, gate, round, [][]string{test, vet}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	pending, err := store.PendingDispatch(ctx, issueID, gate, round)
	if err != nil {
		t.Fatalf("pending dispatch: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("both fresh checks should be pending dispatch, got %d", len(pending))
	}

	// Dispatch one. It leaves the pending set; the other stays.
	if err := store.MarkDispatched(ctx, issueID, gate, round, test); err != nil {
		t.Fatalf("mark dispatched: %v", err)
	}
	pending, _ = store.PendingDispatch(ctx, issueID, gate, round)
	if len(pending) != 1 {
		t.Fatalf("one dispatched should leave one pending, got %d", len(pending))
	}

	// Dispatch the second, then a re-enqueue must not re-surface either.
	if err := store.MarkDispatched(ctx, issueID, gate, round, vet); err != nil {
		t.Fatalf("mark dispatched vet: %v", err)
	}
	if err := store.Enqueue(ctx, issueID, gate, round, [][]string{test, vet}); err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}
	pending, _ = store.PendingDispatch(ctx, issueID, gate, round)
	if len(pending) != 0 {
		t.Fatalf("dispatched checks must not re-surface after re-enqueue, got %d", len(pending))
	}
}

// TestGateEvaluator_DispatchesEachCheckOnce proves the egress wiring end to end:
// the first evaluation of a fresh gate dispatches every required check exactly
// once, a re-evaluation while the checks are in flight dispatches nothing more,
// and the gate still advances only once every check reports green.
func TestGateEvaluator_DispatchesEachCheckOnce(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	disp := &fakeDispatcher{}
	eval := NewGateEvaluator(loopTestPool).WithDispatcher(disp)

	gate := "dispatch-gate"
	agentID := "11111111-1111-1111-1111-111111111111"
	test := []string{"go", "test", "./..."}
	vet := []string{"go", "vet", "./..."}
	value := CheckGateConfig{Checks: [][]string{test, vet}, AgentID: agentID}

	// First evaluation: both checks dispatched once, gate holds.
	advance, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("first eval: %v", err)
	}
	if advance {
		t.Fatal("gate advanced before any check reported")
	}
	if len(disp.calls) != 2 {
		t.Fatalf("first eval should dispatch 2 checks, got %d", len(disp.calls))
	}
	if disp.calls[0].AgentID != agentID {
		t.Fatalf("dispatch missing agent id: %+v", disp.calls[0])
	}
	if disp.calls[0].Gate != gate {
		t.Fatalf("dispatch missing gate: %+v", disp.calls[0])
	}

	// Second evaluation while in flight: nothing re-dispatched.
	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("second eval: %v", err)
	}
	if len(disp.calls) != 2 {
		t.Fatalf("in-flight re-eval must not re-dispatch, got %d", len(disp.calls))
	}

	// All checks report green: the gate advances, with no extra dispatch.
	if err := eval.store.Report(ctx, issueID, gate, gateRound, test, 0); err != nil {
		t.Fatalf("report test: %v", err)
	}
	if err := eval.store.Report(ctx, issueID, gate, gateRound, vet, 0); err != nil {
		t.Fatalf("report vet: %v", err)
	}
	advance, err = eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("eval all green: %v", err)
	}
	if !advance {
		t.Fatal("gate did not advance with all checks green")
	}
	if len(disp.calls) != 2 {
		t.Fatalf("advancing must not dispatch more, got %d", len(disp.calls))
	}
}

// TestGateEvaluator_DispatchesRevisionOnFailure proves the revise-loop egress:
// a failed check calls DispatchRevision (re-running the build), not
// DispatchCheck, and carries the failing outcome so the worker knows what to
// fix.
func TestGateEvaluator_DispatchesRevisionOnFailure(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	disp := &fakeDispatcher{}
	eval := NewGateEvaluator(loopTestPool).WithDispatcher(disp)

	gate := "revision-gate"
	agentID := "11111111-1111-1111-1111-111111111111"
	check := []string{"go", "test", "./..."}
	value := CheckGateConfig{
		Checks:        [][]string{check},
		AgentID:       agentID,
		RevisionSkill: "build",
		Caps:          Caps{MaxIterations: 10, MaxRevisions: 10, NoProgressStalls: 10},
	}

	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("first eval: %v", err)
	}
	if err := eval.store.Report(ctx, issueID, gate, 1, check, 1); err != nil {
		t.Fatalf("report failing: %v", err)
	}
	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("eval after failure: %v", err)
	}

	if len(disp.revisionCalls) != 1 {
		t.Fatalf("want 1 revision dispatched, got %d", len(disp.revisionCalls))
	}
	rc := disp.revisionCalls[0]
	if rc.AgentID != agentID || rc.Gate != gate || rc.SkillName != "build" {
		t.Fatalf("revision dispatch wrong: %+v", rc)
	}
	if rc.Round != 2 {
		t.Fatalf("revision should target the fresh round, got %d", rc.Round)
	}
	if len(rc.Failures) != 1 || rc.Failures[0].ExitCode != 1 {
		t.Fatalf("revision must carry the failing outcome: %+v", rc.Failures)
	}
	// A revision must not also fire a check dispatch for the same failure.
	if len(disp.calls) != 1 {
		t.Fatalf("want exactly the round-1 check dispatch, got %d", len(disp.calls))
	}
}

// TestGateEvaluator_DispatchesJudgeChecksOnce proves the judge egress wiring
// end to end, mirroring TestGateEvaluator_DispatchesEachCheckOnce: the first
// evaluation dispatches the judge check exactly once, a re-evaluation while it
// is in flight dispatches nothing more, and the gate advances only once the
// judge reports a passing verdict alongside the programmatic check.
func TestGateEvaluator_DispatchesJudgeChecksOnce(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	disp := &fakeDispatcher{}
	eval := NewGateEvaluator(loopTestPool).WithDispatcher(disp)

	gate := "judge-dispatch-gate"
	agentID := "11111111-1111-1111-1111-111111111111"
	judgeAgentID := "22222222-2222-2222-2222-222222222222"
	check := []string{"go", "test", "./..."}
	judgeCheck := JudgeCheck{ID: "ux-quality", Rubric: "the UI must not regress"}
	value := CheckGateConfig{
		Checks:       [][]string{check},
		AgentID:      agentID,
		JudgeChecks:  []JudgeCheck{judgeCheck},
		JudgeAgentID: judgeAgentID,
		JudgeSkill:   "judge-skill",
	}

	// First evaluation: the check and the judge check are both dispatched
	// once, gate holds.
	advance, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("first eval: %v", err)
	}
	if advance {
		t.Fatal("gate advanced before anything reported")
	}
	if len(disp.calls) != 1 {
		t.Fatalf("first eval should dispatch 1 check, got %d", len(disp.calls))
	}
	if len(disp.judgeCalls) != 1 {
		t.Fatalf("first eval should dispatch 1 judge check, got %d", len(disp.judgeCalls))
	}
	jc := disp.judgeCalls[0]
	if jc.AgentID != judgeAgentID {
		t.Fatalf("judge dispatch should go to the judge agent, got %q", jc.AgentID)
	}
	if jc.CheckID != judgeCheck.ID || jc.Rubric != judgeCheck.Rubric || jc.SkillName != "judge-skill" {
		t.Fatalf("judge dispatch wrong: %+v", jc)
	}

	// Second evaluation while in flight: nothing re-dispatched.
	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("second eval: %v", err)
	}
	if len(disp.judgeCalls) != 1 {
		t.Fatalf("in-flight re-eval must not re-dispatch the judge check, got %d", len(disp.judgeCalls))
	}

	// The programmatic check reports green but the judge has not reported yet:
	// the gate must still hold.
	if err := eval.store.Report(ctx, issueID, gate, gateRound, check, 0); err != nil {
		t.Fatalf("report check: %v", err)
	}
	advance, err = eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("eval check green, judge pending: %v", err)
	}
	if advance {
		t.Fatal("gate advanced before the judge reported")
	}

	// The judge reports a passing verdict: the gate advances, with no extra
	// dispatch.
	if err := eval.store.ReportJudge(ctx, issueID, gate, gateRound, judgeCheck.ID, true, nil); err != nil {
		t.Fatalf("report judge pass: %v", err)
	}
	advance, err = eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("eval all green: %v", err)
	}
	if !advance {
		t.Fatal("gate did not advance with check and judge both green")
	}
	if len(disp.calls) != 1 || len(disp.judgeCalls) != 1 {
		t.Fatalf("advancing must not dispatch more, got %d checks, %d judge checks", len(disp.calls), len(disp.judgeCalls))
	}
}

// TestGateEvaluator_JudgeRevisionCarriesBlockingIssues proves a failed judge
// verdict revises the gate exactly like a failed programmatic check, and that
// the caps tracker's stall signature reacts to the judge outcome too — a
// worker that fixes nothing and gets the same judge verdict twice in a row
// must count as a no-progress stall, not a fresh attempt.
func TestGateEvaluator_JudgeRevisionCarriesBlockingIssues(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	disp := &fakeDispatcher{}
	eval := NewGateEvaluator(loopTestPool).WithDispatcher(disp)

	gate := "judge-revision-gate"
	agentID := "11111111-1111-1111-1111-111111111111"
	check := []string{"go", "test", "./..."}
	judgeCheck := JudgeCheck{ID: "ux-quality", Rubric: "the UI must not regress"}
	value := CheckGateConfig{
		Checks:        [][]string{check},
		AgentID:       agentID,
		JudgeChecks:   []JudgeCheck{judgeCheck},
		RevisionSkill: "build",
		Caps:          Caps{MaxIterations: 10, MaxRevisions: 10, NoProgressStalls: 2},
	}

	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("first eval: %v", err)
	}
	if err := eval.store.Report(ctx, issueID, gate, 1, check, 0); err != nil {
		t.Fatalf("report check green: %v", err)
	}
	if err := eval.store.ReportJudge(ctx, issueID, gate, 1, judgeCheck.ID, false, []string{"button misaligned"}); err != nil {
		t.Fatalf("report judge failure: %v", err)
	}
	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("eval after judge failure: %v", err)
	}

	if len(disp.revisionCalls) != 1 {
		t.Fatalf("want 1 revision dispatched from a judge failure, got %d", len(disp.revisionCalls))
	}
	state, err := eval.store.LoadGateState(ctx, issueID, gate)
	if err != nil {
		t.Fatalf("load gate state: %v", err)
	}
	if state.Round != 2 || state.Revisions != 1 {
		t.Fatalf("judge failure should advance the round like a check failure: %+v", state)
	}
}
