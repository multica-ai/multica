//go:build agentintegration

package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These tests exercise the real CLI through Multica's Backend, like the
// Hermes usage and Kimi managed-MCP integration tests. They do not exercise
// the server's task queue or the browser transcript.
func codebuddyRealBackend(t *testing.T) Backend {
	t.Helper()
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("real CLI smoke disabled in short mode")
	}
	path, err := exec.LookPath("codebuddy")
	if err != nil {
		t.Skip("codebuddy not on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("CLI version: %v", err)
	}
	t.Logf("CodeBuddy version=%s model=%q", strings.TrimSpace(string(version)), os.Getenv("CODEBUDDY_SMOKE_MODEL"))
	b, err := New("codebuddy", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func codebuddyRealRun(t *testing.T, b Backend, prompt string, opts ExecOptions) (Result, []Message) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	opts.Timeout = 150 * time.Second
	opts.Model = os.Getenv("CODEBUDDY_SMOKE_MODEL")
	started := time.Now()
	session, err := b.Execute(ctx, prompt, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Drain on this goroutine: both the timing and event assertions are race-free.
	var messages []Message
	var firstStatus, firstContent time.Duration
	for msg := range session.Messages {
		messages = append(messages, msg)
		if msg.Type == MessageStatus && firstStatus == 0 {
			firstStatus = time.Since(started)
		}
		if (msg.Type == MessageText || msg.Type == MessageThinking || msg.Type == MessageToolUse) && firstContent == 0 {
			firstContent = time.Since(started)
		}
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a result")
	}
	t.Logf("session=%s status=%s first_status=%s first_content=%s complete=%s usage=%+v", result.SessionID, result.Status, firstStatus, firstContent, time.Since(started), result.Usage)
	if result.Status != "completed" {
		t.Fatalf("run failed: %s: %s", result.Status, result.Error)
	}
	if result.SessionID == "" {
		t.Fatal("no session id")
	}
	if firstContent == 0 {
		t.Fatal("no displayable event")
	}
	var input, output int64
	for _, u := range result.Usage {
		input += u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens
		output += u.OutputTokens
	}
	if input <= 0 || output <= 0 {
		t.Errorf("missing real token usage: %+v", result.Usage)
	}
	return result, messages
}

func codebuddyProbeNonce(t *testing.T) string {
	t.Helper()
	var data [12]byte
	if _, err := rand.Read(data[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(data[:])
}

func TestCodebuddyRealACPSmoke(t *testing.T) {
	b := codebuddyRealBackend(t)
	result, _ := codebuddyRealRun(t, b, "Reply with exactly one word: pong. Do not use tools.", ExecOptions{Cwd: t.TempDir()})
	if strings.TrimSpace(result.Output) != "pong" {
		t.Fatalf("unexpected final output: %q", result.Output)
	}
}

func TestCodebuddyRealACPToolsAndResume(t *testing.T) {
	b := codebuddyRealBackend(t)
	dir := t.TempDir()
	nonce := codebuddyProbeNonce(t)
	first, messages := codebuddyRealRun(t, b, fmt.Sprintf("Remember this randomly generated E2E marker: %s. Use a file-writing tool to write exactly ACP_FILE_OK to probe.txt in the current directory, then reply with exactly FILE_READY. Do not write the E2E marker to any file or run other commands.", nonce), ExecOptions{Cwd: dir})
	data, err := os.ReadFile(filepath.Join(dir, "probe.txt"))
	if err != nil || strings.TrimSpace(string(data)) != "ACP_FILE_OK" {
		t.Fatalf("file not actually written: %q %v", data, err)
	}
	var use, result bool
	for _, m := range messages {
		use = use || m.Type == MessageToolUse
		result = result || m.Type == MessageToolResult
	}
	if !use || !result {
		t.Fatal("missing tool call/result events")
	}
	// Remove the work artifact so the follow-up must use conversation history.
	if err := os.Remove(filepath.Join(dir, "probe.txt")); err != nil {
		t.Fatal(err)
	}
	resumed, _ := codebuddyRealRun(t, b, "Reply with only the randomly generated E2E marker from my previous message. Do not use tools or read files.", ExecOptions{Cwd: dir, ResumeSessionID: first.SessionID})
	if resumed.SessionID != first.SessionID {
		t.Fatalf("resume changed session: %s -> %s", first.SessionID, resumed.SessionID)
	}
	if strings.TrimSpace(resumed.Output) != nonce {
		t.Fatalf("conversation was not restored: %q", resumed.Output)
	}
}

func TestCodebuddyRealACPManagedMCP(t *testing.T) {
	b := codebuddyRealBackend(t)
	if runtime.GOOS == "windows" {
		t.Skip("POSIX MCP fixture")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "mcp.log")
	path := filepath.Join(dir, "mcp-probe")
	nonce := codebuddyProbeNonce(t)
	// Reuse Kimi's MCP server fixture, but keep the response secret out of
	// the tool description and prompt. The server log proves a real tools/call.
	script := strings.ReplaceAll(stdioMcpProbeScript(log), "Returns MULTICA_MCP_OK.", "Returns a private probe value.")
	script = strings.ReplaceAll(script, "MULTICA_MCP_OK", nonce)
	writeTestExecutable(t, path, []byte(script))
	cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"multicaprobe": map[string]any{"command": path, "args": []string{}, "env": map[string]string{}}}})
	result, _ := codebuddyRealRun(t, b, "Call multica_probe_ping and reply with exactly its result. Do not use shell or file tools.", ExecOptions{Cwd: dir, McpConfig: cfg})
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"SPAWNED", `"method":"tools/list"`, `"method":"tools/call"`} {
		if !strings.Contains(string(data), event) {
			t.Fatalf("MCP did not receive %s", event)
		}
	}
	if strings.TrimSpace(result.Output) != nonce {
		t.Fatalf("MCP result not delivered: %q", result.Output)
	}
	// On resume, a fresh process must reconnect the managed server. Change
	// its secret so replaying the previous result cannot satisfy this oracle.
	next := codebuddyProbeNonce(t)
	writeTestExecutable(t, path, []byte(strings.ReplaceAll(script, nonce, next)))
	if err := os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	resumed, _ := codebuddyRealRun(t, b, "Call multica_probe_ping again; its private value has changed. Reply only with the new tool result. Do not use shell or file tools.", ExecOptions{Cwd: dir, McpConfig: cfg, ResumeSessionID: result.SessionID})
	data, err = os.ReadFile(log)
	if err != nil || !strings.Contains(string(data), `"method":"tools/call"`) {
		t.Fatalf("resumed MCP was not called: %v", err)
	}
	if resumed.SessionID != result.SessionID || strings.TrimSpace(resumed.Output) != next {
		t.Fatalf("MCP resume failed: session=%s output=%q", resumed.SessionID, resumed.Output)
	}
}

func TestCodebuddyRealACPCancel(t *testing.T) {
	b := codebuddyRealBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "Reply with pong. Do not use tools.", ExecOptions{Cwd: t.TempDir(), Timeout: 50 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var cancelledAt time.Time
	for msg := range session.Messages {
		if msg.Type == MessageStatus && cancelledAt.IsZero() {
			cancelledAt = time.Now()
			cancel()
		}
	}
	result := <-session.Result
	if cancelledAt.IsZero() {
		t.Fatalf("no session-ready status before termination: %+v", result)
	}
	if result.Status != "aborted" {
		t.Fatalf("cancellation status=%s error=%s", result.Status, result.Error)
	}
	if elapsed := time.Since(cancelledAt); elapsed > 5*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}
