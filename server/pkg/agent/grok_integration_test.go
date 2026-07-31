//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestGrokRealACPSmoke drives the real `grok agent stdio` binary end-to-end.
func TestGrokRealACPSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("grok")
	if err != nil {
		t.Skip("grok not on PATH; skipping real-binary smoke test")
	}
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("grok CLI version: %s", strings.TrimSpace(string(version)))
	} else {
		t.Logf("grok CLI version unavailable: %v (%s)", err, strings.TrimSpace(string(version)))
	}

	backend, err := New("grok", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Reply with exactly one word: pong. Do not use any tools.", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 80 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("real grok run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("expected real grok output to contain 'pong', got %q", result.Output)
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty session id from real grok")
		}
		t.Logf("real grok smoke OK: session=%s output=%q", result.SessionID, result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real grok result")
	}
}
