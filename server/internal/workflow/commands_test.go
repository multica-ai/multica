package workflow

import "testing"

func TestWorkflowCommandHashIsStableForTheSameBusinessRequest(t *testing.T) {
	first := workflowCommandHash("workflow_run.cancel", "run-1", struct {
		ExpectedRevision int64  `json:"expected_state_revision"`
		Reason           string `json:"reason"`
	}{ExpectedRevision: 7, Reason: ""})
	second := workflowCommandHash("workflow_run.cancel", "run-1", struct {
		ExpectedRevision int64  `json:"expected_state_revision"`
		Reason           string `json:"reason"`
	}{ExpectedRevision: 7, Reason: ""})
	if first == "" || first != second {
		t.Fatalf("same workflow command request must have a stable hash: %q != %q", first, second)
	}
}

func TestWorkflowCommandHashChangesWithTargetOrVersion(t *testing.T) {
	base := workflowCommandHash("workflow_run.retry", "run-1", map[string]any{"node": "draft", "expected_state_revision": int64(7)})
	otherRun := workflowCommandHash("workflow_run.retry", "run-2", map[string]any{"node": "draft", "expected_state_revision": int64(7)})
	otherRevision := workflowCommandHash("workflow_run.retry", "run-1", map[string]any{"node": "draft", "expected_state_revision": int64(8)})
	if base == otherRun || base == otherRevision {
		t.Fatal("workflow command hash must bind the idempotency key to its target and expected revision")
	}
}
