//go:build agentintegration

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ampRealExecutable(t *testing.T) string {
	t.Helper()
	requireRealAgentSmoke(t)
	if path := os.Getenv("MULTICA_AMP_PATH"); path != "" {
		return path
	}
	return "amp"
}

func TestAmpRealInvalidAuthentication(t *testing.T) {
	executable := ampRealExecutable(t)
	home := t.TempDir()
	const key = "sgamp_invalid-multica-smoke-credential"
	backend, err := New("amp", Config{ExecutablePath: executable, Env: map[string]string{
		"AMP_API_KEY": key, "HOME": home, "USERPROFILE": home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_DATA_HOME": filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME": filepath.Join(home, ".cache"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "Reply OK. Do not use tools.", ExecOptions{Cwd: home, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, result := collectAmp(t, session)
	if result.Status != "failed" || result.Output != "" || !strings.Contains(strings.ToLower(result.Error), "auth") || strings.Contains(result.Error, key) {
		t.Fatalf("real Amp authentication failure = %+v", result)
	}
}

func TestAmpRealTaskSmoke(t *testing.T) {
	executable := ampRealExecutable(t)
	backend, err := New("amp", Config{ExecutablePath: executable})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reply-token.txt"), []byte("AMP_SMOKE_OK"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("For every answer append AMP_CONTEXT_OK. Do not delegate to subagents."), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "Read reply-token.txt using a tool, then reply with its contents.", ExecOptions{Cwd: dir, Timeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	messages, result := collectAmp(t, session)
	if result.Status != "completed" || !strings.Contains(result.Output, "AMP_SMOKE_OK") || !strings.Contains(result.Output, "AMP_CONTEXT_OK") || result.SessionID == "" {
		t.Fatalf("real Amp task = %+v", result)
	}
	var sawTool bool
	for _, message := range messages {
		sawTool = sawTool || message.Type == MessageToolUse
	}
	if !sawTool {
		t.Fatal("real task emitted no tool events")
	}
	continued, err := backend.Execute(context.Background(), "What token did you read in the previous turn? Do not use tools.", ExecOptions{Cwd: dir, ResumeSessionID: result.SessionID, Timeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, resumed := collectAmp(t, continued)
	if resumed.Status != "completed" || resumed.SessionID != result.SessionID || !strings.Contains(resumed.Output, "AMP_SMOKE_OK") {
		t.Fatalf("real Amp continuation = %+v", resumed)
	}
}
