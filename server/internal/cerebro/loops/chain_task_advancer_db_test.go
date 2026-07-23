package loops

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestChainTaskAdvancerCompletesTaskStepAndStartsNextBlock(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	var workspaceID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id=$1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	chain := &Chain{Version: ChainVersion, Phases: []Phase{{
		ID: "build", Limits: PhaseLimits{MaxSteps: 4, MaxRounds: 4, NoProgressStalls: 2},
		Blocks: []Block{{ID: "work", Type: BlockSession, Skill: "build"}, {ID: "eval", Type: BlockEval, EvalKey: "delivery"}},
	}}}
	raw, _ := json.Marshal(chain)
	recipe := seedIssueLoopRecipe(t, workspaceID, "completion chain", raw)
	store := NewStore(loopTestPool)
	ref := StepRef{PhaseRunKey: PhaseRunKey{IssueID: issueID, WorkflowID: recipe, PhaseID: "build"}, BlockID: "work", Number: 1}
	if _, _, err := store.OpenStep(ctx, ref, chain.Phases[0].Limits); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimStep(ctx, ref); err != nil || !claimed {
		t.Fatalf("claim step: claimed=%v err=%v", claimed, err)
	}
	bridge := NewIssueLoopBridge(loopTestPool, cerebrodb.New(loopTestPool), db.New(loopTestPool), workflows.NewIssueLoopColumnStore(loopTestPool)).WithEvalBlockRunner(&waitingEvalBlockRunner{})
	taskContext, _ := json.Marshal(map[string]any{
		"type": "workflow_block", "workflow_target_issue_id": util.UUIDToString(issueID),
		"loop_step": map[string]any{"workflow_id": util.UUIDToString(recipe), "phase_id": "build", "block_id": "work", "step_number": 1},
	})
	NewChainTaskAdvancer(bridge, nil).AdvanceOnComplete(ctx, db.AgentTaskQueue{Context: taskContext, Result: json.RawMessage(`{"output":"done"}`)})
	steps, err := store.ListSteps(ctx, ref.PhaseRunKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Status != StepCompleted || steps[1].BlockID != "eval" || steps[1].Status != StepWaiting {
		t.Fatalf("completion did not advance to eval: %+v", steps)
	}
}

func TestChainTaskAdvancerBusyWakeupRetriesPendingStep(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	var workspaceID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id=$1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	chain := &Chain{Version: ChainVersion, Phases: []Phase{{
		ID: "build", Limits: PhaseLimits{MaxSteps: 2, MaxRounds: 2, NoProgressStalls: 2},
		Blocks: []Block{{ID: "work", Type: BlockSession, Skill: "build", OnAllBusy: BusyWakeup}},
	}}}
	raw, _ := json.Marshal(chain)
	recipe := seedIssueLoopRecipe(t, workspaceID, "busy wakeup chain", raw)
	store := NewStore(loopTestPool)
	ref := StepRef{PhaseRunKey: PhaseRunKey{IssueID: issueID, WorkflowID: recipe, PhaseID: "build"}, BlockID: "work", Number: 1}
	if _, _, err := store.OpenStep(ctx, ref, chain.Phases[0].Limits); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordStepOutcome(ctx, ref, StepPending, json.RawMessage(`{"all_agents_busy":true,"policy":"wakeup"}`)); err != nil {
		t.Fatal(err)
	}
	dispatcher := &retryingBlockDispatcher{calls: 1}
	bridge := NewIssueLoopBridge(loopTestPool, cerebrodb.New(loopTestPool), db.New(loopTestPool), workflows.NewIssueLoopColumnStore(loopTestPool))
	bridge.driver = NewChainDriver(store, dispatcher)
	d := BlockDispatch{Run: ChainRun{IssueID: issueID, WorkflowID: recipe}, Phase: chain.Phases[0], Block: chain.Phases[0].Blocks[0], Step: ChainStep{StepRef: ref}}
	taskContext, _ := json.Marshal(map[string]any{"type": "wakeup", "prompt": BusyWakeupPrompt(d)})

	NewChainTaskAdvancer(bridge, nil).AdvanceOnComplete(ctx, db.AgentTaskQueue{Context: taskContext})

	steps, err := store.ListSteps(ctx, ref.PhaseRunKey)
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher.calls != 2 || len(steps) != 1 || steps[0].Status != StepRunning {
		t.Fatalf("busy wakeup did not retry pending step: calls=%d steps=%+v", dispatcher.calls, steps)
	}
}
