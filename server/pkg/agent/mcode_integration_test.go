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

// TestMcodeRealACPContextAndToolSmoke drives an authenticated MiniMax Code ACP
// turn end-to-end. Workspace canaries exercise cwd-scoped instructions, project
// skill lookup, file access, and ACP tool event forwarding together.
func TestMcodeRealACPContextAndToolSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("mcode")
	if err != nil {
		t.Skip("mcode not on PATH; skipping real-binary smoke test")
	}
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("mcode --version: %s", strings.TrimSpace(string(version)))
	} else {
		t.Logf("mcode version unavailable: %v (%s)", err, strings.TrimSpace(string(version)))
	}

	workDir := t.TempDir()
	writeMcodeSmokeFile(t, filepath.Join(workDir, "AGENTS.md"), `# Multica smoke context

For every response in this workspace, include the exact marker AGENTS-MCODE-OK.
`)
	writeMcodeSmokeFile(t, filepath.Join(workDir, ".minimax", "skills", "multica-smoke", "SKILL.md"), `---
name: multica-smoke
description: Use when asked to run the Multica MiniMax Code integration smoke.
---
# Multica smoke skill

Read tool-canary.txt with a file-reading tool. Include the exact marker SKILL-MCODE-OK and the file contents in the final response.
`)
	writeMcodeSmokeFile(t, filepath.Join(workDir, "tool-canary.txt"), "TOOL-MCODE-OK\n")

	backend, err := New("mcode", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new mcode backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"Run the multica-smoke skill. Follow the workspace instructions and return its requested evidence.",
		ExecOptions{Cwd: workDir, Timeout: 210 * time.Second},
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var toolUses, toolResults int
	var canaryToolResult bool
	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for message := range session.Messages {
			switch message.Type {
			case MessageToolUse:
				toolUses++
			case MessageToolResult:
				toolResults++
				if strings.Contains(message.Output, "TOOL-MCODE-OK") {
					canaryToolResult = true
				}
			}
		}
	}()

	var result Result
	select {
	case result = <-session.Result:
	case <-time.After(240 * time.Second):
		t.Fatal("timeout waiting for real mcode result")
	}
	<-messagesDone

	if result.Status != "completed" {
		t.Fatalf("real mcode run did not complete: status=%q error=%q output=%q", result.Status, result.Error, result.Output)
	}
	for _, marker := range []string{"AGENTS-MCODE-OK", "SKILL-MCODE-OK", "TOOL-MCODE-OK"} {
		if !strings.Contains(result.Output, marker) {
			t.Fatalf("real mcode output missing %s: %q", marker, result.Output)
		}
	}
	if toolUses == 0 || toolResults == 0 || !canaryToolResult {
		t.Fatalf("real mcode stream did not expose canary file execution: uses=%d results=%d canary_result=%t", toolUses, toolResults, canaryToolResult)
	}
	if result.SessionID == "" {
		t.Fatal("real mcode run returned an empty session id")
	}
	t.Logf("real mcode smoke OK: session=%s tools=%d/%d output=%q", result.SessionID, toolUses, toolResults, result.Output)
}

func writeMcodeSmokeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s parent: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
