package loops

// FIR-2283 integration test for the loop check transport store. Proves the
// round-trip the engine relies on: enqueue pending checks, report exit codes,
// and read them back as the []CheckOutcome the gate logic decides from — so the
// persisted state drives Reconcile / EvaluateGate exactly as the in-memory
// tests do. Skips cleanly when no test DB is reachable.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var loopTestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping loops integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping loops integration tests: db not reachable: %v\n", err)
		os.Exit(m.Run())
	}
	loopTestPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// seedIssue creates a throwaway workspace + issue and returns the issue id. The
// workspace is dropped on cleanup, cascading to the issue and its check runs.
func seedIssue(t *testing.T) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var workspaceID, issueID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('Loops Test', 'loops-test-'||gen_random_uuid()) RETURNING id`).
		Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := loopTestPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		 VALUES ($1, 'Loop check transport', 'member', gen_random_uuid()) RETURNING id`,
		workspaceID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = loopTestPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	return issueID
}

// TestStore_RoundTripDrivesGate walks the full transport: pending -> reported
// -> gate decision, and proves a re-enqueue never resets a reported result.
func TestStore_RoundTripDrivesGate(t *testing.T) {
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
	cfg := CheckGateConfig{Checks: [][]string{test, vet}}

	// 1. Enqueue both checks. With nothing reported they are in flight, so the
	//    gate waits.
	if err := store.Enqueue(ctx, issueID, gate, round, cfg.Checks); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	outcomes, err := store.Outcomes(ctx, issueID, gate, round)
	if err != nil {
		t.Fatalf("outcomes: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("want 2 enqueued outcomes, got %d", len(outcomes))
	}
	if got := Reconcile(cfg, outcomes, nil, nil).Action; got != GateWait {
		t.Fatalf("all pending should wait, got %s", got)
	}

	// 2. One check fails. The gate must revise, even though the other check has
	//    not reported yet — a known failure wins over a pending check.
	if err := store.Report(ctx, issueID, gate, round, test, 1); err != nil {
		t.Fatalf("report failing: %v", err)
	}
	outcomes, _ = store.Outcomes(ctx, issueID, gate, round)
	if got := EvaluateGate(cfg, outcomes); got != GateFailed {
		t.Fatalf("a failed check should fail the gate, got %s", got)
	}
	if got := Reconcile(cfg, outcomes, nil, nil).Action; got != GateRevise {
		t.Fatalf("a failed check should revise, got %s", got)
	}

	// 3. Both checks pass (the failing one re-reported green on the next round's
	//    re-run, modeled here as an updated exit code). The gate advances.
	if err := store.Report(ctx, issueID, gate, round, test, 0); err != nil {
		t.Fatalf("report passing test: %v", err)
	}
	if err := store.Report(ctx, issueID, gate, round, vet, 0); err != nil {
		t.Fatalf("report passing vet: %v", err)
	}
	outcomes, _ = store.Outcomes(ctx, issueID, gate, round)
	if got := EvaluateGate(cfg, outcomes); got != GatePassed {
		t.Fatalf("all green should pass the gate, got %s", got)
	}
	if got := Reconcile(cfg, outcomes, nil, nil).Action; got != GateAdvance {
		t.Fatalf("all green should advance, got %s", got)
	}

	// 4. A re-enqueue must not reset the reported exit codes (idempotent
	//    transport: a re-fire keeps the gate green instead of reopening it).
	if err := store.Enqueue(ctx, issueID, gate, round, cfg.Checks); err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}
	outcomes, _ = store.Outcomes(ctx, issueID, gate, round)
	if len(outcomes) != 2 {
		t.Fatalf("re-enqueue must not add rows, got %d", len(outcomes))
	}
	if got := EvaluateGate(cfg, outcomes); got != GatePassed {
		t.Fatalf("re-enqueue must not reopen the gate, got %s", got)
	}
}

// TestStore_JudgeRoundTripDrivesGate mirrors TestStore_RoundTripDrivesGate for
// judge checks: enqueue -> reported verdict -> gate decision, proving the
// persisted judge state drives Reconcile exactly like the in-memory tests.
func TestStore_JudgeRoundTripDrivesGate(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	store := NewStore(loopTestPool)

	gate := "delivery"
	round := int32(1)
	judgeCheck := JudgeCheck{ID: "ux-quality", Rubric: "the UI must not regress"}
	cfg := CheckGateConfig{JudgeChecks: []JudgeCheck{judgeCheck}}

	// 1. Enqueue the judge check. With nothing reported it is in flight, so the
	//    gate waits.
	if err := store.EnqueueJudge(ctx, issueID, gate, round, cfg.JudgeChecks); err != nil {
		t.Fatalf("enqueue judge: %v", err)
	}
	judgeOutcomes, err := store.JudgeOutcomes(ctx, issueID, gate, round)
	if err != nil {
		t.Fatalf("judge outcomes: %v", err)
	}
	if len(judgeOutcomes) != 1 {
		t.Fatalf("want 1 enqueued judge outcome, got %d", len(judgeOutcomes))
	}
	if got := Reconcile(cfg, nil, judgeOutcomes, nil).Action; got != GateWait {
		t.Fatalf("pending judge should wait, got %s", got)
	}

	// 2. The judge reports a failing verdict with blocking issues. The gate
	//    must revise.
	if err := store.ReportJudge(ctx, issueID, gate, round, judgeCheck.ID, false, []string{"button misaligned on mobile"}); err != nil {
		t.Fatalf("report judge failure: %v", err)
	}
	judgeOutcomes, _ = store.JudgeOutcomes(ctx, issueID, gate, round)
	if got := Reconcile(cfg, nil, judgeOutcomes, nil).Action; got != GateRevise {
		t.Fatalf("a failed judge verdict should revise, got %s", got)
	}
	if len(judgeOutcomes) != 1 || len(judgeOutcomes[0].Blocking) != 1 {
		t.Fatalf("blocking issues not persisted: %+v", judgeOutcomes)
	}

	// 3. The judge re-scores and passes. The gate advances.
	if err := store.ReportJudge(ctx, issueID, gate, round, judgeCheck.ID, true, nil); err != nil {
		t.Fatalf("report judge pass: %v", err)
	}
	judgeOutcomes, _ = store.JudgeOutcomes(ctx, issueID, gate, round)
	if got := Reconcile(cfg, nil, judgeOutcomes, nil).Action; got != GateAdvance {
		t.Fatalf("a passing judge verdict should advance, got %s", got)
	}

	// 4. A re-enqueue must not reset the reported verdict.
	if err := store.EnqueueJudge(ctx, issueID, gate, round, cfg.JudgeChecks); err != nil {
		t.Fatalf("re-enqueue judge: %v", err)
	}
	judgeOutcomes, _ = store.JudgeOutcomes(ctx, issueID, gate, round)
	if len(judgeOutcomes) != 1 {
		t.Fatalf("re-enqueue must not add rows, got %d", len(judgeOutcomes))
	}
	if got := Reconcile(cfg, nil, judgeOutcomes, nil).Action; got != GateAdvance {
		t.Fatalf("re-enqueue must not reopen the gate, got %s", got)
	}
}

// TestStore_JudgeDispatchOnce proves the judge dispatch dedup at the store
// layer, mirroring TestStore_DispatchOnce for programmatic checks.
func TestStore_JudgeDispatchOnce(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	store := NewStore(loopTestPool)

	gate := "delivery"
	round := int32(1)
	a := JudgeCheck{ID: "ux-quality", Rubric: "no regressions"}
	b := JudgeCheck{ID: "copy-tone", Rubric: "matches brand voice"}

	if err := store.EnqueueJudge(ctx, issueID, gate, round, []JudgeCheck{a, b}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	pending, err := store.PendingJudgeDispatch(ctx, issueID, gate, round)
	if err != nil {
		t.Fatalf("pending dispatch: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("both fresh judge checks should be pending dispatch, got %d", len(pending))
	}

	if err := store.MarkJudgeDispatched(ctx, issueID, gate, round, a.ID); err != nil {
		t.Fatalf("mark dispatched: %v", err)
	}
	pending, _ = store.PendingJudgeDispatch(ctx, issueID, gate, round)
	if len(pending) != 1 {
		t.Fatalf("one dispatched should leave one pending, got %d", len(pending))
	}

	if err := store.MarkJudgeDispatched(ctx, issueID, gate, round, b.ID); err != nil {
		t.Fatalf("mark dispatched b: %v", err)
	}
	if err := store.EnqueueJudge(ctx, issueID, gate, round, []JudgeCheck{a, b}); err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}
	pending, _ = store.PendingJudgeDispatch(ctx, issueID, gate, round)
	if len(pending) != 0 {
		t.Fatalf("dispatched judge checks must not re-surface after re-enqueue, got %d", len(pending))
	}
}
