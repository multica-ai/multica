package daemon

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

type unconfirmedCleanupBackend struct{ omitResult bool }

func (b unconfirmedCleanupBackend) Execute(ctx context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	messages := make(chan agent.Message, 1)
	results := make(chan agent.Result, 1)
	messages <- agent.Message{Type: agent.MessageToolUse, Tool: "bash", CallID: "t"}
	go func() {
		<-ctx.Done()
		close(messages)
		if !b.omitResult {
			results <- agent.Result{Status: "cleanup_unconfirmed", Error: "Background processes may still be running"}
		}
		close(results)
	}()
	return &agent.Session{Messages: messages, Result: results, CleanupRequired: true, TerminalObserved: func() bool { return false }}, nil
}

func TestPiCleanupUnconfirmedOutranksCancellation(t *testing.T) {
	for _, manual := range []bool{false, true} {
		for _, omitted := range []bool{false, true} {
			d := newTestDaemon(t)
			d.cfg.AgentIdleWatchdog, d.cfg.AgentToolWatchdog = 50*time.Millisecond, 50*time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			if manual {
				time.AfterFunc(30*time.Millisecond, cancel)
			}
			result, _, err := d.executeAndDrain(ctx, unconfirmedCleanupBackend{omitResult: omitted}, "test", agent.ExecOptions{}, slog.Default(), "cleanup", "", new(atomic.Int32))
			cancel()
			if err != nil || result.Status != "cleanup_unconfirmed" {
				t.Fatalf("manual=%v omitted=%v result=%+v err=%v", manual, omitted, result, err)
			}
			// A drained task may still leave processes behind; even a later
			// empty shutdown drain must retain that uncertainty.
			var tasks sync.WaitGroup
			d.waitForTaskCleanup(&tasks, time.Second)
			if d.ShutdownCleanupConfirmed() {
				t.Fatal("shutdown forgot the provider's unconfirmed cleanup")
			}
			if shouldRetryWithFreshSession(result, "existing-session", 0, "pi") {
				t.Fatal("unconfirmed cleanup allowed an in-turn retry")
			}
		}
	}
}

func TestPiCleanupCancelAcknowledgement(t *testing.T) {
	for _, provider := range []string{"pi", "omp"} {
		failed := TaskResult{Status: "blocked", FailureReason: taskfailure.ReasonProcessCleanupUnconfirmed.String(), Comment: "Unconfirmed background process", BranchName: "work"}
		ack := taskCancelAcknowledgement(failed, nil, provider)
		if ack.ProcessCleanupConfirmed == nil || *ack.ProcessCleanupConfirmed || ack.FailureReason != failed.FailureReason || ack.ErrorMessage != failed.Comment || ack.BranchName != "work" {
			t.Fatalf("cancel acknowledgement lost cleanup failure: %+v", ack)
		}
		ack = taskCancelAcknowledgement(TaskResult{Status: "cancelled"}, nil, provider)
		if ack.ProcessCleanupConfirmed == nil || !*ack.ProcessCleanupConfirmed {
			t.Fatal("confirmed cleanup was not acknowledged")
		}
	}
}
