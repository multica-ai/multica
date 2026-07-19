package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

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
