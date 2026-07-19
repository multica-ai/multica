package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var evalTestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping eval integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping eval integration tests: db not reachable: %v\n", err)
		os.Exit(m.Run())
	}
	evalTestPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

type evalFixture struct {
	workspaceID uuid.UUID
	issueID     uuid.UUID
	workflowID  uuid.UUID
	actorID     uuid.UUID
}

type fakeRunExecutor struct {
	execution RunExecution
	err       error
	calls     int
}

func (f *fakeRunExecutor) Execute(_ context.Context, _ Eval) (RunExecution, error) {
	f.calls++
	return f.execution, f.err
}

func seedEvalFixture(t *testing.T) evalFixture {
	t.Helper()
	ctx := context.Background()
	var f evalFixture
	f.actorID = uuid.New()
	if err := evalTestPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('Eval Test', 'eval-test-'||gen_random_uuid()) RETURNING id`,
	).Scan(&f.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := evalTestPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		 VALUES ($1, 'Eval threshold gate', 'member', $2) RETURNING id`,
		f.workspaceID, f.actorID,
	).Scan(&f.issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := evalTestPool.QueryRow(ctx,
		`INSERT INTO cerebro_workflow (
		 workspace_id, name, trigger_type, trigger_config, conditions,
		 action_type, action_config, created_by_id, created_by_type
		) VALUES ($1, 'Eval gate workflow', 'status_changed', '{}'::jsonb, '[]'::jsonb,
		 'set_status', '{}'::jsonb, $2, 'member') RETURNING id`,
		f.workspaceID, f.actorID,
	).Scan(&f.workflowID); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	t.Cleanup(func() {
		_, _ = evalTestPool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, f.workspaceID)
	})
	return f
}

func seedActiveEval(t *testing.T, f evalFixture, key string, threshold float64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	thresholds, _ := json.Marshal([]map[string]any{{
		"metric": "pass_rate", "operator": "gte", "value": threshold,
	}})
	if err := evalTestPool.QueryRow(context.Background(),
		`INSERT INTO cerebro_eval (
		 workspace_id, eval_key, version, title, status, objective, target,
		 datasets, graders, thresholds, runner, source, created_by_id, created_by_type
		) VALUES ($1,$2,'1.0.0',$2,'active','Prove server-side scoring','{}'::jsonb,
		 '[]'::jsonb,'[]'::jsonb,$3,'{}'::jsonb,'{}'::jsonb,$4,'member') RETURNING id`,
		f.workspaceID, key, thresholds, f.actorID,
	).Scan(&id); err != nil {
		t.Fatalf("create eval: %v", err)
	}
	return id
}

func TestStoreCreateRunIgnoresCallerCasesAndUsesServerExecution(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	evalID := seedActiveEval(t, f, "runner-owned", 0.75)
	if _, err := NewStore(evalTestPool).CreateBinding(context.Background(), f.workspaceID, f.actorID, BindingInput{
		WorkflowID: f.workflowID, EvalID: evalID, Phase: "delivery", Blocking: true,
	}); err != nil {
		t.Fatalf("create binding: %v", err)
	}
	executor := &fakeRunExecutor{execution: RunExecution{
		Status:        RunStatusFailed,
		Results:       json.RawMessage(`{"cases":[{"case_id":"server-case","passed":false}],"outcome":{"status":"failed","pass_rate":0}}`),
		CostCents:     7,
		LatencyMS:     11,
		TargetVersion: "server-target-v1",
	}}
	store := NewStore(evalTestPool).WithRunExecutor(executor)

	run, err := store.CreateRun(context.Background(), f.workspaceID, f.actorID, evalID, "member", EvalRunInput{
		WorkflowID:    &f.workflowID,
		IssueID:       &f.issueID,
		Status:        RunStatusPassed,
		Results:       json.RawMessage(`{"cases":[{"case_id":"forged","passed":true},{"case_id":"forged-2","passed":true}]}`),
		CostCents:     999,
		LatencyMS:     999,
		TargetVersion: "caller-target",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("server executor calls=%d, want 1", executor.calls)
	}
	if run.Status != RunStatusFailed || run.CostCents != 7 || run.LatencyMS != 11 {
		t.Fatalf("persisted run used caller report: %+v", run)
	}
	var persisted struct {
		Cases []struct {
			CaseID string `json:"case_id"`
		} `json:"cases"`
		Outcome RunOutcome `json:"outcome"`
	}
	if err := json.Unmarshal(run.Results, &persisted); err != nil {
		t.Fatalf("decode persisted result: %v", err)
	}
	if run.TargetVersion != "server-target-v1" || len(persisted.Cases) != 1 || persisted.Cases[0].CaseID != "server-case" || persisted.Outcome.Status != RunStatusFailed {
		t.Fatalf("persisted result did not come from server execution: target=%q results=%s", run.TargetVersion, run.Results)
	}
	passed, err := store.BlockingEvalsPassed(context.Background(), f.workflowID, f.issueID, "delivery")
	if err != nil || passed {
		t.Fatalf("forged caller cases opened the gate: passed=%v err=%v", passed, err)
	}
}

func TestBlockingEvalsPassedScopesPlanDeliveryAndMonitorIndependently(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	store := NewStore(evalTestPool).WithRunExecutor(&fakeRunExecutor{execution: RunExecution{
		TargetVersion: "server-target-v1",
		Status:        RunStatusPassed,
		Results:       json.RawMessage(`{"cases":[{"case_id":"server-case","passed":true}],"outcome":{"status":"passed","pass_rate":1}}`),
	}})
	for _, phase := range []string{"plan", "delivery", "monitor"} {
		evalID := seedActiveEval(t, f, "gate-"+phase, 1)
		if _, err := store.CreateBinding(context.Background(), f.workspaceID, f.actorID, BindingInput{
			WorkflowID: f.workflowID, EvalID: evalID, Phase: phase, Blocking: true,
		}); err != nil {
			t.Fatalf("create %s binding: %v", phase, err)
		}
		passed, err := store.BlockingEvalsPassed(context.Background(), f.workflowID, f.issueID, phase)
		if err != nil || passed {
			t.Fatalf("%s binding without a run should fail closed: passed=%v err=%v", phase, passed, err)
		}
		if _, err := store.CreateRun(context.Background(), f.workspaceID, f.actorID, evalID, "member", EvalRunInput{
			WorkflowID: &f.workflowID, IssueID: &f.issueID, Status: "failed",
			Results: json.RawMessage(`{"cases":[{"passed":true}]}`),
		}); err != nil {
			t.Fatalf("create %s passing result: %v", phase, err)
		}
		passed, err = store.BlockingEvalsPassed(context.Background(), f.workflowID, f.issueID, phase)
		if err != nil || !passed {
			t.Fatalf("%s binding with a server-derived passing run should pass: passed=%v err=%v", phase, passed, err)
		}
	}
}

// insertRunAt inserts a run for an eval+issue with an explicit created_at so the
// "latest" ordering under test is deterministic.
func insertRunAt(t *testing.T, f evalFixture, evalID uuid.UUID, status string, createdAt time.Time) {
	t.Helper()
	if _, err := evalTestPool.Exec(context.Background(), `INSERT INTO cerebro_eval_run
		(workspace_id, eval_id, eval_version, issue_id, status, results, created_by_id, created_by_type, created_at)
		SELECT $1, e.id, e.version, $3, $4, '{}'::jsonb, $5, 'member', $6
		FROM cerebro_eval e WHERE e.workspace_id=$1 AND e.id=$2`,
		f.workspaceID, evalID, f.issueID, status, f.actorID, createdAt); err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func TestLatestRunPassed(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	store := NewStore(evalTestPool)
	base := time.Now().UTC()

	t.Run("newest passed after older failed", func(t *testing.T) {
		f := seedEvalFixture(t)
		evalID := seedActiveEval(t, f, "latest-passed", 1)
		insertRunAt(t, f, evalID, "failed", base.Add(-2*time.Hour))
		insertRunAt(t, f, evalID, "passed", base.Add(-1*time.Hour))
		passed, err := store.LatestRunPassed(ctx, f.workspaceID, evalID, f.issueID)
		if err != nil {
			t.Fatal(err)
		}
		if !passed {
			t.Fatal("expected latest run to be passed")
		}
	})

	t.Run("newest failed", func(t *testing.T) {
		f := seedEvalFixture(t)
		evalID := seedActiveEval(t, f, "latest-failed", 1)
		insertRunAt(t, f, evalID, "passed", base.Add(-2*time.Hour))
		insertRunAt(t, f, evalID, "failed", base.Add(-1*time.Hour))
		passed, err := store.LatestRunPassed(ctx, f.workspaceID, evalID, f.issueID)
		if err != nil {
			t.Fatal(err)
		}
		if passed {
			t.Fatal("expected latest run to be failed (fail closed)")
		}
	})

	t.Run("no runs fails closed", func(t *testing.T) {
		f := seedEvalFixture(t)
		evalID := seedActiveEval(t, f, "latest-none", 1)
		passed, err := store.LatestRunPassed(ctx, f.workspaceID, evalID, f.issueID)
		if err != nil {
			t.Fatal(err)
		}
		if passed {
			t.Fatal("expected no runs to fail closed")
		}
	})
}

func TestFailingAdvisoryEvals(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	store := NewStore(evalTestPool)

	t.Run("returns advisory bindings with no passing run, excludes blocking and passing", func(t *testing.T) {
		f := seedEvalFixture(t)
		// Advisory binding, no run yet → failing (warn on missing evidence).
		advNoRun := seedActiveEval(t, f, "adv-no-run", 1)
		if _, err := store.CreateBinding(ctx, f.workspaceID, f.actorID, BindingInput{
			WorkflowID: f.workflowID, EvalID: advNoRun, Phase: "delivery", Blocking: false,
		}); err != nil {
			t.Fatalf("create advisory no-run binding: %v", err)
		}
		// Advisory binding whose latest run failed → failing.
		advFailed := seedActiveEval(t, f, "adv-failed", 1)
		if _, err := store.CreateBinding(ctx, f.workspaceID, f.actorID, BindingInput{
			WorkflowID: f.workflowID, EvalID: advFailed, Phase: "delivery", Blocking: false,
		}); err != nil {
			t.Fatalf("create advisory failed binding: %v", err)
		}
		insertLinkedRunAt(t, f, advFailed, "failed", time.Now().UTC())
		// Advisory binding whose latest run passed → excluded.
		advPassed := seedActiveEval(t, f, "adv-passed", 1)
		if _, err := store.CreateBinding(ctx, f.workspaceID, f.actorID, BindingInput{
			WorkflowID: f.workflowID, EvalID: advPassed, Phase: "delivery", Blocking: false,
		}); err != nil {
			t.Fatalf("create advisory passed binding: %v", err)
		}
		insertLinkedRunAt(t, f, advPassed, "passed", time.Now().UTC())
		// Blocking binding, no run → excluded (advisory query only).
		blk := seedActiveEval(t, f, "blk", 1)
		if _, err := store.CreateBinding(ctx, f.workspaceID, f.actorID, BindingInput{
			WorkflowID: f.workflowID, EvalID: blk, Phase: "delivery", Blocking: true,
		}); err != nil {
			t.Fatalf("create blocking binding: %v", err)
		}

		failing, err := store.FailingAdvisoryEvals(ctx, f.workflowID, f.issueID, "delivery")
		if err != nil {
			t.Fatalf("failing advisory evals: %v", err)
		}
		keys := map[string]bool{}
		for _, b := range failing {
			keys[b.EvalKey] = true
			if b.Blocking {
				t.Fatalf("blocking binding leaked into advisory result: %+v", b)
			}
		}
		if !keys["adv-no-run"] || !keys["adv-failed"] {
			t.Fatalf("expected failing advisory bindings adv-no-run and adv-failed, got %v", keys)
		}
		if keys["adv-passed"] || keys["blk"] {
			t.Fatalf("passing advisory or blocking binding must be excluded, got %v", keys)
		}
	})

	t.Run("scopes by phase", func(t *testing.T) {
		f := seedEvalFixture(t)
		monitorEval := seedActiveEval(t, f, "adv-monitor", 1)
		if _, err := store.CreateBinding(ctx, f.workspaceID, f.actorID, BindingInput{
			WorkflowID: f.workflowID, EvalID: monitorEval, Phase: "monitor", Blocking: false,
		}); err != nil {
			t.Fatalf("create monitor advisory binding: %v", err)
		}
		delivery, err := store.FailingAdvisoryEvals(ctx, f.workflowID, f.issueID, "delivery")
		if err != nil {
			t.Fatal(err)
		}
		if len(delivery) != 0 {
			t.Fatalf("monitor binding must not surface under the delivery phase, got %d", len(delivery))
		}
		monitor, err := store.FailingAdvisoryEvals(ctx, f.workflowID, f.issueID, "monitor")
		if err != nil {
			t.Fatal(err)
		}
		if len(monitor) != 1 || monitor[0].EvalKey != "adv-monitor" {
			t.Fatalf("expected the monitor advisory binding, got %+v", monitor)
		}
	})
}

// insertLinkedRunAt inserts a run for an eval+issue linked to the fixture
// workflow (FailingAdvisoryEvals joins on workflow_id, unlike insertRunAt).
func insertLinkedRunAt(t *testing.T, f evalFixture, evalID uuid.UUID, status string, createdAt time.Time) {
	t.Helper()
	if _, err := evalTestPool.Exec(context.Background(), `INSERT INTO cerebro_eval_run
		(workspace_id, eval_id, eval_version, workflow_id, issue_id, status, results, created_by_id, created_by_type, created_at)
		SELECT $1, e.id, e.version, $3, $4, $5, '{}'::jsonb, $6, 'member', $7
		FROM cerebro_eval e WHERE e.workspace_id=$1 AND e.id=$2`,
		f.workspaceID, evalID, f.workflowID, f.issueID, status, f.actorID, createdAt); err != nil {
		t.Fatalf("insert linked run: %v", err)
	}
}

func TestRunForIssue(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	f := seedEvalFixture(t)
	evalID := seedActiveEval(t, f, "run-for-issue", 1)

	t.Run("executes and persists linked to issue", func(t *testing.T) {
		store := NewStore(evalTestPool).WithRunExecutor(&fakeRunExecutor{execution: RunExecution{
			Status:        RunStatusPassed,
			Results:       json.RawMessage(`{"cases":[{"case_id":"c","passed":true}],"outcome":{"status":"passed","pass_rate":1}}`),
			TargetVersion: "server-target-v1",
		}})
		runID, status, err := store.RunForIssue(ctx, f.workspaceID, f.actorID, evalID, f.issueID, "member")
		if err != nil {
			t.Fatalf("run for issue: %v", err)
		}
		if status != RunStatusPassed || runID == "" {
			t.Fatalf("run id=%q status=%q", runID, status)
		}
		passed, err := store.LatestRunPassed(ctx, f.workspaceID, evalID, f.issueID)
		if err != nil || !passed {
			t.Fatalf("persisted run not readable as passed: passed=%v err=%v", passed, err)
		}
	})

	t.Run("fails closed without a runner", func(t *testing.T) {
		if _, _, err := NewStore(evalTestPool).RunForIssue(ctx, f.workspaceID, f.actorID, evalID, f.issueID, "member"); err == nil {
			t.Fatal("expected RunForIssue to fail when no runner is wired")
		}
	})
}
