package daemon

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestNewSingleTaskRunner_BuildsWithoutRegistration(t *testing.T) {
	t.Setenv("MULTICA_TOKEN", "mul_single_task_test")

	cfg := Config{
		ServerBaseURL:  "http://example.invalid",
		WorkspacesRoot: t.TempDir(),
		HealthPort:     0, // OS-picked; constructor binds it.
	}

	r, err := NewSingleTaskRunner(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewSingleTaskRunner: %v", err)
	}
	defer r.Close()

	if r.client.Token() != "mul_single_task_test" {
		t.Fatalf("token not loaded from env, got %q", r.client.Token())
	}
	if r.HealthPort() == 0 {
		t.Fatalf("expected health port to be bound, got 0")
	}
}

func TestNewSingleTaskRunner_SeedsRuntimeIndex(t *testing.T) {
	t.Setenv("MULTICA_TOKEN", "tk")
	cfg := Config{ServerBaseURL: "http://example.invalid", WorkspacesRoot: t.TempDir()}
	r, err := NewSingleTaskRunner(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	r.SeedRuntime("rt-1", "claude")
	r.mu.Lock()
	got := r.runtimeIndex["rt-1"].Provider
	r.mu.Unlock()
	if got != "claude" {
		t.Fatalf("runtimeIndex not seeded, got provider %q", got)
	}
}

func TestRunOneTask_HappyPath(t *testing.T) {
	srv := startStubAPIServer(t, stubAPIBehavior{taskStatus: "in_progress"})
	defer srv.Close()
	t.Setenv("MULTICA_TOKEN", "tk")

	cfg := Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		Agents: map[string]AgentEntry{
			"claude": {Path: "/bin/true"}, // never invoked when r.runner is stubbed
		},
		AgentTimeout: 10 * time.Second,
	}
	r, err := NewSingleTaskRunner(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// Keep handleTask's cancellation watcher from hammering the stub.
	r.cancelPollInterval = time.Hour

	task := Task{
		ID:          "t1",
		RuntimeID:   "rt-1",
		IssueID:     "i1",
		WorkspaceID: "w1",
		Agent:       &AgentData{Name: "Lambda"},
	}
	r.SeedRuntime(task.RuntimeID, "claude")

	// Bypass the real agent spawn; assert handleTask drives the lifecycle.
	r.runner = taskRunnerFunc(func(ctx context.Context, t Task, provider string, slot int, log *slog.Logger) (TaskResult, error) {
		return TaskResult{Status: "completed", Comment: "ok", BranchName: "feat/x", SessionID: "s1", WorkDir: "/tmp/wd"}, nil
	})

	if err := r.RunOneTask(context.Background(), task); err != nil {
		t.Fatalf("RunOneTask: %v", err)
	}

	if !srv.sawComplete("t1") {
		t.Fatalf("expected CompleteTask call for t1, got: %v", srv.calls())
	}
}

func TestRunOneTask_RequiresWorkspaceID(t *testing.T) {
	t.Setenv("MULTICA_TOKEN", "tk")
	cfg := Config{ServerBaseURL: "http://example.invalid", WorkspacesRoot: t.TempDir()}
	r, err := NewSingleTaskRunner(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	err = r.RunOneTask(context.Background(), Task{ID: "t1", RuntimeID: "rt-1"})
	if err == nil || !contains(err.Error(), "WorkspaceID required") {
		t.Fatalf("expected WorkspaceID required error, got: %v", err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
