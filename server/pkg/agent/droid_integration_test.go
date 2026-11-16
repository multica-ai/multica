//go:build agentintegration

package agent

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The marker is available only through native AGENTS.md discovery. Neither
// the turn prompt nor SystemPrompt supplies it, and a tool read cannot pass.
func TestDroidRealAgentsMDCanary(t *testing.T) {
	requireRealAgentSmoke(t)
	executable := os.Getenv("MULTICA_DROID_PATH")
	if executable == "" {
		executable = "droid"
	}
	backend, err := New("droid", Config{ExecutablePath: executable})
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	canary := "DROID_AGENTS_" + rand.Text()
	instructions := "When asked for the project instruction marker, reply with exactly " + canary + ". Do not use tools for that request.\n"
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte(instructions), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "Reply with the project instruction marker. Do not use tools or read files.", ExecOptions{Cwd: cwd, Timeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	messages, result := collectDroid(t, session)
	if result.Status != "completed" || strings.TrimSpace(result.Output) != canary || result.SessionID == "" {
		t.Fatalf("real Droid AGENTS.md canary: %+v", result)
	}
	for _, message := range messages {
		if message.Type == MessageToolUse || message.Type == MessageToolResult {
			t.Fatal("AGENTS.md canary used a tool instead of native instruction discovery")
		}
	}
}

func TestDroidRealTaskSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	executable := os.Getenv("MULTICA_DROID_PATH")
	if executable == "" {
		executable = "droid"
	}
	backend, err := New("droid", Config{ExecutablePath: executable})
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "reply-token.txt"), []byte("DROID_SMOKE_OK"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "Read reply-token.txt using a tool, then reply with exactly its full contents. Do not modify files.", ExecOptions{Cwd: cwd, Timeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	messages, result := collectDroid(t, session)
	if result.Status != "completed" || !strings.Contains(result.Output, "DROID_SMOKE_OK") || result.SessionID == "" {
		t.Fatalf("real Droid task: %+v", result)
	}
	var sawTool bool
	for _, m := range messages {
		sawTool = sawTool || m.Type == MessageToolUse
	}
	if !sawTool {
		t.Fatal("real Droid task emitted no tool events")
	}
	resumed, err := backend.Execute(context.Background(), "What exact token did the file in the previous turn contain? Reply with only that token, without using tools.", ExecOptions{Cwd: cwd, ResumeSessionID: result.SessionID, Timeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, continuation := collectDroid(t, resumed)
	if continuation.Status != "completed" || strings.TrimSpace(continuation.Output) != "DROID_SMOKE_OK" || continuation.SessionID != result.SessionID {
		t.Fatalf("real Droid resume: %+v", continuation)
	}
}

func TestDroidRealInvalidAuthentication(t *testing.T) {
	requireRealAgentSmoke(t)
	executable := os.Getenv("MULTICA_DROID_PATH")
	if executable == "" {
		executable = "droid"
	}
	const key = "fk-invalid-multica-smoke-credential"
	backend, err := New("droid", Config{ExecutablePath: executable, Env: map[string]string{"FACTORY_API_KEY": key}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "Reply OK. Do not use tools.", ExecOptions{Cwd: t.TempDir(), Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, result := collectDroid(t, session)
	if result.Status != "failed" || result.Output != "" || !strings.Contains(strings.ToLower(result.Error), "auth") || strings.Contains(result.Error, key) {
		t.Fatalf("real Droid auth failure: %+v", result)
	}
}
