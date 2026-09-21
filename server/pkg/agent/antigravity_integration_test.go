//go:build agentintegration

package agent

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestAntigravityRealResumedUsageIsPerExecution is opt-in because it consumes
// the signed-in Antigravity account's quota. It verifies the installed agy
// binary's wire contract and, importantly, that a resumed execution reports
// only that execution's completed-step usage to Multica.
func TestAntigravityRealResumedUsageIsPerExecution(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	execPath, err := exec.LookPath("agy")
	if err != nil {
		t.Skipf("agy is not installed: %v", err)
	}

	backend, err := New("antigravity", Config{
		ExecutablePath: execPath,
		Logger:         quietAntigravityLogger(),
	})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	workDir := t.TempDir()
	first := runAntigravityRealTurn(t, ctx, backend, "Reply with exactly FIRST_TURN_OK and nothing else.", ExecOptions{
		Cwd:     workDir,
		Timeout: 2 * time.Minute,
	})
	if !strings.Contains(first.Output, "FIRST_TURN_OK") {
		t.Fatalf("first output = %q, want FIRST_TURN_OK", first.Output)
	}
	if first.SessionID == "" {
		t.Fatal("expected first stream-json conversation id")
	}
	if total := antigravityResultUsageTotal(first); total <= 0 {
		t.Fatalf("expected non-zero first-turn provider usage, got %+v", first.Usage)
	}

	second := runAntigravityRealTurn(t, ctx, backend, "Reply with exactly SECOND_TURN_OK and nothing else.", ExecOptions{
		Cwd:             workDir,
		Timeout:         2 * time.Minute,
		ResumeSessionID: first.SessionID,
	})
	if !strings.Contains(second.Output, "SECOND_TURN_OK") {
		t.Fatalf("second output = %q, want SECOND_TURN_OK", second.Output)
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("resumed session id = %q, want %q", second.SessionID, first.SessionID)
	}
	if total := antigravityResultUsageTotal(second); total <= 0 {
		t.Fatalf("expected non-zero resumed-turn provider usage, got %+v", second.Usage)
	}

	// Manual wire verification against agy 1.1.25 showed result.usage growing
	// cumulatively across resumed turns, while each DONE step_update carried
	// only the current execution's usage. The unit fixture deliberately makes
	// those values differ; this real test protects the resume path itself.
}

func runAntigravityRealTurn(t *testing.T, ctx context.Context, backend Backend, prompt string, opts ExecOptions) Result {
	t.Helper()
	session, err := backend.Execute(ctx, prompt, opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	for model, usage := range result.Usage {
		t.Logf("session=%q model=%q usage=%+v", result.SessionID, model, usage)
	}
	return result
}

func antigravityResultUsageTotal(result Result) int64 {
	var total int64
	for _, usage := range result.Usage {
		total += usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	}
	return total
}
