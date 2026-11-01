package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// A backend run that ends "cancelled" because its context went away splits two
// ways: the daemon is shutting down (#9773) — the main path under rollout
// restarts, which must stay retryable like the hard-kill runtime_recovery path
// — or the cancellation is defensive (the server-side cancel was already
// discarded by handleTask's cancelledByPoll branch), which keeps the plain
// "cancelled" terminal.
func TestCancelledRunOutcomeSplitsDaemonShutdownFromDefensiveCancel(t *testing.T) {
	sessionID, workDir, envRoot := "session-1", "/work/dir", "/env/root"

	shutdownCtx, cancelShutdown := context.WithCancel(context.Background())
	cancelShutdown()

	for _, tt := range []struct {
		name              string
		ctx               context.Context
		wantStatus        string
		wantFailureReason string
		wantComment       string
	}{
		{name: "daemon shutdown reports retryable runtime_recovery", ctx: shutdownCtx, wantStatus: "failed", wantFailureReason: "runtime_recovery", wantComment: "daemon shut down while the task was in flight"},
		{name: "defensive cancellation keeps the plain cancelled terminal", ctx: context.Background(), wantStatus: "cancelled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cancelledRunOutcome(tt.ctx, sessionID, workDir, envRoot, nil)
			if got := ok; got != (tt.wantStatus == "failed") {
				t.Fatalf("shutdown detected = %v, want %v", got, tt.wantStatus == "failed")
			}
			if got.Status != tt.wantStatus {
				t.Fatalf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.FailureReason != tt.wantFailureReason {
				t.Fatalf("FailureReason = %q, want %q", got.FailureReason, tt.wantFailureReason)
			}
			if got.Comment != tt.wantComment {
				t.Fatalf("Comment = %q, want %q", got.Comment, tt.wantComment)
			}
			if got.SessionID != sessionID || got.WorkDir != workDir || got.EnvRoot != envRoot {
				t.Fatalf("resume state lost: %+v", got)
			}
		})
	}
}

// TestRunTaskReportsDaemonShutdownCancellationAsRetryableRecovery drives a
// real runTask whose backend is killed by the daemon's context chain going
// away (a graceful shutdown). The old path reported the run as bare
// "cancelled" — failed with no retry even though max_attempts allows one
// (#9773); it must now land on runtime_recovery, the retryable reason orphan
// recovery already uses for a daemon that died mid-task.
func TestRunTaskReportsDaemonShutdownCancellationAsRetryableRecovery(t *testing.T) {
	t.Parallel()

	// A fake provider binary that never finishes: the run's only way out is
	// the context cancellation under test.
	testDir := t.TempDir()
	fakeBin := filepath.Join(testDir, "claude")
	writeTestExecutable(t, fakeBin, []byte("#!/bin/sh\ncat >/dev/null\nsleep 30\n"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         logger,
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-shutdown": {ID: "rt-shutdown", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			AgentTimeout:   30 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents:         map[string]AgentEntry{"claude": {Path: fakeBin}},
		},
	}

	parentCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()

	result, err := d.runTask(parentCtx, leaderReuseTestTask("task-shutdown"), "claude", 0, logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.Status != "failed" || result.FailureReason != taskfailure.ReasonRuntimeRecovery.String() {
		t.Fatalf("shutdown cancel reported as status=%q failure_reason=%q, want failed/%s",
			result.Status, result.FailureReason, taskfailure.ReasonRuntimeRecovery)
	}
	if result.Comment == "" {
		t.Fatal("shutdown cancellation must carry a non-empty error")
	}
}
