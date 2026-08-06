package loops

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type recordingHookEvaluator struct {
	mu     sync.Mutex
	events []workflows.HookEvent
}

func (e *recordingHookEvaluator) Evaluate(_ context.Context, event workflows.HookEvent) (workflows.HookResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
	return workflows.HookResult{Evaluated: true, Decision: workflows.HookAllow}, nil
}

func (e *recordingHookEvaluator) count(eventType workflows.HookEventType) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	count := 0
	for _, event := range e.events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func TestChainWorkflowGateRequiresFinalApprovalBeforeDone(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	var workspaceID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id=$1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	approverID := uuid.NewString()
	chain := &Chain{
		Version: ChainVersion, DoneStatus: "done",
		Phases: []Phase{{
			ID: "approval", Limits: PhaseLimits{MaxSteps: 2, MaxRounds: 2, NoProgressStalls: 1},
			Blocks: []Block{{
				ID: "signoff", Type: BlockHuman, Prompt: "Approve delivery",
				ApproverType: AssigneeMember, ApproverID: approverID,
			}},
		}},
	}
	raw, err := json.Marshal(chain)
	if err != nil {
		t.Fatal(err)
	}
	recipe := seedIssueLoopRecipe(t, workspaceID, "Workflow status gate", raw)
	hooks := &recordingHookEvaluator{}
	bridge := NewIssueLoopBridge(
		loopTestPool, cerebrodb.New(loopTestPool), db.New(loopTestPool),
		workflows.NewIssueLoopColumnStore(loopTestPool),
	).WithHooks(hooks)

	if err := bridge.ActivateOnIssue(ctx, workspaceID, recipe, pgtype.UUID{}, recipe, issueID, "member", raw); err != nil {
		t.Fatal(err)
	}
	issue, err := db.New(loopTestPool).GetIssue(ctx, issueID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.BeforeIssueStatusChange(ctx, issue, "done", "member", approverID); err == nil || !strings.Contains(err.Error(), finalStepApprovalRequirement) {
		t.Fatalf("unapproved Done error = %v, want final approval requirement", err)
	}

	if err := bridge.ResolveHumanBlock(ctx, recipe, issueID, "signoff", true, "Approved", approverID); err != nil {
		t.Fatal(err)
	}
	issue, err = db.New(loopTestPool).GetIssue(ctx, issueID)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Status != "done" {
		t.Fatalf("issue status = %q, want done after approval", issue.Status)
	}
	if hooks.count(workflows.HookBeforeIssueStatus) < 2 {
		t.Fatalf("status hook count = %d, want rejected and approved attempts", hooks.count(workflows.HookBeforeIssueStatus))
	}
	if hooks.count(workflows.HookAfterWorkflowStep) != 1 {
		t.Fatalf("step hook count = %d, want 1", hooks.count(workflows.HookAfterWorkflowStep))
	}
}
