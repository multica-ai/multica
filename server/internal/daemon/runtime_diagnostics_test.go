package daemon

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

type retryDiagnosticsBackend struct{}

func (retryDiagnosticsBackend) Execute(ctx context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	messages := make(chan agent.Message)
	result := make(chan agent.Result, 1)
	go func() {
		defer close(messages)
		defer close(result)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case messages <- agent.Message{Type: "diagnostic", Content: "Runtime retry; no progress"}:
				case <-ctx.Done():
				}
			case <-ctx.Done():
				result <- agent.Result{Status: "cancelled", Error: ctx.Err().Error()}
				return
			}
		}
	}()
	return &agent.Session{Messages: messages, Result: result}, nil
}

func TestRuntimeDiagnosticsAreVisibleWithoutResettingIdleWatchdog(t *testing.T) {
	d, recorder := newTranscriptRecorder(t)
	d.cfg.AgentIdleWatchdog = 60 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	result, _, err := d.executeAndDrain(ctx, retryDiagnosticsBackend{}, "p", agent.ExecOptions{}, slog.Default(), "diagnostic-watchdog", "", new(atomic.Int32))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "idle_watchdog" {
		t.Errorf("result status = %q; retry diagnostics must not postpone idle detection", result.Status)
	}
	got := recorder.snapshot()
	if len(got) == 0 || got[0].Type != "error" || got[0].Content != "Runtime retry; no progress" {
		t.Errorf("runtime diagnostics were not uploaded as visible error messages: %+v", got)
	}
}
