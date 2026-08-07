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

// TestPlatformAgentCLIRealCodexCompatibility drives an external Platform Agent
// CLI through Multica's production Codex backend and verifies the minimum
// app-server lifecycle used by custom runtime profiles.
func TestPlatformAgentCLIRealCodexCompatibility(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping external Platform Agent CLI smoke test in -short mode")
	}

	path := strings.TrimSpace(os.Getenv("MULTICA_PLATFORM_AGENT_CLI_PATH"))
	if path == "" {
		t.Skip("set MULTICA_PLATFORM_AGENT_CLI_PATH to the Platform Agent CLI executable")
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
	if !strings.Contains(strings.ToLower(version), "platform-agent-cli") {
		t.Fatalf("unexpected Platform Agent CLI version output: %q", version)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend, err := New("codex", Config{
		ExecutablePath: path,
		CLIVersion:     version,
		CodexVersion:   version,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("new Codex backend: %v", err)
	}

	const prompt = "multica source integration smoke"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, prompt, ExecOptions{
		Cwd:              t.TempDir(),
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

	if result.Status != "completed" {
		t.Fatalf("Platform Agent CLI run status=%q error=%q", result.Status, result.Error)
	}
	if result.Error != "" {
		t.Fatalf("Platform Agent CLI returned error %q", result.Error)
	}
	wantOutput := fmt.Sprintf("Mock Runtime 已收到任务：%s", prompt)
	if result.Output != wantOutput {
		t.Fatalf("output=%q, want %q", result.Output, wantOutput)
	}
	if result.SessionID == "" {
		t.Fatal("expected a non-empty Codex thread session ID")
	}

	textMessages := 0
	for _, message := range messages {
		switch message.Type {
		case MessageText:
			textMessages++
		case MessageToolUse, MessageToolResult:
			t.Fatalf("Phase 0 CLI emitted unexpected tool message: %+v", message)
		}
	}
	if textMessages == 0 {
		t.Fatal("expected at least one streamed text message")
	}
	t.Logf("Platform Agent CLI smoke OK: version=%q session=%s output=%q", version, result.SessionID, result.Output)
}
