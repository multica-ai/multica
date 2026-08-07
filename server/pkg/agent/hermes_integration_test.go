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

// TestHermesRealACPUsageSmoke drives the real `hermes acp` binary end-to-end
// and verifies its metering survives the shared ACP reconciliation path.
func TestHermesRealACPUsageSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("hermes")
	if err != nil {
		t.Skip("hermes not on PATH; skipping real-binary smoke test")
	}
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("hermes CLI version: %s", strings.TrimSpace(string(version)))
	}

	backend, err := New("hermes", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Reply with exactly one word: alpha. Do not use any tools.", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 150 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	t.Logf("status=%q error=%q output=%q session=%q", result.Status, result.Error, result.Output, result.SessionID)
	if result.Status != "completed" {
		t.Fatalf("expected completed, got %q (%s)", result.Status, result.Error)
	}
	if !strings.Contains(strings.ToLower(result.Output), "alpha") {
		t.Fatalf("expected output to contain alpha, got %q", result.Output)
	}
	if result.SessionID == "" {
		t.Error("expected a non-empty session id")
	}
	if len(result.Usage) == 0 {
		t.Fatal("no usage reported by the real Hermes ACP run")
	}

	var total TokenUsage
	for model, usage := range result.Usage {
		t.Logf("usage[%s] = input:%d output:%d cacheRead:%d cacheWrite:%d costTicks:%d",
			model,
			usage.InputTokens,
			usage.OutputTokens,
			usage.CacheReadTokens,
			usage.CacheWriteTokens,
			usage.CostUSDTicks,
		)
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		total.CacheReadTokens += usage.CacheReadTokens
		total.CacheWriteTokens += usage.CacheWriteTokens
	}
	if total.InputTokens <= 0 {
		t.Errorf("input tokens = %d, want > 0", total.InputTokens)
	}
	if total.OutputTokens <= 0 {
		t.Errorf("output tokens = %d, want > 0", total.OutputTokens)
	}
}
