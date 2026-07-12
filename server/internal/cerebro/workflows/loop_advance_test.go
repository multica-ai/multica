package workflows

import (
	"context"
	"encoding/json"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// planTaskWithContext builds a completed plan-phase task carrying the FIR-3052
// advance-hook context fields the advancer reads.
func planTaskWithContext(t *testing.T, phase, targetIssueID, from, to string) db.AgentTaskQueue {
	t.Helper()
	ctx := map[string]any{
		"loop_phase":               phase,
		"workflow_target_issue_id": targetIssueID,
	}
	if from != "" {
		ctx["loop_advance_from_status"] = from
	}
	if to != "" {
		ctx["loop_advance_to_status"] = to
	}
	raw, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal task context: %v", err)
	}
	return db.AgentTaskQueue{
		ID:      mustUUID("99999999-9999-9999-9999-999999999999"),
		Context: raw,
	}
}

const advTargetIssue = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

// TestAdvanceOnComplete_SkipsNonPlanPhase: a build (or any non-plan) completion
// must never touch the issue — the hook only fans plan -> build.
func TestAdvanceOnComplete_SkipsNonPlanPhase(t *testing.T) {
	fake := &fakeIssueActions{ParentIssue: db.Issue{Status: "todo"}}
	adv := NewLoopPhaseAdvancer(newServiceWithFake(fake))
	adv.AdvanceOnComplete(context.Background(), planTaskWithContext(t, "build", advTargetIssue, "todo", "in_progress"))
	if fake.UpdateStatusCalled {
		t.Fatal("build-phase completion must not advance status")
	}
}

// TestAdvanceOnComplete_SkipsWithoutAdvanceTo: a plan task with no target status
// (e.g. an old task from before this feature) is a no-op.
func TestAdvanceOnComplete_SkipsWithoutAdvanceTo(t *testing.T) {
	fake := &fakeIssueActions{ParentIssue: db.Issue{Status: "todo"}}
	adv := NewLoopPhaseAdvancer(newServiceWithFake(fake))
	adv.AdvanceOnComplete(context.Background(), planTaskWithContext(t, "plan", advTargetIssue, "todo", ""))
	if fake.UpdateStatusCalled {
		t.Fatal("plan completion without a target status must not advance")
	}
}

// TestAdvanceOnComplete_SkipsWhenAlreadyMoved: if the plan agent already flipped
// the status (current != from), the hook must not override it. This is the
// idempotency + no-override guard.
func TestAdvanceOnComplete_SkipsWhenAlreadyMoved(t *testing.T) {
	fake := &fakeIssueActions{ParentIssue: db.Issue{Status: "in_review"}}
	adv := NewLoopPhaseAdvancer(newServiceWithFake(fake))
	adv.AdvanceOnComplete(context.Background(), planTaskWithContext(t, "plan", advTargetIssue, "todo", "in_progress"))
	if fake.UpdateStatusCalled {
		t.Fatal("hook must not advance an issue already moved past the planning status")
	}
}

// TestAdvanceOnComplete_SkipsWhenAlreadyAtBuild: guard against a redundant flip
// when the issue is already on the build status.
func TestAdvanceOnComplete_SkipsWhenAlreadyAtBuild(t *testing.T) {
	fake := &fakeIssueActions{ParentIssue: db.Issue{Status: "in_progress"}}
	adv := NewLoopPhaseAdvancer(newServiceWithFake(fake))
	// From empty so the planning-status guard is skipped; the already-at-build
	// guard is the one under test.
	adv.AdvanceOnComplete(context.Background(), planTaskWithContext(t, "plan", advTargetIssue, "", "in_progress"))
	if fake.UpdateStatusCalled {
		t.Fatal("hook must not re-flip an issue already on the build status")
	}
}

// TestAdvanceOnComplete_NilSafe: an unwired advancer and an empty context must
// never panic (best-effort, post-commit contract).
func TestAdvanceOnComplete_NilSafe(t *testing.T) {
	var adv *LoopPhaseAdvancer
	adv.AdvanceOnComplete(context.Background(), db.AgentTaskQueue{})

	fake := &fakeIssueActions{}
	adv = NewLoopPhaseAdvancer(newServiceWithFake(fake))
	adv.AdvanceOnComplete(context.Background(), db.AgentTaskQueue{})
	if fake.UpdateStatusCalled {
		t.Fatal("empty-context task must not advance")
	}
}
