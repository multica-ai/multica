//go:build agentintegration

package execenv

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func realOpenclawBin(t *testing.T) string {
	t.Helper()
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to allow real agent CLI access")
	}
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	bin := os.Getenv("MULTICA_REAL_OPENCLAW_BIN")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("openclaw")
		if err != nil {
			t.Skip("openclaw not on PATH; skipping real-binary smoke test")
		}
	}
	return bin
}

func TestOpenclawDaemonEquivalentRealTask(t *testing.T) {
	bin := realOpenclawBin(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prepareCtx, prepareCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer prepareCancel()
	started := time.Now()
	environment, err := PrepareIsolated(prepareCtx, preparationHelperTestCommand(), PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "00000000-0000-4000-8000-000000000001",
		TaskID:         "00000000-0000-4000-8000-000000000002",
		AgentName:      "openclaw-real-smoke",
		Provider:       "openclaw",
		OpenclawBin:    bin,
		Task: TaskContextForEnv{
			IssueID: "openclaw-real-smoke",
		},
	}, logger)
	if err != nil {
		t.Fatalf("prepare isolated daemon-equivalent environment: %v", err)
	}
	t.Logf("real isolated daemon-equivalent preparation completed in %s", time.Since(started))

	agentEnv := map[string]string{
		"OPENCLAW_CONFIG_PATH": environment.OpenclawConfigPath,
	}
	if environment.OpenclawIncludeRoot != "" {
		roots := []string{environment.OpenclawIncludeRoot}
		if existing := strings.TrimSpace(os.Getenv("OPENCLAW_INCLUDE_ROOTS")); existing != "" {
			roots = append(roots, existing)
		}
		agentEnv["OPENCLAW_INCLUDE_ROOTS"] = strings.Join(roots, string(os.PathListSeparator))
	}
	backend, err := agent.ResolveBackend("openclaw", agent.Config{
		ExecutablePath: bin,
		Env:            agentEnv,
		Logger:         logger,
		TaskID:         "00000000-0000-4000-8000-000000000002",
		BuiltinRuntime: true,
	})
	if err != nil {
		t.Fatalf("resolve real OpenClaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	session, err := backend.Execute(ctx, "Reply with exactly: MULTICA_DAEMON_REAL_OK", agent.ExecOptions{
		Cwd:     environment.WorkDir,
		Model:   "main",
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("start real OpenClaw backend: %v", err)
	}
	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for range session.Messages {
		}
	}()
	result, ok := <-session.Result
	<-messagesDone
	if !ok {
		t.Fatal("real OpenClaw backend closed without a result")
	}
	if result.Status != "completed" {
		t.Fatalf("real OpenClaw task status = %q, error = %q", result.Status, result.Error)
	}
	if strings.TrimSpace(result.Output) != "MULTICA_DAEMON_REAL_OK" {
		t.Fatalf("real OpenClaw task output = %q", result.Output)
	}
	t.Logf("real daemon-equivalent task completed in %dms", result.DurationMs)
}
