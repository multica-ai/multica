//go:build agentintegration

package agent

import (
	"context"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestKimiRealThinkingSmoke verifies both halves of the live Kimi contract:
// model-specific effort discovery and the real ACP
// session/set_config_option -> session/prompt execution path.
func TestKimiRealThinkingSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in short mode")
	}
	path, err := exec.LookPath("kimi")
	if err != nil {
		t.Skip("kimi not on PATH; skipping real-binary smoke test")
	}

	discoveryCtx, cancelDiscovery := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelDiscovery()
	models, err := discoverKimiModels(discoveryCtx, path)
	if err != nil {
		t.Fatalf("discover real Kimi models: %v", err)
	}
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	k3 := byID["kimi-code/k3"]
	if k3.Thinking == nil || !hasThinkingLevel(k3.Thinking, "low") ||
		!hasThinkingLevel(k3.Thinking, "high") || !hasThinkingLevel(k3.Thinking, "max") {
		t.Fatalf("real K3 effort catalog = %+v, want low/high/max", k3.Thinking)
	}
	if coding := byID["kimi-code/kimi-for-coding"]; coding.Thinking != nil {
		t.Fatalf("kimi-for-coding unexpectedly advertises effort overrides: %+v", coding.Thinking)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend, err := New("kimi", Config{ExecutablePath: path, Logger: logger})
	if err != nil {
		t.Fatalf("new Kimi backend: %v", err)
	}
	runCtx, cancelRun := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelRun()
	session, err := backend.Execute(runCtx,
		"Reply with exactly one word: pong. Do not use any tools.",
		ExecOptions{
			Cwd:           t.TempDir(),
			Model:         "kimi-code/k3",
			ThinkingLevel: "low",
			Timeout:       100 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("execute real Kimi turn: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("real Kimi run status = %q, error = %q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("real Kimi output did not contain pong: %q", result.Output)
		}
		if result.SessionID == "" {
			t.Fatal("real Kimi run returned no session id")
		}
		t.Log("real Kimi ACP thinking smoke passed with model=kimi-code/k3 effort=low")
	case <-time.After(120 * time.Second):
		t.Fatal("timeout waiting for real Kimi result")
	}
}
