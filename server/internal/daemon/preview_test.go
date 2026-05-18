package daemon

import (
	"context"
	"log/slog"
	"runtime"
	"testing"
	"time"
)

func TestPreviewManagerStartReusesAndStops(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("preview process existence checks are platform-specific; covered on unix in CI/dev")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager := NewPreviewManager(t.TempDir(), "daemon-test", slog.New(slog.DiscardHandler))
	req := PreviewStartRequest{
		WorkspaceID: "workspace-1",
		IssueID:     "issue-1",
		CWD:         t.TempDir(),
		Command:     []string{"sh", "-c", "while true; do sleep 1; done"},
	}

	started, err := manager.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = manager.Stop(started.Preview.ID)
	})
	if started.Status != PreviewStartStatusStarted {
		t.Fatalf("Start() status = %q, want %q", started.Status, PreviewStartStatusStarted)
	}
	if started.Preview.Visibility != PreviewVisibilityPrivate {
		t.Fatalf("preview visibility = %q, want %q", started.Preview.Visibility, PreviewVisibilityPrivate)
	}
	if started.Preview.PID <= 0 {
		t.Fatalf("preview pid = %d, want positive", started.Preview.PID)
	}

	reused, err := manager.Start(ctx, req)
	if err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if reused.Status != PreviewStartStatusReused {
		t.Fatalf("second Start() status = %q, want %q", reused.Status, PreviewStartStatusReused)
	}
	if reused.Preview.ID != started.Preview.ID {
		t.Fatalf("reused preview id = %q, want %q", reused.Preview.ID, started.Preview.ID)
	}
	if reused.Preview.PID != started.Preview.PID {
		t.Fatalf("reused preview pid = %d, want %d", reused.Preview.PID, started.Preview.PID)
	}

	stopped, err := manager.Stop(started.Preview.ID)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if stopped.Status != PreviewStatusStopped {
		t.Fatalf("stopped status = %q, want %q", stopped.Status, PreviewStatusStopped)
	}
	if stopped.PID != 0 {
		t.Fatalf("stopped pid = %d, want 0", stopped.PID)
	}
}

func TestPreviewManagerStopAllStopsRunningPreviews(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("preview process existence checks are platform-specific; covered on unix in CI/dev")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager := NewPreviewManager(t.TempDir(), "daemon-test", slog.New(slog.DiscardHandler))
	baseReq := PreviewStartRequest{
		WorkspaceID: "workspace-1",
		CWD:         t.TempDir(),
		Command:     []string{"sh", "-c", "while true; do sleep 1; done"},
	}

	reqA := baseReq
	reqA.IssueID = "issue-a"
	startedA, err := manager.Start(ctx, reqA)
	if err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = manager.Stop(startedA.Preview.ID)
	})

	reqB := baseReq
	reqB.IssueID = "issue-b"
	startedB, err := manager.Start(ctx, reqB)
	if err != nil {
		t.Fatalf("Start(B) error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = manager.Stop(startedB.Preview.ID)
	})

	if startedA.Preview.ID == startedB.Preview.ID {
		t.Fatalf("preview ids should differ, got %q", startedA.Preview.ID)
	}
	if err := manager.StopAll(ctx); err != nil {
		t.Fatalf("StopAll() error = %v", err)
	}

	for _, started := range []PreviewStartResponse{startedA, startedB} {
		stopped, err := manager.Status(ctx, started.Preview.ID)
		if err != nil {
			t.Fatalf("Status(%s) error = %v", started.Preview.ID, err)
		}
		if stopped.Status != PreviewStatusStopped {
			t.Fatalf("preview %s status = %q, want %q", stopped.ID, stopped.Status, PreviewStatusStopped)
		}
		if stopped.PID != 0 {
			t.Fatalf("preview %s pid = %d, want 0", stopped.ID, stopped.PID)
		}
	}
}
