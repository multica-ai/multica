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

// TestTraecliRealACPSmoke drives the real `traecli acp serve` binary end-to-end.
func TestTraecliRealACPSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("traecli")
	if err != nil {
		t.Skip("traecli not on PATH; skipping real-binary smoke test")
	}

	backend, err := New("traecli", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
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
			t.Fatalf("real traecli run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("expected real traecli output to contain 'pong', got %q", result.Output)
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty session id from real traecli")
		}
		t.Logf("real traecli smoke OK: session=%s output=%q", result.SessionID, result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real traecli result")
	}
}
