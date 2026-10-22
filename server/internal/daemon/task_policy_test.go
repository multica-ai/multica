package daemon

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskpolicy"
)

func TestExecuteAndDrainRequiresTaskOwnedBrainAcceptanceEvaluator(t *testing.T) {
	policy, err := taskpolicy.NewBrainMCPAcceptance(taskpolicy.Request{
		Mode: taskpolicy.BrainMCPAcceptanceMode, MCPServer: "brain",
		ProjectIDs: []string{"brain", "multica"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, restricted := range []bool{false, true} {
		d := newTestDaemon(t)
		backend := &fakeBackend{results: []agent.Result{{Status: "completed"}}}
		opts := agent.ExecOptions{}
		if restricted {
			opts.ExecutionPolicy = policy
			opts.EvaluateBrainAcceptance = func(agent.BrainAcceptanceEvaluation) error { return nil }
		}
		if _, _, err := d.executeAndDrain(context.Background(), backend, "synthetic", opts, slog.Default(), "synthetic-task", "", new(atomic.Int32)); err != nil {
			t.Fatal(err)
		}
		evaluate := backend.calls[0].EvaluateBrainAcceptance
		if restricted {
			if evaluate == nil || evaluate(agent.BrainAcceptanceEvaluation{}) == nil {
				t.Fatal("restricted execution did not require the task-owned frozen evaluator")
			}
		} else if evaluate != nil {
			t.Fatal("ordinary execution gained an acceptance evaluator")
		}
	}
}
