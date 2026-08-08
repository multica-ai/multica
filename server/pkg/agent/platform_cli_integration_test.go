//go:build agentintegration

package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	platformSmokeWorkspaceID = "workspace-platform-smoke"
	platformSmokeAgentID     = "agent-platform-smoke"
	platformSmokeTaskID      = "task-platform-smoke"
	platformSmokeRuntimeID   = "runtime-platform-smoke"
)

// TestPlatformCLIIntegrationContextAwareRuntime drives the independently
// built CLI through Multica's built-in backend. The temporary working tree is
// the same boundary the daemon materializes for production task execution.
func TestPlatformCLIIntegrationContextAwareRuntime(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping external Platform Agent CLI smoke test in -short mode")
	}

	path := strings.TrimSpace(os.Getenv("MULTICA_PLATFORM_AGENT_CLI_PATH"))
	if path == "" {
		t.Fatal("MULTICA_PLATFORM_AGENT_CLI_PATH must name the explicitly built Platform Agent CLI")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("MULTICA_PLATFORM_AGENT_CLI_PATH must be absolute, got %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat Platform Agent CLI: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("Platform Agent CLI path is not a regular file: %s", path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("Platform Agent CLI path is not executable: %s", path)
	}

	versionCtx, cancelVersion := context.WithTimeout(context.Background(), 5*time.Second)
	versionOutput, err := exec.CommandContext(versionCtx, path, "--version").CombinedOutput()
	cancelVersion()
	if err != nil {
		t.Fatalf("Platform Agent CLI --version failed: %v (%s)", err, strings.TrimSpace(string(versionOutput)))
	}
	version := strings.TrimSpace(string(versionOutput))
	if err := CheckMinVersion("platform-agent-cli", version); err != nil {
		t.Fatalf("Platform Agent CLI version %q is not accepted: %v", version, err)
	}

	cwd := t.TempDir()
	writePlatformSmokeFile(t, cwd, "AGENTS.md", "You are the imported research lead. Use the attached skills and command context.\n")
	writePlatformSmokeFile(t, cwd, ".agent_context/skills/research/SKILL.md", "# Research\nCollect primary evidence.\n")
	writePlatformSmokeFile(t, cwd, ".agent_context/skills/review/SKILL.md", "# Review\nCheck every finding.\n")
	writePlatformSmokeFile(t, cwd, ".platform-agent/context.json", `{
  "schema_version": "platform-agent.runtime-context/v1",
  "extension": {
    "key": "research-team",
    "version": "2.0.0",
    "release_id": "release-platform-smoke",
    "digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  },
  "agent": { "source_key": "research-lead" },
  "commands": [
    {
      "name": "summarize",
      "description": "Summarize verified findings.",
      "content": "Return a concise evidence summary.",
      "metadata": { "format": "brief" }
    }
  ]
}`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend, err := New("platform-agent-cli", Config{
		ExecutablePath: path,
		CLIVersion:     version,
		Logger:         logger,
		Env: map[string]string{
			"PLATFORM_AGENT_MODE":  "mock",
			"MULTICA_WORKSPACE_ID": platformSmokeWorkspaceID,
			"MULTICA_AGENT_ID":     platformSmokeAgentID,
			"MULTICA_TASK_ID":      platformSmokeTaskID,
			"MULTICA_RUNTIME_ID":   platformSmokeRuntimeID,
		},
	})
	if err != nil {
		t.Fatalf("new Platform Agent backend: %v", err)
	}

	const prompt = "summarize the imported extension context"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, prompt, ExecOptions{
		Cwd:              cwd,
		Timeout:          25 * time.Second,
		HandshakeTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute Platform Agent CLI: %v", err)
	}

	messagesCh := make(chan []Message, 1)
	go func() {
		var messages []Message
		for message := range session.Messages {
			messages = append(messages, message)
		}
		messagesCh <- messages
	}()

	var result Result
	select {
	case result = <-session.Result:
	case <-ctx.Done():
		t.Fatal("timeout waiting for Platform Agent CLI result")
	}
	messages := <-messagesCh

	if result.Status != "completed" || result.Error != "" {
		t.Fatalf("Platform Agent CLI result = %+v", result)
	}
	wantOutput := fmt.Sprintf(
		"extension=research-team@2.0.0 release=release-platform-smoke digest=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef agent=research-lead skills=2 commands=1 input=%s",
		prompt,
	)
	if result.Output != wantOutput {
		t.Fatalf("output=%q, want %q", result.Output, wantOutput)
	}
	if result.SessionID == "" {
		t.Fatal("expected a non-empty app-server thread session ID")
	}

	textMessages := 0
	for _, message := range messages {
		switch message.Type {
		case MessageText:
			textMessages++
		case MessageToolUse, MessageToolResult:
			t.Fatalf("Platform Agent CLI emitted unexpected tool message: %+v", message)
		}
	}
	if textMessages == 0 {
		t.Fatal("expected at least one streamed text message")
	}
	t.Logf("Platform Agent CLI context smoke OK: version=%q session=%s output=%q", version, result.SessionID, result.Output)
}

func writePlatformSmokeFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
