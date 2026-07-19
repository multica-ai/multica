package loops

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func seedIssueLoopRecipe(t *testing.T, workspaceID pgtype.UUID, name string, loopSpecJSON []byte) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := loopTestPool.QueryRow(context.Background(),
		`INSERT INTO cerebro_workflow (
			workspace_id, name, trigger_type, action_type, created_by_id, created_by_type,
			workflow_type, loop_spec
		) VALUES (
			$1, $2, 'status_changed', 'run_skill', gen_random_uuid(), 'member',
			'issue_loop', $3
		) RETURNING id`,
		workspaceID, name, loopSpecJSON,
	).Scan(&id); err != nil {
		t.Fatalf("seed issue-loop recipe: %v", err)
	}
	return id
}

// TestIssueLoopBridge_ProjectWideSyncMaterializesNoRules covers the Tine
// live-test finding: saving an Issue workflow recipe (the project-wide
// SyncIssueLoop path) must NOT materialize globally-firing rules — those
// fired on unrelated issues (e.g. an unscoped loop:planning-dispatch). A
// recipe is a per-issue template: only ActivateOnIssue materializes rules.
func TestIssueLoopBridge_ProjectWideSyncMaterializesNoRules(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)

	var workspaceID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id = $1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatalf("load issue workspace: %v", err)
	}

	loopSpecJSON, err := json.Marshal(testWaitingChain())
	if err != nil {
		t.Fatalf("marshal loop spec: %v", err)
	}

	recipe := seedIssueLoopRecipe(t, workspaceID, "project-wide sync recipe", loopSpecJSON)
	bridge := NewIssueLoopBridge(loopTestPool, cerebrodb.New(loopTestPool), db.New(loopTestPool), workflows.NewIssueLoopColumnStore(loopTestPool)).WithEvalBlockRunner(&waitingEvalBlockRunner{})

	// Project-wide sync (recipe save) must create zero generated rules.
	if err := bridge.SyncIssueLoop(ctx, workspaceID, recipe, pgtype.UUID{}, pgtype.UUID{}, "member", loopSpecJSON); err != nil {
		t.Fatalf("project-wide sync: %v", err)
	}
	var projectWide int
	if err := loopTestPool.QueryRow(ctx,
		`SELECT count(*) FROM cerebro_workflow WHERE generated_from_workflow_id = $1`,
		recipe,
	).Scan(&projectWide); err != nil {
		t.Fatalf("count generated rules: %v", err)
	}
	if projectWide != 0 {
		t.Fatalf("project-wide sync should materialize no rules, got %d", projectWide)
	}

	// Activation on a specific issue still materializes that issue's rules.
	if err := bridge.ActivateOnIssue(ctx, workspaceID, recipe, pgtype.UUID{}, recipe, issueID, "member", loopSpecJSON); err != nil {
		t.Fatalf("activate on issue: %v", err)
	}
	var perIssue int
	if err := loopTestPool.QueryRow(ctx,
		`SELECT count(*) FROM cerebro_workflow WHERE generated_from_workflow_id = $1 AND generated_for_issue_id = $2`,
		recipe, issueID,
	).Scan(&perIssue); err != nil {
		t.Fatalf("count per-issue rules: %v", err)
	}
	if perIssue == 0 {
		t.Fatal("activation on an issue should materialize that issue's rules")
	}
}

func TestIssueLoopBridge_ActivateOnIssueReplacesPriorRecipeForSameIssue(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)

	var workspaceID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id = $1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatalf("load issue workspace: %v", err)
	}

	loopSpecJSON, err := json.Marshal(testWaitingChain())
	if err != nil {
		t.Fatalf("marshal loop spec: %v", err)
	}

	firstRecipe := seedIssueLoopRecipe(t, workspaceID, "first issue loop recipe", loopSpecJSON)
	secondRecipe := seedIssueLoopRecipe(t, workspaceID, "second issue loop recipe", loopSpecJSON)

	bridge := NewIssueLoopBridge(loopTestPool, cerebrodb.New(loopTestPool), db.New(loopTestPool), workflows.NewIssueLoopColumnStore(loopTestPool)).WithEvalBlockRunner(&waitingEvalBlockRunner{})
	if err := bridge.ActivateOnIssue(ctx, workspaceID, firstRecipe, pgtype.UUID{}, firstRecipe, issueID, "member", loopSpecJSON); err != nil {
		t.Fatalf("activate first recipe: %v", err)
	}
	if err := bridge.ActivateOnIssue(ctx, workspaceID, secondRecipe, pgtype.UUID{}, secondRecipe, issueID, "member", loopSpecJSON); err != nil {
		t.Fatalf("activate second recipe: %v", err)
	}

	rows, err := loopTestPool.Query(ctx,
		`SELECT DISTINCT generated_from_workflow_id
		 FROM cerebro_workflow
		 WHERE generated_for_issue_id = $1
		 ORDER BY generated_from_workflow_id`,
		issueID,
	)
	if err != nil {
		t.Fatalf("list active recipes: %v", err)
	}
	defer rows.Close()

	var active []string
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan active recipe: %v", err)
		}
		active = append(active, util.UUIDToString(id))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate active recipes: %v", err)
	}

	if len(active) != 1 || active[0] != util.UUIDToString(secondRecipe) {
		t.Fatalf("issue should have only the latest active recipe, got %v; want %s", active, util.UUIDToString(secondRecipe))
	}
}

func testWaitingChain() *Chain {
	return &Chain{Version: ChainVersion, Phases: []Phase{{ID: "phase", Blocks: []Block{{ID: "gate", Type: BlockEval, EvalKey: "delivery"}}, Limits: PhaseLimits{MaxSteps: 3, MaxRounds: 3, NoProgressStalls: 1}}}}
}

func TestIssueLoopBridge_ResolveHumanBlockCompletesStep(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	var workspaceID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id=$1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	chain := &Chain{Version: ChainVersion, DoneStatus: "done", Phases: []Phase{{ID: "approval", Limits: PhaseLimits{MaxSteps: 2, MaxRounds: 2, NoProgressStalls: 1}, Blocks: []Block{{ID: "signoff", Type: BlockHuman, Prompt: "Approve", ApproverType: AssigneeMember, ApproverID: uuid.NewString()}}}}}
	raw, _ := json.Marshal(chain)
	recipe := seedIssueLoopRecipe(t, workspaceID, "human chain", raw)
	store := NewStore(loopTestPool)
	ref := StepRef{PhaseRunKey: PhaseRunKey{IssueID: issueID, WorkflowID: recipe, PhaseID: "approval"}, BlockID: "signoff", Number: 1}
	if _, _, err := store.OpenStep(ctx, ref, chain.Phases[0].Limits); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimStep(ctx, ref); err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if err := store.RecordStepOutcome(ctx, ref, StepWaiting, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	bridge := NewIssueLoopBridge(loopTestPool, cerebrodb.New(loopTestPool), db.New(loopTestPool), workflows.NewIssueLoopColumnStore(loopTestPool))
	if err := bridge.ResolveHumanBlock(ctx, recipe, issueID, "signoff", true, "ok"); err != nil {
		t.Fatal(err)
	}
	steps, err := store.ListSteps(ctx, ref.PhaseRunKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Status != StepCompleted {
		t.Fatalf("steps=%+v", steps)
	}
}

func TestIssueLoopBridge_ResumeActiveIssueLoopsStartsCutoverMarker(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	var workspaceID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id=$1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	chain := testWaitingChain()
	raw, _ := json.Marshal(chain)
	recipe := seedIssueLoopRecipe(t, workspaceID, "cutover resume", raw)
	if _, err := loopTestPool.Exec(ctx, `INSERT INTO cerebro_workflow (workspace_id,name,enabled,trigger_type,trigger_config,conditions,action_type,action_config,editor_mode,editor_layout,created_by_id,created_by_type,generated_from_workflow_id,generated_for_issue_id) VALUES ($1,'loop:chain-activation',false,'status_changed','{"to_status":"__chain_driver__"}','[]','set_status','{"status":"done"}','form','null',gen_random_uuid(),'member',$2,$3)`, workspaceID, recipe, issueID); err != nil {
		t.Fatal(err)
	}
	bridge := NewIssueLoopBridge(loopTestPool, cerebrodb.New(loopTestPool), db.New(loopTestPool), workflows.NewIssueLoopColumnStore(loopTestPool)).WithEvalBlockRunner(&fakeEvalBlockRunner{})
	if err := bridge.ResumeActiveIssueLoops(ctx); err != nil {
		t.Fatal(err)
	}
	steps, err := NewStore(loopTestPool).ListSteps(ctx, PhaseRunKey{IssueID: issueID, WorkflowID: recipe, PhaseID: "phase"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Status != StepCompleted {
		t.Fatalf("steps=%+v", steps)
	}
}
