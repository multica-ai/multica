//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestKimiRealACPUsageSmoke drives the real `kimi acp` binary end-to-end and
// asserts the task comes back with a token split.
//
// This is the test that would have caught #6448 before it shipped: kimi-code
// 0.33.0 reports no usage over ACP at all, so any fix validated only against a
// hand-written ACP fixture passes while the real runtime still reports nothing.
//
// The model matters: the CLI rejects thinking=off on some models with
// `400 invalid thinking`, and a turn that dies there writes no usage record.
// KIMI_SMOKE_MODEL overrides the default when the account's default model has
// that constraint.
func TestKimiRealACPUsageSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("kimi")
	if err != nil {
		t.Skip("kimi not on PATH; skipping real-binary smoke test")
	}
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("kimi CLI version: %s", strings.TrimSpace(string(version)))
	}

	backend, err := New("kimi", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Reply with exactly one word: alpha", ExecOptions{
		Timeout: 150 * time.Second,
		Model:   kimiSmokeModel(),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	t.Logf("status=%q error=%q output=%q", result.Status, result.Error, result.Output)
	if result.Status != "completed" {
		t.Fatalf("expected completed, got %q (%s)", result.Status, result.Error)
	}

	if len(result.Usage) == 0 {
		t.Fatal("no usage reported: the wire-log fallback did not fire")
	}
	var total TokenUsage
	for model, u := range result.Usage {
		t.Logf("usage[%s] = input:%d output:%d cacheRead:%d cacheWrite:%d",
			model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.CacheReadTokens += u.CacheReadTokens
		total.CacheWriteTokens += u.CacheWriteTokens
	}
	if total.InputTokens <= 0 {
		t.Errorf("input tokens = %d, want > 0", total.InputTokens)
	}
	if total.OutputTokens <= 0 {
		t.Errorf("output tokens = %d, want > 0", total.OutputTokens)
	}
}

func kimiSmokeModel() string {
	if model := strings.TrimSpace(os.Getenv("KIMI_SMOKE_MODEL")); model != "" {
		return model
	}
	return ""
}
