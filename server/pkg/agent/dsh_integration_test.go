//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// TestDshRealRuntimeSmoke is opt-in because it calls the configured DeepSeek
// model and consumes API quota. It exercises the complete Multica backend,
// installed DSH profile, model provider, and terminal-result path.
func TestDshRealRuntimeSmoke(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run the DSH integration smoke")
	}
	path := os.Getenv("MULTICA_DSH_PATH")
	if path == "" {
		t.Fatal("MULTICA_DSH_PATH is required")
	}
	b, err := New("dsh", Config{ExecutablePath: path, TaskID: "dsh-real-smoke", Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cwd := t.TempDir()
	session, err := b.Execute(ctx, "Remember the code word PINEAPPLE. Reply with exactly DSH_MULTICA_OK and nothing else.", ExecOptions{
		Cwd: cwd, Model: "deepseek-official/deepseek-v4-flash", ThinkingLevel: "off",
		Timeout: 90 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("DSH smoke failed: status=%q error=%q", result.Status, result.Error)
	}
	if strings.TrimSpace(result.Output) != "DSH_MULTICA_OK" {
		t.Fatalf("unexpected DSH output: %q", result.Output)
	}
	if result.SessionID == "" {
		t.Fatal("DSH smoke returned no session ID")
	}

	resumed, err := b.Execute(ctx, "Reply with exactly the code word from the previous turn and nothing else.", ExecOptions{
		Cwd: cwd, Model: "deepseek-official/deepseek-v4-flash", ThinkingLevel: "off",
		ResumeSessionID: result.SessionID, Timeout: 90 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range resumed.Messages {
	}
	resumeResult := <-resumed.Result
	if resumeResult.Status != "completed" {
		t.Fatalf("DSH resume smoke failed: status=%q error=%q", resumeResult.Status, resumeResult.Error)
	}
	if strings.TrimSpace(resumeResult.Output) != "PINEAPPLE" {
		t.Fatalf("DSH resume lost conversation context: %q", resumeResult.Output)
	}
	if resumeResult.SessionID != result.SessionID {
		t.Fatalf("DSH resume changed session ID: %q -> %q", result.SessionID, resumeResult.SessionID)
	}
}
