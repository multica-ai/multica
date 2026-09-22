package workflow

import "testing"

func TestWorkItemRequestHashDependsOnDecisionBodyNotIdempotencyKey(t *testing.T) {
	base := WorkItemSubmission{Action: "approve", Values: map[string]any{"final_text": "ready"}}
	first := workItemRequestHash("run-1", "item-1", base)
	if first == "" {
		t.Fatal("work item request hash must not be empty")
	}
	if got := workItemRequestHash("run-1", "item-1", base); got != first {
		t.Fatalf("same work item request hash = %q, want %q", got, first)
	}
	changedAction := base
	changedAction.Action = "rework"
	if got := workItemRequestHash("run-1", "item-1", changedAction); got == first {
		t.Fatal("different work item decision must have a different request hash")
	}
	changedTarget := base
	if got := workItemRequestHash("run-2", "item-1", changedTarget); got == first {
		t.Fatal("different work item target must have a different request hash")
	}
}
