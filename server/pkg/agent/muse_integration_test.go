//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMuseRealMSPSmoke drives an authenticated Meta Muse Code MSP turn
// end-to-end. It proves the three things a fake cannot: the handshake shape
// Muse actually emits, that approvalMode=allowAll lets a tool run without a
// human, and that the workspace AGENTS.md reaches the model.
func TestMuseRealMSPSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("muse")
	if err != nil {
		t.Skip("muse not on PATH; skipping real-binary smoke test")
	}
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("muse --version: %s", strings.TrimSpace(string(version)))
	} else {
		t.Logf("muse version unavailable: %v (%s)", err, strings.TrimSpace(string(version)))
	}

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte(`# Multica smoke context

For every response in this workspace, include the exact marker AGENTS-MUSE-OK.
`), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "tool-canary.txt"), []byte("TOOL-MUSE-OK\n"), 0o644); err != nil {
		t.Fatalf("write tool canary: %v", err)
	}

	backend, err := New("muse", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new muse backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"Read tool-canary.txt with a shell or file tool, then reply with its contents and the workspace marker.",
		ExecOptions{Cwd: workDir, Timeout: 210 * time.Second},
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var sawToolUse, sawToolResult bool
	for msg := range session.Messages {
		switch msg.Type {
		case MessageToolUse:
			sawToolUse = true
		case MessageToolResult:
			sawToolResult = true
		}
	}

	var result Result
	for res := range session.Result {
		result = res
	}
	t.Logf("status=%s session=%s duration=%dms error=%q output=%q", result.Status, result.SessionID, result.DurationMs, result.Error, result.Output)

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (error %q)", result.Status, result.Error)
	}
	if result.SessionID == "" {
		t.Fatal("no session id returned; resume would be impossible")
	}
	if !strings.Contains(result.Output, "AGENTS-MUSE-OK") {
		t.Errorf("output did not carry the workspace marker: %q", result.Output)
	}
	if !strings.Contains(result.Output, "TOOL-MUSE-OK") {
		t.Errorf("output did not carry the tool canary; a skipped tool would mean approval or sandbox blocked the run: %q", result.Output)
	}
	if !sawToolUse || !sawToolResult {
		t.Errorf("tool events missing (use=%v result=%v); the item stream did not reach the board", sawToolUse, sawToolResult)
	}
	if usage, ok := result.Usage["muse-wonder-full-1.0"]; ok && usage.InputTokens == 0 {
		t.Errorf("usage reported a zero input count: %+v", usage)
	}
}
