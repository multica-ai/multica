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

// FIR-3308: the unbacked-promise check rejects a comment that says the agent
// will carry on while no wakeup is scheduled to make that happen. The text is a
// verbatim real comment (FIR-3387) that promised an automatic continuation and
// then went 25 hours with no follow-up until a human chased it.
func TestUnbackedPromiseRejectedWithoutWakeup(t *testing.T) {
	g := New(fakeFlags{flags: map[string]bool{FlagNoUnbackedPromise: true}})
	ws := testWorkspaceID(t)

	content := memberMention + " Ikke live endnu: konflikten er løst, DM-kontrollerne er grønne, " +
		"og GitHub kører den sidste kontrol. Fortsættelsen er planlagt automatisk; derefter lægger " +
		"jeg ændringen live og verificerer den."

	msg, ok := g.RejectComment(context.Background(), ws, "agent", authorAgentID, content, false, nil, false, false)
	if ok {
		t.Fatalf("a promised continuation with no wakeup must be rejected")
	}
	if msg != UnbackedPromiseMessage {
		t.Fatalf("wrong rejection message: %q", msg)
	}
}

// The same comment passes once the agent has actually scheduled the wakeup: the
// promise is then backed by something that will deliver it.
func TestUnbackedPromiseAllowedWithWakeup(t *testing.T) {
	g := New(fakeFlags{flags: map[string]bool{FlagNoUnbackedPromise: true}})
	ws := testWorkspaceID(t)

	content := memberMention + " Jeg fortsætter med den sidste kontrol."

	if _, ok := g.RejectComment(context.Background(), ws, "agent", authorAgentID, content, false, nil, false, true /* active wakeup */); !ok {
		t.Fatalf("a promise backed by an active wakeup must pass")
	}
}

// A delivered result is not a promise, so it passes with no wakeup. This is the
// shape the rule is meant to encourage.
func TestDeliveredResultIsNotAPromise(t *testing.T) {
	g := New(fakeFlags{flags: map[string]bool{FlagNoUnbackedPromise: true}})
	ws := testWorkspaceID(t)

	content := memberMention + " Worker-buildet virker igen i production — du behøver ikke gøre mere. " +
		"Verificeret: kontrollerne var grønne og den nye version er aktiv."

	if _, ok := g.RejectComment(context.Background(), ws, "agent", authorAgentID, content, false, nil, false, false); !ok {
		t.Fatalf("a delivered, verified result must pass")
	}
}

// A next step that belongs to the reader must not trip the guard: the agent is
// not the one who has to act, so no wakeup is owed. Verbatim from FIR-3371.
func TestReaderOwnedNextStepIsNotAPromise(t *testing.T) {
	g := New(fakeFlags{flags: map[string]bool{FlagNoUnbackedPromise: true}})
	ws := testWorkspaceID(t)

	content := memberMention + " Det eneste næste skridt er at skrive `approve` på FIR-3372. " +
		"Derefter kan den samlede AI CFO-test køres i production."

	if _, ok := g.RejectComment(context.Background(), ws, "agent", authorAgentID, content, false, nil, false, false); !ok {
		t.Fatalf("a next step owned by the reader must not be treated as an agent promise")
	}
}

// Default OFF: with no flag row the check never fires, so production behaviour
// is unchanged until an admin turns it on.
func TestUnbackedPromiseFlagOffAllows(t *testing.T) {
	g := New(fakeFlags{flags: map[string]bool{}})
	ws := testWorkspaceID(t)

	content := memberMention + " Jeg fortsætter automatisk."

	if _, ok := g.RejectComment(context.Background(), ws, "agent", authorAgentID, content, false, nil, false, false); !ok {
		t.Fatalf("the check must not fire while its flag is off")
	}
}

// Member-authored comments are never gated, even by the new check.
func TestUnbackedPromiseNeverGatesMembers(t *testing.T) {
	g := New(fakeFlags{flags: map[string]bool{FlagNoUnbackedPromise: true}})
	ws := testWorkspaceID(t)

	if _, ok := g.RejectComment(context.Background(), ws, "member", "", "Jeg fortsætter i morgen.", false, nil, false, false); !ok {
		t.Fatalf("member comments must never be gated")
	}
}
