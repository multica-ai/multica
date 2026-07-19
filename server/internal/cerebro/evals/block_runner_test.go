package evals

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/loops"
)

func TestBlockRunnerSelectsBindingPhaseAndHonorsAdvisory(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	f := seedEvalFixture(t)
	evalID := seedActiveEval(t, f, "phase-aware-block", 1)
	otherEvalID := seedActiveEval(t, f, "other-advisory", 1)
	store := NewStore(evalTestPool)
	for _, binding := range []BindingInput{
		{WorkflowID: f.workflowID, EvalID: evalID, Phase: "plan", Blocking: false},
		{WorkflowID: f.workflowID, EvalID: evalID, Phase: "delivery", Blocking: true},
		{WorkflowID: f.workflowID, EvalID: evalID, Phase: "monitor", Blocking: true},
		{WorkflowID: f.workflowID, EvalID: otherEvalID, Phase: "plan", Blocking: false},
	} {
		if _, err := store.CreateBinding(ctx, f.workspaceID, f.actorID, binding); err != nil {
			t.Fatalf("create %s binding: %v", binding.Phase, err)
		}
	}
	executor := &fakeRunExecutor{execution: RunExecution{
		Status:        RunStatusFailed,
		TargetVersion: "target-v1",
		Results:       json.RawMessage(`{"outcome":{"status":"failed","pass_rate":0}}`),
	}}
	inbox := &fakeInboxWriter{}
	runner := NewBlockRunner(evalTestPool, executor).WithAdvisoryWarner(NewAdvisoryWarner(
		store,
		&fakeOwnerAdminLister{recipients: []pgtype.UUID{recipient()}},
		inbox,
		nil,
	))
	dispatch := loops.BlockDispatch{
		Run: loops.ChainRun{
			IssueID:    pgtype.UUID{Bytes: f.issueID, Valid: true},
			WorkflowID: pgtype.UUID{Bytes: f.workflowID, Valid: true},
		},
		Block: loops.Block{ID: "quality", Type: loops.BlockEval, EvalKey: "phase-aware-block", EvalPhase: "plan"},
	}

	status, outcome, err := runner.RunEvalBlock(ctx, dispatch)
	if err != nil || status != loops.StepCompleted {
		t.Fatalf("advisory eval should complete with warning: status=%q outcome=%s err=%v", status, outcome, err)
	}
	var advisory map[string]any
	if err := json.Unmarshal(outcome, &advisory); err != nil || advisory["warning"] != true || advisory["phase"] != "plan" {
		t.Fatalf("advisory outcome=%s err=%v", outcome, err)
	}
	if len(inbox.calls) != 1 || inbox.calls[0].Type != inboxTypeEvalAdvisoryFailed {
		t.Fatalf("only the executed advisory binding should notify, cards=%+v", inbox.calls)
	}

	dispatch.Block.EvalPhase = "delivery"
	status, outcome, err = runner.RunEvalBlock(ctx, dispatch)
	if err != nil || status != loops.StepFailed {
		t.Fatalf("blocking eval should fail the step: status=%q outcome=%s err=%v", status, outcome, err)
	}

	dispatch.Block.EvalPhase = "monitor"
	status, outcome, err = runner.RunEvalBlock(ctx, dispatch)
	if err != nil || status != loops.StepCompleted {
		t.Fatalf("monitor eval must never block: status=%q outcome=%s err=%v", status, outcome, err)
	}
}

func TestBlockRunnerDefaultsLegacyBlockToDelivery(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	f := seedEvalFixture(t)
	evalID := seedActiveEval(t, f, "legacy-delivery-block", 1)
	if _, err := NewStore(evalTestPool).CreateBinding(ctx, f.workspaceID, f.actorID, BindingInput{
		WorkflowID: f.workflowID, EvalID: evalID, Phase: "delivery", Blocking: true,
	}); err != nil {
		t.Fatal(err)
	}
	runner := NewBlockRunner(evalTestPool, &fakeRunExecutor{execution: RunExecution{
		Status: RunStatusPassed, TargetVersion: "target-v1",
		Results: json.RawMessage(`{"outcome":{"status":"passed","pass_rate":1}}`),
	}})
	status, _, err := runner.RunEvalBlock(ctx, loops.BlockDispatch{
		Run: loops.ChainRun{
			IssueID:    pgtype.UUID{Bytes: f.issueID, Valid: true},
			WorkflowID: pgtype.UUID{Bytes: f.workflowID, Valid: true},
		},
		Block: loops.Block{ID: "quality", Type: loops.BlockEval, EvalKey: "legacy-delivery-block"},
	})
	if err != nil || status != loops.StepCompleted {
		t.Fatalf("legacy block should use delivery binding: status=%q err=%v", status, err)
	}
}
