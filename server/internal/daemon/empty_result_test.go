package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestPromoteEmptyAgentResultFailure(t *testing.T) {
	t.Parallel()

	got, changed := promoteEmptyAgentResultFailure(agent.Result{Status: "completed"}, 0)
	if !changed {
		t.Fatal("silent completed result was not promoted to failed")
	}
	if got.Status != "failed" {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.Error != emptyAgentResultError {
		t.Fatalf("error = %q, want %q", got.Error, emptyAgentResultError)
	}
	if reason := taskfailure.Classify(got.Error); reason != taskfailure.ReasonAgentEmptyOrUnparseableOutput {
		t.Fatalf("failure reason = %q, want %q", reason, taskfailure.ReasonAgentEmptyOrUnparseableOutput)
	}
}

func TestPromoteEmptyAgentResultFailureRequiresAllSignals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result agent.Result
		tools  int32
	}{
		{name: "text output", result: agent.Result{Status: "completed", Output: "done"}},
		{name: "usage map", result: agent.Result{Status: "completed", Usage: map[string]agent.TokenUsage{"k3": {}}}},
		{name: "tool activity", result: agent.Result{Status: "completed"}, tools: 1},
		{name: "already failed", result: agent.Result{Status: "failed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, changed := promoteEmptyAgentResultFailure(tt.result, tt.tools)
			if changed {
				t.Fatalf("result was promoted: %+v", got)
			}
			if got.Status != tt.result.Status || got.Error != tt.result.Error {
				t.Fatalf("result changed from %+v to %+v", tt.result, got)
			}
		})
	}
}

func TestPromoteEmptyAgentResultFailurePreservesExistingError(t *testing.T) {
	t.Parallel()

	got, changed := promoteEmptyAgentResultFailure(agent.Result{
		Status: "completed",
		Error:  "provider detail",
	}, 0)
	if !changed {
		t.Fatal("silent completed result with an existing error was not promoted")
	}
	if got.Status != "failed" || got.Error != "provider detail" {
		t.Fatalf("promotion changed the existing error: %+v", got)
	}
}
