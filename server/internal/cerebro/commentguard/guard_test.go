package commentguard

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/handler"
)

type evaluatorFunc func(context.Context, workflows.HookEvent) (workflows.HookResult, error)

func (f evaluatorFunc) Evaluate(ctx context.Context, event workflows.HookEvent) (workflows.HookResult, error) {
	return f(ctx, event)
}

func TestEvaluateCommentUsesWorkflowAsOnlyDecision(t *testing.T) {
	var captured workflows.HookEvent
	service := New(evaluatorFunc(func(_ context.Context, event workflows.HookEvent) (workflows.HookResult, error) {
		captured = event
		return workflows.HookResult{
			Evaluated: true, Decision: workflows.HookModify,
			Modifications: map[string]any{"parent_id": "correct-thread"},
		}, nil
	}))
	result, err := service.EvaluateComment(context.Background(), handler.CommentWorkflowGateInput{
		WorkspaceID: pgtype.UUID{Valid: true}, AuthorType: "agent", AuthorID: "agent-1",
		IssueID: "issue-1", ParentID: "wrong-thread", Content: "No recipient",
		ThreadRequired: true, RequiredParentID: "correct-thread",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Allowed || result.ParentID != "correct-thread" {
		t.Fatalf("result = %#v, want Workflow modification", result)
	}
	message := captured.Context["message"].(map[string]any)
	if message["has_recipient"] != false || message["correct_thread"] != false {
		t.Fatalf("Workflow facts = %#v", message)
	}
}

func TestRecipientFactsExcludeIssueAndSelfMentions(t *testing.T) {
	if hasRecipient("[FIR-1](mention://issue/i) [Self](mention://agent/a)", "a") {
		t.Fatal("issue and self mentions must not count as recipients")
	}
	if !hasRecipient("[Other](mention://agent/b)", "a") {
		t.Fatal("another agent must count as a recipient")
	}
}
