package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func newTestCodexClient(t *testing.T) (*codexClient, *fakeStdin, []Message) {
	t.Helper()
	fs := &fakeStdin{}
	var mu sync.Mutex
	var messages []Message

	c := &codexClient{
		cfg:         Config{Logger: slog.Default()},
		stdin:       fs,
		pending:     make(map[int]*pendingRPC),
		processDone: make(chan struct{}),
		onMessage: func(msg Message) {
			mu.Lock()
			messages = append(messages, msg)
			mu.Unlock()
		},
		onTurnDone: func(aborted bool) {},
	}
	return c, fs, messages
}

type fakeStdin struct {
	mu   sync.Mutex
	data []byte
}

func (f *fakeStdin) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = append(f.data, p...)
	return len(p), nil
}

func (f *fakeStdin) Lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var lines []string
	for _, line := range splitLines(string(f.data)) {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

type fakeStdinWithHook struct {
	fakeStdin
	afterWrite func()
}

func (f *fakeStdinWithHook) Write(p []byte) (int, error) {
	n, err := f.fakeStdin.Write(p)
	if f.afterWrite != nil {
		f.afterWrite()
	}
	return n, err
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func TestCodexHandleResponseSuccess(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)

	// Register a pending request
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "test"}
	c.mu.Lock()
	c.pending[1] = pr
	c.mu.Unlock()

	c.handleLine(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)

	res := <-pr.ch
	if res.err != nil {
		t.Fatalf("expected no error, got %v", res.err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(res.result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed["ok"] != true {
		t.Fatalf("expected ok=true, got %v", parsed["ok"])
	}
}

func TestCodexHandleResponseError(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)

	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "test"}
	c.mu.Lock()
	c.pending[1] = pr
	c.mu.Unlock()

	c.handleLine(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad request"}}`)

	res := <-pr.ch
	if res.err == nil {
		t.Fatal("expected error")
	}
	if res.result != nil {
		t.Fatalf("expected nil result, got %v", res.result)
	}
}

func TestCodexHandleServerRequestAutoApproves(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	// Command execution approval
	c.handleLine(`{"jsonrpc":"2.0","id":10,"method":"item/commandExecution/requestApproval","params":{}}`)

	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != float64(10) {
		t.Fatalf("expected id=10, got %v", resp["id"])
	}
	result := resp["result"].(map[string]any)
	if result["decision"] != "accept" {
		t.Fatalf("expected decision=accept, got %v", result["decision"])
	}
}

func TestCodexHandleServerRequestFileChangeApproval(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	c.handleLine(`{"jsonrpc":"2.0","id":11,"method":"applyPatchApproval","params":{}}`)

	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	result := resp["result"].(map[string]any)
	if result["decision"] != "accept" {
		t.Fatalf("expected decision=accept, got %v", result["decision"])
	}
}

func TestCodexHandleServerRequestMCPElicitation(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	c.handleLine(`{"jsonrpc":"2.0","id":12,"method":"mcpServer/elicitation/request","params":{}}`)

	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != float64(12) {
		t.Fatalf("expected id=12, got %v", resp["id"])
	}
	result := resp["result"].(map[string]any)
	if result["action"] != "accept" {
		t.Fatalf("expected action=accept, got %v", result["action"])
	}
	if _, ok := result["content"]; !ok {
		t.Fatal("expected content key in response")
	}
	if _, ok := result["_meta"]; !ok {
		t.Fatal("expected _meta key in response")
	}
}

func TestCodexHandleServerRequestPermissionsApproval(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	c.handleLine(`{"jsonrpc":"2.0","id":14,"method":"item/permissions/requestApproval","params":{"permissions":{"network":{"enabled":true},"fileSystem":{"read":["/tmp/repo"],"write":["/tmp/repo"]}}}}`)

	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != float64(14) {
		t.Fatalf("expected id=14, got %v", resp["id"])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got response: %v", resp)
	}
	if result["scope"] != "turn" {
		t.Fatalf("expected scope=turn, got %v", result["scope"])
	}
	permissions, ok := result["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions object, got %v", result["permissions"])
	}
	network, ok := permissions["network"].(map[string]any)
	if !ok {
		t.Fatalf("expected network permissions object, got %v", permissions["network"])
	}
	if network["enabled"] != true {
		t.Fatalf("expected network.enabled=true, got %v", network["enabled"])
	}
	fileSystem, ok := permissions["fileSystem"].(map[string]any)
	if !ok {
		t.Fatalf("expected fileSystem permissions object, got %v", permissions["fileSystem"])
	}
	if got := fileSystem["read"].([]any)[0]; got != "/tmp/repo" {
		t.Fatalf("expected fileSystem.read to round-trip, got %v", got)
	}
}

func TestCodexPermissionsApprovalResponseDropsUnknownKeysAndLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	resp := codexPermissionsApprovalResponse(
		json.RawMessage(`{"permissions":{"network":{"enabled":true},"gpu":{"enabled":true}}}`),
		logger,
	)

	if resp["scope"] != "turn" {
		t.Fatalf("expected scope=turn, got %v", resp["scope"])
	}
	perms, ok := resp["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions object, got %v", resp["permissions"])
	}
	if _, ok := perms["network"]; !ok {
		t.Fatalf("expected network permission to be granted, got %v", perms)
	}
	if _, ok := perms["gpu"]; ok {
		t.Fatalf("expected unrecognized key gpu to be dropped, got %v", perms)
	}
	if !strings.Contains(buf.String(), "gpu") {
		t.Fatalf("expected dropped key to be logged, got %q", buf.String())
	}
}

func TestCodexPermissionsApprovalResponseMalformedParamsLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	resp := codexPermissionsApprovalResponse(json.RawMessage(`{"permissions":"not-an-object"}`), logger)

	if resp["scope"] != "turn" {
		t.Fatalf("expected scope=turn, got %v", resp["scope"])
	}
	perms, ok := resp["permissions"].(map[string]any)
	if !ok || len(perms) != 0 {
		t.Fatalf("expected empty permissions on malformed params, got %v", resp["permissions"])
	}
	if !strings.Contains(buf.String(), "failed to parse") {
		t.Fatalf("expected parse failure to be logged, got %q", buf.String())
	}
}

func TestCodexHandleServerRequestUnknownReturnsError(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	c.handleLine(`{"jsonrpc":"2.0","id":13,"method":"some/unknown/method","params":{}}`)

	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != float64(13) {
		t.Fatalf("expected id=13, got %v", resp["id"])
	}
	if resp["result"] != nil {
		t.Fatalf("expected no result for error response, got %v", resp["result"])
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}
	if errObj["code"] != float64(-32601) {
		t.Fatalf("expected error code -32601, got %v", errObj["code"])
	}
	if got := c.getTurnError(); !strings.Contains(got, "some/unknown/method") {
		t.Fatalf("expected turn error to include unsupported request method, got %q", got)
	}
}

func TestCodexLegacyEventTaskStarted(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	var gotStatus bool
	c.onMessage = func(msg Message) {
		if msg.Type == MessageStatus && msg.Status == "running" {
			gotStatus = true
		}
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"task_started"}}}`)

	if !gotStatus {
		t.Fatal("expected status=running message")
	}
	if !c.turnStarted {
		t.Fatal("expected turnStarted=true")
	}
	if c.notificationProtocol != "legacy" {
		t.Fatalf("expected protocol=legacy, got %q", c.notificationProtocol)
	}
}

func TestCodexLegacyEventAgentMessage(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	var gotText string
	c.onMessage = func(msg Message) {
		if msg.Type == MessageText {
			gotText = msg.Content
		}
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"agent_message","message":"I found the bug"}}}`)

	if gotText != "I found the bug" {
		t.Fatalf("expected text 'I found the bug', got %q", gotText)
	}
}

func TestCodexLegacyEventExecCommand(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	var messages []Message
	c.onMessage = func(msg Message) {
		messages = append(messages, msg)
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"exec_command_begin","call_id":"c1","command":"ls -la"}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"exec_command_end","call_id":"c1","output":"total 42"}}}`)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Type != MessageToolUse || messages[0].Tool != "exec_command" || messages[0].CallID != "c1" {
		t.Fatalf("unexpected begin message: %+v", messages[0])
	}
	if messages[1].Type != MessageToolResult || messages[1].CallID != "c1" || messages[1].Output != "total 42" {
		t.Fatalf("unexpected end message: %+v", messages[1])
	}
}

func TestCodexLegacyEventTaskComplete(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	var done bool
	c.onTurnDone = func(aborted bool) {
		done = true
		if aborted {
			t.Fatal("expected aborted=false")
		}
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"task_complete"}}}`)

	if !done {
		t.Fatal("expected onTurnDone to be called")
	}
}

func TestCodexLegacyEventTurnAborted(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	var abortedResult bool
	c.onTurnDone = func(aborted bool) {
		abortedResult = aborted
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"turn_aborted"}}}`)

	if !abortedResult {
		t.Fatal("expected aborted=true")
	}
}

func TestCodexRawTurnStarted(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	// The zero value "" doesn't match "unknown", so protocol auto-detection
	// won't trigger. Set it explicitly as production code would.
	c.notificationProtocol = "unknown"

	var gotStatus bool
	c.onMessage = func(msg Message) {
		if msg.Type == MessageStatus && msg.Status == "running" {
			gotStatus = true
		}
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/started","params":{"turn":{"id":"turn-1"}}}`)

	if !gotStatus {
		t.Fatal("expected status=running message")
	}
	if c.notificationProtocol != "raw" {
		t.Fatalf("expected protocol=raw, got %q", c.notificationProtocol)
	}
	if c.turnID != "turn-1" {
		t.Fatalf("expected turnID=turn-1, got %q", c.turnID)
	}
}

func TestCodexRawTurnCompleted(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var doneCount int
	c.onTurnDone = func(aborted bool) {
		doneCount++
		if aborted {
			t.Fatal("expected aborted=false")
		}
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed"}}}`)

	if doneCount != 1 {
		t.Fatalf("expected onTurnDone called once, got %d", doneCount)
	}
}

func TestCodexRawTurnCompletedSubtractsCachedInput(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.onTurnDone = func(aborted bool) {}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-usage","status":"completed","usage":{"input_tokens":1000,"cached_input_tokens":300,"output_tokens":50}}}}`)

	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.usage.InputTokens != 700 {
		t.Fatalf("input tokens = %d, want uncached 700", c.usage.InputTokens)
	}
	if c.usage.CacheReadTokens != 300 {
		t.Fatalf("cache read tokens = %d, want 300", c.usage.CacheReadTokens)
	}
	if c.usage.OutputTokens != 50 {
		t.Fatalf("output tokens = %d, want 50", c.usage.OutputTokens)
	}
}

func TestCodexRawTurnCompletedDeduplication(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var doneCount int
	c.onTurnDone = func(aborted bool) {
		doneCount++
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed"}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed"}}}`)

	if doneCount != 1 {
		t.Fatalf("expected deduplication, but onTurnDone called %d times", doneCount)
	}
}

func TestCodexRawTurnCompletedAborted(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var wasAborted bool
	c.onTurnDone = func(aborted bool) {
		wasAborted = aborted
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-2","status":"cancelled"}}}`)

	if !wasAborted {
		t.Fatal("expected aborted=true for cancelled status")
	}
}

func TestCodexRawTurnCompletedFailedCapturesError(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var wasAborted bool
	c.onTurnDone = func(aborted bool) {
		wasAborted = aborted
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-f","status":"failed","error":{"message":"unexpected status 401 Unauthorized"}}}}`)

	if wasAborted {
		t.Fatal("failed is distinct from aborted")
	}
	if got := c.getTurnError(); got != "unexpected status 401 Unauthorized" {
		t.Fatalf("expected error captured from turn.error.message, got %q", got)
	}
}

func TestCodexRawTurnCompletedFailedWithoutMessageFallsBack(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.onTurnDone = func(aborted bool) {}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-f","status":"failed"}}}`)

	if got := c.getTurnError(); got != "codex turn failed" {
		t.Fatalf("expected fallback message, got %q", got)
	}
}

func TestCodexRawErrorNotificationTerminal(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	done := false
	var activities []string
	c.onSemanticActivity = func(activity string) {
		activities = append(activities, activity)
	}
	c.onTurnDone = func(aborted bool) {
		if aborted {
			t.Fatal("terminal error should not mark the turn aborted")
		}
		done = true
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"error","params":{"error":{"message":"boom"},"willRetry":false}}`)

	if got := c.getTurnError(); got != "boom" {
		t.Fatalf("expected terminal error captured, got %q", got)
	}
	if !done {
		t.Fatal("terminal error should finish the turn")
	}
	if got, want := strings.Join(activities, ","), "error:terminal"; got != want {
		t.Fatalf("semantic activity = %q, want %q", got, want)
	}
}

func TestCodexRawErrorNotificationRetryingIgnored(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	var activities []string
	c.onSemanticActivity = func(activity string) {
		activities = append(activities, activity)
	}
	c.onTurnDone = func(aborted bool) {
		t.Fatal("retrying error should not finish the turn")
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"error","params":{"error":{"message":"reconnecting"},"willRetry":true}}`)

	if got := c.getTurnError(); got != "" {
		t.Fatalf("retrying error should not be captured, got %q", got)
	}
	if got, want := strings.Join(activities, ","), "error:retry"; got != want {
		t.Fatalf("semantic activity = %q, want %q", got, want)
	}
}

func TestCodexFirstTurnProgressActivity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		activity string
		want     bool
	}{
		{activity: "", want: false},
		{activity: "status:running", want: false},
		{activity: "error:retry", want: false},
		{activity: "error", want: true},
		{activity: "text", want: true},
		{activity: "tool-use:exec_command", want: true},
		{activity: "tool-result:exec_command", want: true},
		{activity: "item/started:commandExecution:cmd-1", want: true},
		{activity: "item/completed:agentMessage:msg-1", want: true},
		{activity: "error:terminal", want: true},
		{activity: "turn:completed", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.activity, func(t *testing.T) {
			if got := isCodexFirstTurnProgressActivity(tc.activity); got != tc.want {
				t.Fatalf("isCodexFirstTurnProgressActivity(%q) = %v, want %v", tc.activity, got, tc.want)
			}
		})
	}
}

func TestCodexSetTurnErrorFirstWins(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)

	c.setTurnError("first")
	c.setTurnError("second")

	if got := c.getTurnError(); got != "first" {
		t.Fatalf("expected first-wins semantics, got %q", got)
	}
}

func TestParseCodexSessionFileSubtractsCachedInput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := strings.Join([]string{
		`{"timestamp":"2026-06-12T17:29:27.587Z","type":"turn_context","payload":{"model":"gpt-5.5"}}`,
		`{"timestamp":"2026-06-12T17:35:37.479Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":300,"output_tokens":40,"reasoning_output_tokens":10,"total_tokens":1040},"model":"gpt-5.5"}}}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got := parseCodexSessionFile(path)
	if got == nil {
		t.Fatal("expected usage")
	}
	if got.model != "gpt-5.5" {
		t.Fatalf("model = %q, want gpt-5.5", got.model)
	}
	if got.usage.InputTokens != 700 {
		t.Fatalf("input tokens = %d, want uncached 700", got.usage.InputTokens)
	}
	if got.usage.CacheReadTokens != 300 {
		t.Fatalf("cache read tokens = %d, want 300", got.usage.CacheReadTokens)
	}
	if got.usage.OutputTokens != 50 {
		t.Fatalf("output tokens = %d, want 50", got.usage.OutputTokens)
	}
}

// The per-task CODEX_HOME must win over the ambient env / global home so usage
// is read from the same task-local sessions Codex actually wrote to (MUL-4424).
func TestCodexSessionRootPrefersExplicitTaskHome(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.
	envHome := t.TempDir()
	taskHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(envHome, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir env sessions: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(taskHome, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir task sessions: %v", err)
	}
	t.Setenv("CODEX_HOME", envHome)

	if got, want := codexSessionRoot(taskHome), filepath.Join(taskHome, "sessions"); got != want {
		t.Errorf("explicit task home ignored: got %q, want %q", got, want)
	}
	if got, want := codexSessionRoot(""), filepath.Join(envHome, "sessions"); got != want {
		t.Errorf("empty task home should fall back to ambient CODEX_HOME: got %q, want %q", got, want)
	}
}

func TestScanCodexSessionUsageReadsPerTaskHome(t *testing.T) {
	t.Parallel()
	taskHome := t.TempDir()
	startTime := time.Now().Add(-time.Minute)
	dateDir := filepath.Join(taskHome, "sessions",
		fmt.Sprintf("%04d", startTime.Year()),
		fmt.Sprintf("%02d", int(startTime.Month())),
		fmt.Sprintf("%02d", startTime.Day()),
	)
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir date dir: %v", err)
	}
	content := strings.Join([]string{
		`{"timestamp":"2026-07-13T00:00:00.000Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}`,
		`{"timestamp":"2026-07-13T00:00:01.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":500,"output_tokens":20},"model":"gpt-5.6-sol"}}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dateDir, "rollout.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	got := scanCodexSessionUsage(startTime, taskHome)
	if got == nil {
		t.Fatal("expected usage scanned from the per-task home")
	}
	if got.usage.InputTokens != 500 || got.usage.OutputTokens != 20 {
		t.Errorf("usage = %+v, want input=500 output=20", got.usage)
	}
	if got.model != "gpt-5.6-sol" {
		t.Errorf("model = %q, want gpt-5.6-sol", got.model)
	}
}

func TestCodexRawItemCommandExecution(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var messages []Message
	c.onMessage = func(msg Message) {
		messages = append(messages, msg)
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"item/started","params":{"item":{"type":"commandExecution","id":"item-1","command":"git status"}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"commandExecution","id":"item-1","aggregatedOutput":"on branch main"}}}`)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Type != MessageToolUse || messages[0].Tool != "exec_command" || messages[0].Input["command"] != "git status" {
		t.Fatalf("unexpected start message: %+v", messages[0])
	}
	if messages[1].Type != MessageToolResult || messages[1].Output != "on branch main" {
		t.Fatalf("unexpected complete message: %+v", messages[1])
	}
}

func TestCodexRawItemAgentMessageFinalAnswer(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.turnStarted = true

	var gotText string
	var turnDone bool
	c.onMessage = func(msg Message) {
		if msg.Type == MessageText {
			gotText = msg.Content
		}
	}
	c.onTurnDone = func(aborted bool) {
		turnDone = true
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"agentMessage","id":"msg-1","text":"Done!","phase":"final_answer"}}}`)

	if gotText != "Done!" {
		t.Fatalf("expected text 'Done!', got %q", gotText)
	}
	if !turnDone {
		t.Fatal("expected onTurnDone for final_answer")
	}
}

func TestCodexRawThreadStatusIdle(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.turnStarted = true

	var turnDone bool
	c.onTurnDone = func(aborted bool) {
		turnDone = true
		if aborted {
			t.Fatal("expected aborted=false for idle")
		}
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"thread/status/changed","params":{"status":{"type":"idle"}}}`)

	if !turnDone {
		t.Fatal("expected onTurnDone for idle status")
	}
}

// Regression for #1181: subagent threads (e.g. memory consolidation)
// are multiplexed on the same stdio pipe. Their turn/completed must not
// terminate the main turn.
func TestCodexRawTurnCompletedFromSubagentIgnored(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.threadID = "thr_main"

	var doneCount int
	c.onTurnDone = func(aborted bool) {
		doneCount++
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_subagent","turn":{"id":"turn-sub","status":"completed"}}}`)

	if doneCount != 0 {
		t.Fatalf("subagent turn/completed must not trigger onTurnDone, got %d calls", doneCount)
	}

	// Sanity check: a matching threadId still drives completion.
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_main","turn":{"id":"turn-main","status":"completed"}}}`)
	if doneCount != 1 {
		t.Fatalf("matching threadId should trigger onTurnDone exactly once, got %d", doneCount)
	}
}

// Regression for #1181: subagent agentMessage/final_answer must not
// trigger turn completion or leak text into the main output stream.
func TestCodexRawItemAgentMessageFinalAnswerFromSubagentIgnored(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.threadID = "thr_main"
	c.turnStarted = true

	var messages []Message
	var doneCount int
	c.onMessage = func(msg Message) {
		messages = append(messages, msg)
	}
	c.onTurnDone = func(aborted bool) {
		doneCount++
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr_subagent","item":{"type":"agentMessage","id":"sub-1","text":"subagent leakage","phase":"final_answer"}}}`)

	if len(messages) != 0 {
		t.Fatalf("subagent text must not leak into output builder, got %+v", messages)
	}
	if doneCount != 0 {
		t.Fatalf("subagent final_answer must not trigger onTurnDone, got %d calls", doneCount)
	}
}

func TestCodexCloseAllPending(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)

	pr1 := &pendingRPC{ch: make(chan rpcResult, 1), method: "m1"}
	pr2 := &pendingRPC{ch: make(chan rpcResult, 1), method: "m2"}
	c.mu.Lock()
	c.pending[1] = pr1
	c.pending[2] = pr2
	c.mu.Unlock()

	c.closeAllPending(fmt.Errorf("test error"))

	r1 := <-pr1.ch
	if r1.err == nil {
		t.Fatal("expected error for pending 1")
	}
	r2 := <-pr2.ch
	if r2.err == nil {
		t.Fatal("expected error for pending 2")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) != 0 {
		t.Fatalf("expected empty pending map, got %d", len(c.pending))
	}
}

func TestCodexRequestFailsImmediatelyAfterProcessExit(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)
	c.markProcessExited(errCodexProcessExited)

	_, err := c.request(context.Background(), "thread/start", map[string]any{})
	if !errors.Is(err, errCodexProcessExited) {
		t.Fatalf("request error = %v, want errCodexProcessExited", err)
	}
	if lines := fs.Lines(); len(lines) != 0 {
		t.Fatalf("request should not write after process exit, wrote %d lines", len(lines))
	}
}

func TestCodexRequestPrefersContextCancellationOverProcessExit(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)
	processExitMarked := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if len(fs.Lines()) >= 1 {
				cancel()
				c.markProcessExited(errCodexProcessExited)
				processExitMarked <- nil
				return
			}
			if time.Now().After(deadline) {
				processExitMarked <- fmt.Errorf("timed out waiting for request write")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	_, err := c.request(ctx, "thread/start", map[string]any{})
	if markErr := <-processExitMarked; markErr != nil {
		t.Fatal(markErr)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("request error = %v, want context.Canceled", err)
	}
}

func TestCodexRequestPrefersReadyResponseOverProcessExit(t *testing.T) {
	t.Parallel()

	var c *codexClient
	fs := &fakeStdinWithHook{}
	fs.afterWrite = func() {
		c.handleLine(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
		c.markProcessExited(errCodexProcessExited)
	}
	c = &codexClient{
		cfg:         Config{Logger: slog.Default()},
		stdin:       fs,
		pending:     make(map[int]*pendingRPC),
		processDone: make(chan struct{}),
	}

	result, err := c.request(context.Background(), "thread/start", map[string]any{})
	if err != nil {
		t.Fatalf("request error = %v, want ready response", err)
	}
	if string(result) != `{"ok":true}` {
		t.Fatalf("response = %s, want {\"ok\":true}", result)
	}
}

func TestCodexHandleInvalidJSON(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	// Should not panic
	c.handleLine("not json at all")
	c.handleLine("")
	c.handleLine("{}")
}

func TestExtractThreadID(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"thread":{"id":"t-123"}}`)
	got := extractThreadID(data)
	if got != "t-123" {
		t.Fatalf("expected t-123, got %q", got)
	}
}

func TestExtractThreadIDMissing(t *testing.T) {
	t.Parallel()

	got := extractThreadID(json.RawMessage(`{}`))
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractNestedString(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"a": map[string]any{
			"b": "value",
		},
	}
	got := extractNestedString(m, "a", "b")
	if got != "value" {
		t.Fatalf("expected 'value', got %q", got)
	}
}

func TestExtractNestedStringMissingKey(t *testing.T) {
	t.Parallel()

	m := map[string]any{"a": "flat"}
	got := extractNestedString(m, "a", "b")
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestNilIfEmpty(t *testing.T) {
	t.Parallel()

	if nilIfEmpty("") != nil {
		t.Fatal("expected nil for empty string")
	}
	if nilIfEmpty("hello") != "hello" {
		t.Fatal("expected 'hello'")
	}
}

// runRPCScript feeds JSON-RPC responses back to the codexClient by matching
// each method call written to stdin against the script, and emitting the
// scripted response via c.handleLine. It returns once all scripted calls have
// been served.
type rpcResponse struct {
	method   string          // expected request method
	result   json.RawMessage // success result body (mutually exclusive with errMsg)
	errMsg   string          // non-empty → respond with JSON-RPC error object
	errCode  int             // JSON-RPC error code when errMsg is set
	assertFn func(t *testing.T, params map[string]any)
}

// drainRPCScript spins up a goroutine that watches fs.Lines() for new outbound
// requests and, for each one, injects the scripted response via c.handleLine.
// It returns a stop function that blocks until the script is exhausted or the
// test terminates.
func drainRPCScript(t *testing.T, c *codexClient, fs *fakeStdin, script []rpcResponse) func() {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		seen := 0
		deadline := time.Now().Add(2 * time.Second)
		for seen < len(script) {
			lines := fs.Lines()
			for seen < len(lines) && seen < len(script) {
				var req struct {
					ID     int             `json:"id"`
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if err := json.Unmarshal([]byte(lines[seen]), &req); err != nil {
					t.Errorf("drainRPCScript: unmarshal request %d: %v", seen, err)
					return
				}
				expected := script[seen]
				if req.Method != expected.method {
					t.Errorf("drainRPCScript: call %d method = %q, want %q", seen, req.Method, expected.method)
					return
				}
				if expected.assertFn != nil {
					var params map[string]any
					_ = json.Unmarshal(req.Params, &params)
					expected.assertFn(t, params)
				}
				var resp string
				if expected.errMsg != "" {
					resp = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":%d,"message":%q}}`, req.ID, expected.errCode, expected.errMsg)
				} else {
					resp = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, req.ID, string(expected.result))
				}
				c.handleLine(resp)
				seen++
			}
			if seen < len(script) {
				if time.Now().After(deadline) {
					t.Errorf("drainRPCScript: timed out after %d/%d responses", seen, len(script))
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	return func() {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("drainRPCScript did not finish")
		}
	}
}

func TestCodexStartOrResumeThreadStartsFresh(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	wait := drainRPCScript(t, c, fs, []rpcResponse{
		{
			method: "thread/start",
			result: json.RawMessage(`{"thread":{"id":"thr_fresh"}}`),
			assertFn: func(t *testing.T, params map[string]any) {
				if params["cwd"] != "/work" {
					t.Errorf("cwd = %v, want /work", params["cwd"])
				}
				if params["persistExtendedHistory"] != true {
					t.Error("expected persistExtendedHistory=true on thread/start")
				}
			},
		},
	})
	defer wait()

	threadID, resumed, err := c.startOrResumeThread(context.Background(), ExecOptions{Cwd: "/work"}, slog.Default())
	if err != nil {
		t.Fatalf("startOrResumeThread: %v", err)
	}
	if threadID != "thr_fresh" {
		t.Errorf("threadID = %q, want thr_fresh", threadID)
	}
	if resumed {
		t.Error("resumed should be false when no prior session is provided")
	}
}

func TestCodexStartOrResumeThreadSetsNameOnFreshThread(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	wait := drainRPCScript(t, c, fs, []rpcResponse{
		{
			method: "thread/start",
			result: json.RawMessage(`{"thread":{"id":"thr_named"}}`),
		},
		{
			method: "thread/name/set",
			result: json.RawMessage(`{}`),
			assertFn: func(t *testing.T, params map[string]any) {
				if params["threadId"] != "thr_named" {
					t.Errorf("threadId = %v, want thr_named", params["threadId"])
				}
				if params["name"] != "Review GitHub issue #3843" {
					t.Errorf("name = %v, want semantic title", params["name"])
				}
			},
		},
	})
	defer wait()

	threadID, resumed, err := c.startOrResumeThread(
		context.Background(),
		ExecOptions{ThreadName: "Review GitHub issue #3843"},
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("startOrResumeThread: %v", err)
	}
	if threadID != "thr_named" {
		t.Errorf("threadID = %q, want thr_named", threadID)
	}
	if resumed {
		t.Error("resumed should be false when no prior session is provided")
	}
}

func TestCodexStartOrResumeThreadNameFailureDoesNotBlock(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	wait := drainRPCScript(t, c, fs, []rpcResponse{
		{
			method: "thread/start",
			result: json.RawMessage(`{"thread":{"id":"thr_named"}}`),
		},
		{
			method:  "thread/name/set",
			errMsg:  "unsupported method",
			errCode: -32601,
		},
	})
	defer wait()

	threadID, resumed, err := c.startOrResumeThread(
		context.Background(),
		ExecOptions{ThreadName: "Semantic task title"},
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("startOrResumeThread should continue after name failure: %v", err)
	}
	if threadID != "thr_named" {
		t.Errorf("threadID = %q, want thr_named", threadID)
	}
	if resumed {
		t.Error("resumed should be false when no prior session is provided")
	}
}

func TestCodexStartOrResumeThreadResumesPriorThread(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	wait := drainRPCScript(t, c, fs, []rpcResponse{
		{
			method: "thread/resume",
			result: json.RawMessage(`{"thread":{"id":"thr_prior"}}`),
			assertFn: func(t *testing.T, params map[string]any) {
				if params["threadId"] != "thr_prior" {
					t.Errorf("threadId = %v, want thr_prior", params["threadId"])
				}
				if params["cwd"] != "/work" {
					t.Errorf("cwd = %v, want /work", params["cwd"])
				}
			},
		},
	})
	defer wait()

	threadID, resumed, err := c.startOrResumeThread(
		context.Background(),
		ExecOptions{Cwd: "/work", ResumeSessionID: "thr_prior"},
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("startOrResumeThread: %v", err)
	}
	if threadID != "thr_prior" {
		t.Errorf("threadID = %q, want thr_prior", threadID)
	}
	if !resumed {
		t.Error("expected resumed=true when thread/resume succeeded")
	}
}

func TestCodexStartOrResumeThreadFallsBackOnResumeError(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	wait := drainRPCScript(t, c, fs, []rpcResponse{
		{
			method:  "thread/resume",
			errMsg:  "unknown thread",
			errCode: -32602,
		},
		{
			method: "thread/start",
			result: json.RawMessage(`{"thread":{"id":"thr_new"}}`),
		},
	})
	defer wait()

	threadID, resumed, err := c.startOrResumeThread(
		context.Background(),
		ExecOptions{Cwd: "/work", ResumeSessionID: "thr_stale"},
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("startOrResumeThread: %v", err)
	}
	if threadID != "thr_new" {
		t.Errorf("threadID = %q, want thr_new (fresh thread after fallback)", threadID)
	}
	if resumed {
		t.Error("expected resumed=false after falling back to thread/start")
	}
}

// codexTurnInput carries the user-visible disclosure when a resume was expected
// but the backend landed on a fresh thread. Paired with
// TestCodexStartOrResumeThreadFallsBackOnResumeError (which proves the live
// thread/resume RPC returns resumed=false on a recoverable error), this proves
// the real fallback surfaces the notice while a successful resume does not
// (MUL-4424).
func TestCodexTurnInput(t *testing.T) {
	t.Parallel()

	const prompt = "do the task"
	text := func(input []map[string]any) string {
		if len(input) != 1 {
			t.Fatalf("expected a single input block, got %d", len(input))
		}
		s, _ := input[0]["text"].(string)
		return s
	}

	// Resume expected but the backend fell back to a fresh thread → disclose,
	// and the original prompt must still be delivered.
	fallback := text(codexTurnInput(prompt, true, false))
	if !strings.Contains(fallback, "previous conversation context could not be restored") {
		t.Errorf("expected continuity notice on resume fallback, got:\n%s", fallback)
	}
	if !strings.HasSuffix(fallback, prompt) {
		t.Errorf("prompt must survive alongside the notice, got:\n%s", fallback)
	}

	// Successful resume, or an ordinary fresh start with no resume expected →
	// no notice, prompt delivered verbatim.
	if got := text(codexTurnInput(prompt, true, true)); got != prompt {
		t.Errorf("successful resume must not add a notice, got:\n%s", got)
	}
	if got := text(codexTurnInput(prompt, false, false)); got != prompt {
		t.Errorf("fresh start must not add a notice, got:\n%s", got)
	}
}

func TestCodexStartOrResumeThreadDoesNotFallBackAfterProcessExit(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)
	processExitMarked := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if len(fs.Lines()) >= 1 {
				c.markProcessExited(errCodexProcessExited)
				processExitMarked <- nil
				return
			}
			if time.Now().After(deadline) {
				processExitMarked <- fmt.Errorf("timed out waiting for thread/resume request")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	threadID, resumed, err := c.startOrResumeThread(
		ctx,
		ExecOptions{Cwd: "/work", ResumeSessionID: "thr_stale"},
		slog.Default(),
	)
	if markErr := <-processExitMarked; markErr != nil {
		t.Fatal(markErr)
	}
	if !errors.Is(err, errCodexProcessExited) {
		t.Fatalf("startOrResumeThread error = %v, want errCodexProcessExited", err)
	}
	if threadID != "" {
		t.Fatalf("threadID = %q, want empty", threadID)
	}
	if resumed {
		t.Fatal("resumed should be false on process exit")
	}
	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected only thread/resume request, got %d lines: %v", len(lines), lines)
	}
	var req struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if req.Method != "thread/resume" {
		t.Fatalf("request method = %q, want thread/resume", req.Method)
	}
}

func TestCodexStartOrResumeThreadFallsBackWhenResumeReturnsNoID(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	wait := drainRPCScript(t, c, fs, []rpcResponse{
		{
			method: "thread/resume",
			result: json.RawMessage(`{"thread":{}}`),
		},
		{
			method: "thread/start",
			result: json.RawMessage(`{"thread":{"id":"thr_new"}}`),
		},
	})
	defer wait()

	threadID, resumed, err := c.startOrResumeThread(
		context.Background(),
		ExecOptions{ResumeSessionID: "thr_prior"},
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("startOrResumeThread: %v", err)
	}
	if threadID != "thr_new" {
		t.Errorf("threadID = %q, want thr_new", threadID)
	}
	if resumed {
		t.Error("expected resumed=false when resume yielded no thread ID")
	}
}

func TestCodexStartOrResumeThreadStartFailureSurfaces(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	wait := drainRPCScript(t, c, fs, []rpcResponse{
		{
			method:  "thread/start",
			errMsg:  "boom",
			errCode: -32000,
		},
	})
	defer wait()

	_, _, err := c.startOrResumeThread(context.Background(), ExecOptions{}, slog.Default())
	if err == nil {
		t.Fatal("expected error when thread/start fails")
	}
	if !strings.Contains(err.Error(), "thread/start") {
		t.Errorf("error should mention thread/start, got %v", err)
	}
}

func TestCodexProtocolDetectionLegacyBlocksRaw(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)

	var messages []Message
	c.onMessage = func(msg Message) {
		messages = append(messages, msg)
	}

	// First: receive a legacy event -> locks to "legacy"
	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"task_started"}}}`)

	if c.notificationProtocol != "legacy" {
		t.Fatalf("expected legacy, got %q", c.notificationProtocol)
	}

	// Now send a raw notification -> should be ignored
	messagesBefore := len(messages)
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/started","params":{"turn":{"id":"turn-1"}}}`)

	if len(messages) != messagesBefore {
		t.Fatal("raw notification should be ignored in legacy mode")
	}
}

func TestStderrTailForwardsAndCapturesTail(t *testing.T) {
	t.Parallel()

	var sink strings.Builder
	s := newStderrTail(&sink, 16)

	if _, err := s.Write([]byte("first line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.Write([]byte("error: unexpected argument '-m' found\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Inner writer sees every byte verbatim.
	want := "first line\nerror: unexpected argument '-m' found\n"
	if sink.String() != want {
		t.Errorf("inner sink: got %q, want %q", sink.String(), want)
	}

	// Tail is bounded by max; earlier bytes get dropped.
	tail := s.Tail()
	if len(tail) > 16 {
		t.Errorf("tail exceeds bound: got %d bytes (%q)", len(tail), tail)
	}
	if tail == "" {
		t.Fatal("expected non-empty tail")
	}
	// Tail must be a suffix of what was written (whitespace-trimmed).
	if !strings.HasSuffix(strings.TrimSpace(want), tail) {
		t.Errorf("tail %q is not a suffix of %q", tail, want)
	}
}

func TestStderrTailEmptyWhenNothingWritten(t *testing.T) {
	t.Parallel()

	var sink strings.Builder
	s := newStderrTail(&sink, 16)
	if tail := s.Tail(); tail != "" {
		t.Errorf("expected empty tail, got %q", tail)
	}
}

func TestStderrTailReturnsValidUTF8(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
		max   int
		want  string
	}{
		{
			name:  "leading rune split by tail bound",
			input: []byte("aa界bc"),
			max:   4,
			want:  "bc",
		},
		{
			name:  "trailing incomplete rune",
			input: append([]byte("ok"), []byte("界")[:2]...),
			max:   16,
			want:  "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var sink bytes.Buffer
			s := newStderrTail(&sink, tt.max)
			if _, err := s.Write(tt.input); err != nil {
				t.Fatalf("write: %v", err)
			}

			if !bytes.Equal(sink.Bytes(), tt.input) {
				t.Errorf("inner sink: got %q, want raw bytes %q", sink.Bytes(), tt.input)
			}
			got := s.Tail()
			if !utf8.ValidString(got) {
				t.Errorf("tail is not valid UTF-8: %q", got)
			}
			if len(got) > tt.max {
				t.Errorf("tail exceeds bound: got %d bytes (%q), max %d", len(got), got, tt.max)
			}
			if got != tt.want {
				t.Errorf("tail: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodexExecuteSurfacesStderrWhenChildExitsEarly(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Fake codex binary: writes a canonical CLI rejection line to stderr and
	// exits before ever responding to `initialize`, mimicking what real codex
	// does when `app-server` gets a flag it doesn't accept. This exercises the
	// real os/exec stderr pipe-copy goroutine — without drainAndWait joining
	// cmd.Wait() before sampling stderrBuf.Tail(), Result.Error would come
	// back empty or truncated here.
	fakePath := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\n" +
		"echo \"error: unexpected argument '-m' found\" >&2\n" +
		"exit 2\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("codex", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new codex backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Drain message stream so the lifecycle goroutine can progress.
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "codex initialize failed") {
			t.Fatalf("expected error to mention initialize failure, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "unexpected argument '-m' found") {
			t.Fatalf("expected error to include stderr hint, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestCodexExecuteStartupRPCsHaveBoundedHandshakeTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Every startup RPC shares one handshake bound, so the *successful*
	// preamble RPCs (e.g. initialize) must also round-trip within it. The fake
	// app-server is a forked /bin/sh script; under parallel test load its
	// startup + first read/echo can spike well past a few hundred ms, which
	// used to make initialize spuriously time out before the subtest reached
	// the RPC it targets. Keep this comfortably above sh startup jitter yet
	// below the 5s semantic timeout and the 10s executeFakeCodex ceiling.
	const handshakeTimeout = 3 * time.Second
	tests := []struct {
		name   string
		method string
		body   string
		opts   ExecOptions
	}{
		{
			name:   "initialize",
			method: "initialize",
			body: "" +
				`read line` + "\n" +
				`read line` + "\n",
		},
		{
			name:   "thread start",
			method: "thread/start",
			body: "" +
				`read line` + "\n" +
				`echo '{"jsonrpc":"2.0","id":1,"result":{}}'` + "\n" +
				`read line` + "\n" +
				`read line` + "\n" +
				`read line` + "\n",
		},
		{
			name:   "thread resume",
			method: "thread/resume",
			body: "" +
				`read line` + "\n" +
				`echo '{"jsonrpc":"2.0","id":1,"result":{}}'` + "\n" +
				`read line` + "\n" +
				`read line` + "\n" +
				`read line` + "\n",
			opts: ExecOptions{ResumeSessionID: "thr-prior"},
		},
		{
			name:   "turn start",
			method: "turn/start",
			body: "" +
				`read line` + "\n" +
				`echo '{"jsonrpc":"2.0","id":1,"result":{}}'` + "\n" +
				`read line` + "\n" +
				`read line` + "\n" +
				`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-timeout"}}}'` + "\n" +
				`read line` + "\n" +
				`read line` + "\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fakePath := writeFakeCodexAppServer(t, tc.body)
			tc.opts.HandshakeTimeout = handshakeTimeout
			tc.opts.SemanticInactivityTimeout = 5 * time.Second

			started := time.Now()
			result := executeFakeCodex(t, fakePath, tc.opts)
			elapsed := time.Since(started)

			if result.Status != "failed" {
				t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
			}
			for _, want := range []string{CodexHandshakeTimeoutMarker, tc.method, handshakeTimeout.String()} {
				if !strings.Contains(result.Error, want) {
					t.Fatalf("expected error to contain %q, got %q", want, result.Error)
				}
			}
			// Proves the RPC was bounded rather than hanging to the 10s
			// executeFakeCodex ceiling; the handshake fires at ~3s and
			// shutdown is fast (closing stdin EOFs the fake).
			if elapsed > 8*time.Second {
				t.Fatalf("handshake timeout took %s, expected < 8s", elapsed)
			}
		})
	}
}

func TestCodexExecuteRetriesInitializeTimeoutOnceAfterCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	countPath := filepath.Join(t.TempDir(), "launch-count")
	fakePath := writeFakeCodexAppServer(t, ""+
		`count=0; test -f `+countPath+` && count=$(cat `+countPath+`)`+"\n"+
		`count=$((count + 1)); echo "$count" > `+countPath+"\n"+
		`read line`+"\n"+
		`if test "$count" -eq 1; then sleep 5; exit 0; fi`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-retried"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-retried","turn":{"id":"turn-1","status":"completed"}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   10 * time.Second,
		HandshakeTimeout:          100 * time.Millisecond,
		SemanticInactivityTimeout: time.Second,
	})
	if result.Status != "completed" {
		t.Fatalf("expected retry to complete, got status=%q error=%q", result.Status, result.Error)
	}
	data, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatalf("read launch count: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "2" {
		t.Fatalf("launch count = %q, want 2", got)
	}
}

func TestCodexExecuteInitializeRetrySafetyGates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	t.Run("both attempts time out", func(t *testing.T) {
		countPath := filepath.Join(t.TempDir(), "launch-count")
		fakePath := writeFakeCodexAppServer(t, ""+
			`count=0; test -f `+countPath+` && count=$(cat `+countPath+`)`+"\n"+
			`count=$((count + 1)); echo "$count" > `+countPath+"\n"+
			`read line`+"\n"+
			`sleep 1`+"\n")
		result := executeFakeCodex(t, fakePath, ExecOptions{Timeout: 5 * time.Second, HandshakeTimeout: 50 * time.Millisecond})
		if result.Status != "failed" || !strings.Contains(result.Error, CodexHandshakeTimeoutMarker) {
			t.Fatalf("expected final initialize timeout, got status=%q error=%q", result.Status, result.Error)
		}
		data, _ := os.ReadFile(countPath)
		if got := strings.TrimSpace(string(data)); got != "2" {
			t.Fatalf("launch count = %q, want 2", got)
		}
	})

	t.Run("semantic activity suppresses retry", func(t *testing.T) {
		countPath := filepath.Join(t.TempDir(), "launch-count")
		fakePath := writeFakeCodexAppServer(t, ""+
			`echo x >> `+countPath+"\n"+
			`read line`+"\n"+
			`echo '{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"unexpected","item":{"type":"commandExecution","id":"cmd-1","command":"true"}}}'`+"\n"+
			`sleep 1`+"\n")
		result := executeFakeCodex(t, fakePath, ExecOptions{Timeout: 5 * time.Second, HandshakeTimeout: 50 * time.Millisecond})
		if result.Status != "failed" {
			t.Fatalf("expected failed, got %q", result.Status)
		}
		data, _ := os.ReadFile(countPath)
		if got := strings.Count(string(data), "x"); got != 1 {
			t.Fatalf("launch count = %d, want 1", got)
		}
	})

	t.Run("unconfirmed cleanup suppresses retry", func(t *testing.T) {
		codexCleanupConfirmationOverride.Store(-1)
		defer codexCleanupConfirmationOverride.Store(0)
		countPath := filepath.Join(t.TempDir(), "launch-count")
		fakePath := writeFakeCodexAppServer(t, ""+
			`echo x >> `+countPath+"\n"+
			`read line`+"\n"+
			`sleep 1`+"\n")
		result := executeFakeCodex(t, fakePath, ExecOptions{Timeout: 5 * time.Second, HandshakeTimeout: 50 * time.Millisecond})
		if !strings.Contains(result.Error, "retry suppressed: process cleanup/reap not confirmed") {
			t.Fatalf("expected cleanup reason, got %q", result.Error)
		}
		data, _ := os.ReadFile(countPath)
		if got := strings.Count(string(data), "x"); got != 1 {
			t.Fatalf("launch count = %d, want 1", got)
		}
	})
}

func TestCodexExecuteDoesNotSerializeConcurrentLaunches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`sleep 0.4`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-concurrent"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-concurrent","turn":{"id":"turn-1","status":"completed"}}}'`+"\n")

	maxActiveCodexLaunchesObserved.Store(0)
	results := make(chan Result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			backend, _ := New("codex", Config{ExecutablePath: fakePath, Logger: slog.Default()})
			session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 5 * time.Second})
			if err != nil {
				results <- Result{Status: "failed", Error: err.Error()}
				return
			}
			go func() {
				for range session.Messages {
				}
			}()
			results <- <-session.Result
		}()
	}
	for i := 0; i < 2; i++ {
		if result := <-results; result.Status != "completed" {
			t.Fatalf("concurrent result failed: %+v", result)
		}
	}
	if got := maxActiveCodexLaunchesObserved.Load(); got < 2 {
		t.Fatalf("launches appear serialized: max active=%d", got)
	}
}

func TestSanitizeCodexDiagnosticRedactsSecrets(t *testing.T) {
	got := sanitizeCodexDiagnostic("Authorization: Bearer bearer-token\n" +
		`{"token":"json-token","auth": "json-auth", "api_key":"json-key"}` +
		"\npassword=plain-secret\x00")
	for _, secret := range []string{"bearer-token", "json-token", "json-auth", "json-key", "plain-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized diagnostic leaked %q: %q", secret, got)
		}
	}
}

func TestCodexExecuteRedactsStderrFromResultAndLogs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	const (
		bearer     = "bearer-secret-value"
		jsonToken  = "json-secret-value"
		standalone = "sk-abcdefghijklmnopqrstuvwxyz123456"
	)
	fakePath := writeFakeCodexAppServer(t, ""+
		`echo 'Authorization: Bearer `+bearer+`' >&2`+"\n"+
		`echo '{"token":"`+jsonToken+`"}' >&2`+"\n"+
		`echo '`+standalone+`' >&2`+"\n"+
		`exit 2`+"\n")
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	backend, err := New("codex", Config{ExecutablePath: fakePath, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	combined := result.Error + "\n" + logs.String()
	for _, secret := range []string{bearer, jsonToken, standalone} {
		if strings.Contains(combined, secret) {
			t.Fatalf("stderr secret leaked: %q in %q", secret, combined)
		}
	}
}

func TestCodexExecuteDoesNotProbeVersionBeforeInitialize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-fast-init"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-fast-init","turn":{"id":"turn-1","status":"completed"}}}'`+"\n")
	data, err := os.ReadFile(fakePath)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`echo "codex-cli 0.0.0-test"; exit 0`), []byte(`sleep 2; echo "codex-cli 0.0.0-test"; exit 0`), 1)
	writeTestExecutable(t, fakePath, data)

	backend, err := New("codex", Config{ExecutablePath: fakePath, Logger: slog.Default(), CodexVersion: "cached-test-version"})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Execute blocked on version probe before initialize: %s", elapsed)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("result=%+v", result)
	}
}

func TestCodexExecuteRetriesAfterSignaledProcessIsReaped(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux signal/reap semantics regression")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	defer codexGracefulShutdownTimeoutNanos.Store(0)
	countPath := filepath.Join(t.TempDir(), "launch-count")
	fakePath := writeFakeCodexAppServer(t, ""+
		`count=0; test -f `+countPath+` && count=$(cat `+countPath+`)`+"\n"+
		`count=$((count + 1)); echo "$count" > `+countPath+"\n"+
		`read line`+"\n"+
		`if test "$count" -eq 1; then exec sleep 30; fi`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-signaled"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-signaled","turn":{"id":"turn-1","status":"completed"}}}'`+"\n")
	result := executeFakeCodex(t, fakePath, ExecOptions{Timeout: 5 * time.Second, HandshakeTimeout: 50 * time.Millisecond})
	if result.Status != "completed" {
		t.Fatalf("signaled/reaped first attempt should retry: %+v", result)
	}
}

func TestCodexExecuteTimesOutWhenTurnStopsAfterToolResult(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-stale"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-stale","turn":{"id":"turn-stale"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thr-stale","item":{"type":"commandExecution","id":"cmd-1","command":"git status"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-stale","item":{"type":"commandExecution","id":"cmd-1","aggregatedOutput":"clean"}}}'`+"\n"+
		`sleep 5`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	})
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "semantic inactivity") {
		t.Fatalf("expected semantic inactivity error, got %q", result.Error)
	}
	if result.SessionID != "thr-stale" {
		t.Fatalf("expected session id to be preserved, got %q", result.SessionID)
	}
}

func TestCodexExecuteFirstTurnNoProgressSurfacesDiagnostics(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-stuck"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-stuck","turn":{"id":"turn-stuck"}}}'`+"\n"+
		`echo 'ERROR codex_models_manager::manager: failed to refresh available models: timeout waiting for child process to exit' >&2`+"\n"+
		`sleep 5`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	})
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		CodexFirstTurnNoProgressMarker,
		"thr-stuck",
		"turn-stuck",
		`model="default(empty)"`,
		`codex_version="codex-cli 0.0.0-test"`,
		"model catalog refresh timed out",
		"codex stderr:",
		codexModelCatalogRefreshTimeoutSignal,
	} {
		if !strings.Contains(result.Error, want) {
			t.Fatalf("expected error to contain %q, got %q", want, result.Error)
		}
	}
}

func TestCodexExecuteFailsWhenProcessExitsDuringActiveTurn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-crash"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-crash","turn":{"id":"turn-crash"}}}'`+"\n"+
		`echo 'fatal: app-server crashed after turn/start' >&2`+"\n"+
		`exit 2`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 5 * time.Second,
	})
	if result.Status != "failed" {
		t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "codex process exited") {
		t.Fatalf("expected process-exit error, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "fatal: app-server crashed after turn/start") {
		t.Fatalf("expected stderr tail in error, got %q", result.Error)
	}
	if strings.Contains(result.Error, "timeout") {
		t.Fatalf("process exit should fail fast instead of timeout, got %q", result.Error)
	}
}
func TestCodexExecuteCleansUpWhenScannerOverflowsOnResume(t *testing.T) {
	// Not t.Parallel(): this test mutates codexGracefulShutdownTimeoutNanos
	// globally, so running concurrently with other codex Execute tests
	// would shrink their grace window too and risk flakes.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Regression for GH#4520. On `thread/resume`, the fake codex emits a
	// single stdout line larger than the daemon's bufio.Scanner cap (10 MB),
	// which trips "bufio.Scanner: token too long" in the reader goroutine.
	// Pre-fix, drainAndWait then hung forever on cmd.Wait(): the reader had
	// stopped consuming the pipe, codex was blocked writing into a full
	// stdout buffer, stdin.Close never unblocked codex, and the deferred
	// cancel() ran AFTER drainAndWait in the LIFO defer order. The failed
	// Result therefore never reached the outer daemon and its
	// fresh-session fallback never fired.
	//
	// Post-fix, drainAndWait does graceful-then-cancel in two bounded phases
	// (see codex.go), and cmd.Cancel group-SIGKILLs the codex tree so the
	// process exits even when stdin EOF isn't sufficient. We verify the
	// failed Result reaches the caller within a small bound and carries an
	// empty SessionID so the outer daemon's PriorSessionID-with-empty-
	// SessionID fallback can retry a fresh session.
	codexGracefulShutdownTimeoutNanos.Store(int64(500 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		// Emit a > 10 MB single line with no embedded newline. printf
		// avoids the trailing newline echo would add; head + tr generates
		// the bulk payload in pure POSIX shell. The scanner errors out at
		// 10 MB even though we write 11 MB.
		`printf '{"jsonrpc":"2.0","id":2,"result":{"big":"'`+"\n"+
		`head -c 11000000 /dev/zero | tr '\0' 'x'`+"\n"+
		`printf '"}}\n'`+"\n"+
		// Hold the process open without reading more stdin. Pre-fix this
		// hangs cmd.Wait() because codex never sees stdin EOF (it isn't
		// in a read syscall) and the stdout pipe stays full. Cleanup must
		// fall back to the group-SIGKILL path to make progress.
		`sleep 30`+"\n")

	start := time.Now()
	result := executeFakeCodex(t, fakePath, ExecOptions{
		Cwd:                       t.TempDir(),
		ResumeSessionID:           "thr_prior",
		Timeout:                   30 * time.Second,
		SemanticInactivityTimeout: 5 * time.Second,
	})
	elapsed := time.Since(start)

	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (error=%q, elapsed=%s)",
			result.Status, result.Error, elapsed)
	}
	if !strings.Contains(result.Error, "token too long") {
		t.Fatalf("expected error to surface scanner overflow cause, got %q",
			result.Error)
	}
	// Empty SessionID is the contract the outer daemon fallback relies on
	// (daemon.go: result.Status == "failed" && PriorSessionID != "" &&
	// result.SessionID == "" → retry fresh). Verify thread/resume failure
	// preserves that contract.
	if result.SessionID != "" {
		t.Fatalf("expected empty SessionID so outer fallback retries fresh, got %q",
			result.SessionID)
	}
	// With the shrunken 500 ms grace, two bounded phases plus the SIGKILL
	// round-trip should complete in ~1-2 s. Pre-fix this test would block
	// until the executeFakeCodex 10 s outer timeout and fail with "timeout
	// waiting for result". We assert a much tighter bound so a future
	// regression cannot quietly slip back up to 10 s.
	if elapsed > 5*time.Second {
		t.Fatalf("cleanup took %s, expected < 5s with shrunken grace (bug regressed?)",
			elapsed)
	}
}

func TestCodexExecuteSurfacesUnsupportedServerRequestOnInterruptedTurn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-request"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-request","turn":{"id":"turn-request"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","id":99,"method":"item/tool/call","params":{"threadId":"thr-request","turnId":"turn-request","callId":"call-1","namespace":null,"tool":"custom","arguments":{}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-request","turn":{"id":"turn-request","status":"interrupted"}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 5 * time.Second,
	})
	if result.Status != "aborted" {
		t.Fatalf("expected aborted, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "unsupported codex app-server request: item/tool/call") {
		t.Fatalf("expected unsupported request error, got %q", result.Error)
	}
}

func TestCodexExecuteTimeoutWinsOverProcessExitDuringActiveTurn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-timeout"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-timeout","turn":{"id":"turn-timeout"}}}'`+"\n"+
		`read line`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 30 * time.Second,
	})
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "codex timed out after") {
		t.Fatalf("expected timeout error, got %q", result.Error)
	}
	if strings.Contains(result.Error, "codex process exited") {
		t.Fatalf("timeout should win over process EOF, got %q", result.Error)
	}
}

func TestCodexExecuteFirstTurnRetryErrorDoesNotSatisfyProgress(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-retry"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-retry","turn":{"id":"turn-retry"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"error","params":{"threadId":"thr-retry","error":{"message":"temporary reconnect"},"willRetry":true}}'`+"\n"+
		`sleep 5`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 200 * time.Millisecond,
	})
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, CodexFirstTurnNoProgressMarker) {
		t.Fatalf("expected first-turn no-progress error, got %q", result.Error)
	}
	if strings.Contains(result.Error, CodexSemanticInactivityMarker) {
		t.Fatalf("retrying error should not demote first-turn timeout to semantic inactivity, got %q", result.Error)
	}
}

func TestCodexExecuteLegacyFirstTurnMessageSatisfiesProgress(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-legacy"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"task_started"}}}'`+"\n"+
		`sleep 0.05`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"agent_message","message":"legacy alive"}}}'`+"\n"+
		`sleep 0.07`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"task_complete"}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	})
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "legacy alive" {
		t.Fatalf("expected legacy output, got %q", result.Output)
	}
}

func TestCodexExecuteSemanticInactivityAllowsContinuousMessages(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-progress"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-progress","turn":{"id":"turn-progress"}}}'`+"\n"+
		`sleep 0.05`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-progress","item":{"type":"agentMessage","id":"msg-1","text":"still working"}}}'`+"\n"+
		`sleep 0.05`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-progress","item":{"type":"commandExecution","id":"cmd-1","aggregatedOutput":"ok"}}}'`+"\n"+
		`sleep 0.05`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-progress","turn":{"id":"turn-progress","status":"completed"}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 90 * time.Millisecond,
	})
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "still working") {
		t.Fatalf("expected streamed text in output, got %q", result.Output)
	}
}

func TestCodexExecuteSemanticInactivityAllowsContinuousDeltaProgress(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-delta"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-delta","turn":{"id":"turn-delta"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/commandExecution/outputDelta","params":{"threadId":"thr-delta","item":{"type":"commandExecution","id":"cmd-1"},"delta":"line 1\n"}}'`+"\n"+
		`sleep 0.05`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thr-delta","item":{"type":"agentMessage","id":"msg-1"},"delta":"thinking"}}'`+"\n"+
		`sleep 0.05`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/fileChange/outputDelta","params":{"threadId":"thr-delta","item":{"type":"fileChange","id":"patch-1"},"delta":"patched"}}'`+"\n"+
		`sleep 0.05`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/mcpToolCall/progress","params":{"threadId":"thr-delta","item":{"type":"mcpToolCall","id":"mcp-1"},"progress":{"message":"still running"}}}'`+"\n"+
		`sleep 0.05`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-delta","turn":{"id":"turn-delta","status":"completed"}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 150 * time.Millisecond,
	})
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
}

func TestCodexExecuteSemanticInactivityDoesNotAffectNormalTurnCompletion(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-normal"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-normal","turn":{"id":"turn-normal"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-normal","item":{"type":"agentMessage","id":"msg-1","text":"Done"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-normal","turn":{"id":"turn-normal","status":"completed"}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	})
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "Done" {
		t.Fatalf("expected output Done, got %q", result.Output)
	}
}

func TestCodexExecuteTurnCompletionCanPrecedeTurnStartResponse(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-reordered"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-reordered","turn":{"id":"turn-reordered"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-reordered","item":{"type":"agentMessage","id":"msg-1","text":"Done"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-reordered","turn":{"id":"turn-reordered","status":"completed"}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	})
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "Done" {
		t.Fatalf("expected output Done, got %q", result.Output)
	}
}

func writeFakeCodexAppServer(t *testing.T, body string) string {
	t.Helper()
	fakePath := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\n" +
		`if [ "$1" = "--version" ]; then echo "codex-cli 0.0.0-test"; exit 0; fi` + "\n" +
		body
	writeTestExecutable(t, fakePath, []byte(script))
	return fakePath
}

func executeFakeCodex(t *testing.T, fakePath string, opts ExecOptions) Result {
	t.Helper()
	backend, err := New("codex", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new codex backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
		return Result{}
	}
}

func TestWithAgentStderrAppendsHint(t *testing.T) {
	t.Parallel()

	if got := withAgentStderr("codex initialize failed: process exited", "codex", ""); got != "codex initialize failed: process exited" {
		t.Errorf("empty tail should not modify msg, got %q", got)
	}
	msg := withAgentStderr("codex initialize failed: process exited", "codex", "unexpected argument '-m' found")
	want := "codex initialize failed: process exited; codex stderr: unexpected argument '-m' found"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestBuildCodexArgsExtraArgsBeforeCustomArgsAndFiltersBoth(t *testing.T) {
	args := buildCodexArgs(ExecOptions{
		ExtraArgs:  []string{"--listen", "tcp://evil", "--sandbox", "read-only"},
		CustomArgs: []string{"--sandbox", "workspace-write", "--listen=bad"},
	}, slog.Default())
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "tcp://evil") || strings.Contains(joined, "--listen=bad") {
		t.Fatalf("blocked args should be filtered from both layers: %v", args)
	}
	extraIdx, customIdx := -1, -1
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--sandbox" && args[i+1] == "read-only" {
			extraIdx = i
		}
		if args[i] == "--sandbox" && args[i+1] == "workspace-write" {
			customIdx = i
		}
	}
	if extraIdx == -1 || customIdx == -1 || extraIdx > customIdx {
		t.Fatalf("expected extra args before custom args, got %v", args)
	}
}

func TestBuildCodexArgsDoesNotLeakMcpToArgv(t *testing.T) {
	t.Parallel()

	// MCP config is materialised into $CODEX_HOME/config.toml, never into
	// argv — otherwise `mcp_servers.<id>.env` secrets would land in
	// `ps aux` output and in the daemon's `agent command` log line. This
	// test pins the contract: even with a non-empty mcp_config, no -c /
	// --config / mcp_servers.* entry shows up in buildCodexArgs output.
	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","env":{"SECRET":"hunter2"}}}}`)
	args := buildCodexArgs(ExecOptions{
		McpConfig:  raw,
		CustomArgs: []string{"-c", `model="o3"`},
	}, slog.Default())

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "mcp_servers") {
		t.Fatalf("argv must not mention mcp_servers (now lives in config.toml), got %v", args)
	}
	if strings.Contains(joined, "hunter2") {
		t.Fatalf("argv must not leak secret env values, got %v", args)
	}
	for i := 0; i+1 < len(args); i++ {
		if (args[i] == "-c" || args[i] == "--config") && strings.HasPrefix(args[i+1], "mcp_servers.") {
			t.Fatalf("expected no -c mcp_servers.* in argv, got %v", args)
		}
	}
	// Legitimate non-mcp `-c model=…` from custom_args must still survive.
	foundModel := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-c" && args[i+1] == `model="o3"` {
			foundModel = true
		}
	}
	if !foundModel {
		t.Fatalf("expected non-mcp -c override to be preserved, got %v", args)
	}
}

func TestCodexExecuteFailsClosedWhenMcpConfigInvalid(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// When the admin has a managed mcp_config but the JSON is malformed
	// (or any other reason ensureCodexMcpConfig fails), fail closed
	// instead of silently launching with the user's global MCP — that
	// would look indistinguishable from "the saved config was applied"
	// and is exactly the surprise the MCP Tab is supposed to remove.
	fakePath := writeFakeCodexAppServer(t, "exit 0\n")

	codexHome := t.TempDir()
	backend, err := New("codex", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CODEX_HOME": codexHome},
	})
	if err != nil {
		t.Fatalf("new codex backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = backend.Execute(ctx, "prompt", ExecOptions{
		Timeout:   2 * time.Second,
		McpConfig: json.RawMessage(`not json`),
	})
	if err == nil {
		t.Fatal("expected Execute to fail closed on malformed mcp_config, got nil error")
	}
	if !strings.Contains(err.Error(), "mcp_config") {
		t.Fatalf("expected error to mention mcp_config, got %q", err)
	}
}

func TestCodexExecuteFailsClosedWhenManagedMcpButNoCodexHome(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Managed mcp_config saved but no CODEX_HOME to anchor it — same
	// fail-closed reasoning: silently launching would inherit whatever
	// MCP setup the host user has, which is the wrong shape of failure.
	fakePath := writeFakeCodexAppServer(t, "exit 0\n")

	backend, err := New("codex", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{}, // no CODEX_HOME
	})
	if err != nil {
		t.Fatalf("new codex backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = backend.Execute(ctx, "prompt", ExecOptions{
		Timeout:   2 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`),
	})
	if err == nil {
		t.Fatal("expected Execute to fail closed when managed mcp_config but no CODEX_HOME, got nil error")
	}
	if !strings.Contains(err.Error(), "CODEX_HOME") {
		t.Fatalf("expected error to mention CODEX_HOME, got %q", err)
	}
}

func TestBuildCodexArgsPreservesCustomMcpOverridesWhenUnmanaged(t *testing.T) {
	t.Parallel()

	// Existing Codex agents may rely on `custom_args: ["-c", "mcp_servers.…"]`
	// because before MUL-2764 there was no MCP Tab. When the agent has
	// no managed mcp_config saved, the daemon must leave those entries
	// alone — silently dropping them would break the only way those
	// users had to configure MCP. We only claim the `mcp_servers`
	// namespace once an admin opts in via the MCP Tab.
	args := buildCodexArgs(ExecOptions{
		CustomArgs: []string{"-c", `mcp_servers.fetch={ command = "uvx" }`, "-c", `model="o3"`},
	}, slog.Default())
	foundMcp := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-c" && strings.HasPrefix(args[i+1], "mcp_servers.") {
			foundMcp = true
		}
	}
	if !foundMcp {
		t.Fatalf("custom_args mcp_servers entry must survive when agent has no managed mcp_config, got %v", args)
	}
}

func TestBuildCodexArgsDropsCustomMcpOverridesWhenManaged(t *testing.T) {
	t.Parallel()

	// Once an admin saves a managed mcp_config, the daemon owns
	// the `mcp_servers` namespace via $CODEX_HOME/config.toml. Codex's
	// `-c` is last-wins, so any `-c mcp_servers.…` left in custom_args
	// would silently shadow the saved managed entries.
	raw := json.RawMessage(`{"mcpServers":{"managed":{"command":"managed-cmd"}}}`)
	args := buildCodexArgs(ExecOptions{
		McpConfig:  raw,
		CustomArgs: []string{"-c", `mcp_servers.fetch={ command = "evil" }`, "-c", `model="o3"`},
	}, slog.Default())
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-c" && strings.HasPrefix(args[i+1], "mcp_servers.") {
			t.Fatalf("custom_args mcp_servers must be filtered when managed mcp_config is present, got %v", args)
		}
	}
	// Unrelated -c key still passes through.
	foundModel := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-c" && args[i+1] == `model="o3"` {
			foundModel = true
		}
	}
	if !foundModel {
		t.Fatalf("unrelated -c override must still survive, got %v", args)
	}
}

func TestFilterCodexCustomConfigOverridesDropsMcpServers(t *testing.T) {
	t.Parallel()

	// Codex `-c` is last-wins, so a user-supplied `-c mcp_servers.…` in
	// custom_args would silently shadow whatever the MCP Tab wrote into
	// CODEX_HOME/config.toml. Verify that all spellings of the override
	// get dropped, while unrelated `-c` keys pass through.
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "separated -c mcp_servers.fetch=…",
			in:   []string{"-c", `mcp_servers.fetch={ command = "evil" }`, "-c", `model="o3"`},
			want: []string{"-c", `model="o3"`},
		},
		{
			name: "inline -c=mcp_servers.fetch=…",
			in:   []string{`-c=mcp_servers.fetch={ command = "evil" }`, "--listen=keep"},
			want: []string{"--listen=keep"},
		},
		{
			name: "long form --config mcp_servers.x.env.KEY=val",
			in:   []string{"--config", `mcp_servers.x.env.KEY="leak"`, "--config", `sandbox="workspace-write"`},
			want: []string{"--config", `sandbox="workspace-write"`},
		},
		{
			name: "passes through unrelated -c overrides",
			in:   []string{"-c", `model="o3"`, "-c", `sandbox.network_access=true`},
			want: []string{"-c", `model="o3"`, "-c", `sandbox.network_access=true`},
		},
		{
			name: "matches mcp_servers root assignment",
			in:   []string{"-c", `mcp_servers={fetch={command="evil"}}`, "-c", `model="o3"`},
			want: []string{"-c", `model="o3"`},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := filterCodexCustomConfigOverrides(tc.in, slog.Default())
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("filterCodexCustomConfigOverrides(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFilterCodexShellEnvConfigOverrides(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "root policy override",
			in:   []string{"-c", `shell_environment_policy.include_only=["PATH"]`, "-c", `model="o3"`},
			want: []string{"-c", `model="o3"`},
		},
		{
			name: "profile policy override",
			in:   []string{`--config=profiles.work.shell_environment_policy.ignore_default_excludes=false`, "--sandbox", "workspace-write"},
			want: []string{"--sandbox", "workspace-write"},
		},
		{
			name: "quoted policy key",
			in:   []string{"--config", `profiles.work."shell_environment_policy".inherit="none"`},
			want: []string{},
		},
		{
			name: "unrelated override survives",
			in:   []string{"-c", `model="o3"`, "-c", `profiles.work.model="gpt-5.6"`, "-c", `tools.shell_environment_policy="metadata"`},
			want: []string{"-c", `model="o3"`, "-c", `profiles.work.model="gpt-5.6"`, "-c", `tools.shell_environment_policy="metadata"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := filterCodexShellEnvConfigOverrides(tc.in, slog.Default())
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("filterCodexShellEnvConfigOverrides(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestEnsureCodexMcpConfigEmptyClearsBlock(t *testing.T) {
	t.Parallel()

	// When agent.mcp_config is null/empty the managed block is removed
	// from config.toml, but unrelated content (sandbox block, user-level
	// `[mcp_servers.user]`) is left untouched.
	tmp := filepath.Join(t.TempDir(), "config.toml")
	initial := "sandbox_mode = \"workspace-write\"\n\n" +
		multicaCodexMcpBeginMarker + "\n" +
		"[mcp_servers.fetch]\ncommand = \"uvx\"\n" +
		multicaCodexMcpEndMarker + "\n\n" +
		"[mcp_servers.user_global]\ncommand = \"keep\"\n"
	if err := os.WriteFile(tmp, []byte(initial), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := ensureCodexMcpConfig(tmp, nil, slog.Default()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	got := string(data)
	if strings.Contains(got, multicaCodexMcpBeginMarker) {
		t.Fatalf("managed block should be cleared, got:\n%s", got)
	}
	if !strings.Contains(got, "[mcp_servers.user_global]") {
		t.Fatalf("user-defined mcp_servers should be left alone when agent has no mcp_config, got:\n%s", got)
	}
	if !strings.Contains(got, `sandbox_mode = "workspace-write"`) {
		t.Fatalf("unrelated config preserved, got:\n%s", got)
	}
}

func TestEnsureCodexMcpConfigWritesManagedBlock(t *testing.T) {
	t.Parallel()

	// A non-empty mcp_config writes one `[mcp_servers.<name>]` table per
	// server, in stable alphabetical order, into the managed block. The
	// file mode is 0o600 because env values may carry secrets.
	tmp := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(tmp, []byte("sandbox_mode = \"workspace-write\"\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	raw := json.RawMessage(`{"mcpServers":{"zeta":{"command":"b"},"alpha":{"command":"a","env":{"K":"v"}}}}`)
	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	got := string(data)

	if !strings.Contains(got, multicaCodexMcpBeginMarker) || !strings.Contains(got, multicaCodexMcpEndMarker) {
		t.Fatalf("expected managed block markers, got:\n%s", got)
	}
	alphaIdx := strings.Index(got, "[mcp_servers.alpha]")
	zetaIdx := strings.Index(got, "[mcp_servers.zeta]")
	if alphaIdx == -1 || zetaIdx == -1 {
		t.Fatalf("expected both server tables, got:\n%s", got)
	}
	if alphaIdx > zetaIdx {
		t.Fatalf("expected alpha before zeta (alphabetical), got:\n%s", got)
	}
	for _, want := range []string{
		`command = "a"`,
		`env = { K = "v" }`,
		`command = "b"`,
		`sandbox_mode = "workspace-write"`, // unrelated user content preserved
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in:\n%s", want, got)
		}
	}
	for _, unexpected := range []string{
		`experimental_use_rmcp_client`,
		`http_headers`,
	} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("stdio servers should not get remote MCP key %q, got:\n%s", unexpected, got)
		}
	}

	fi, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected mode 0o600 for secret-bearing config, got %o", mode)
	}
}

func TestEnsureCodexMcpConfigTranslatesRemoteHTTPServer(t *testing.T) {
	t.Parallel()

	tmp := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(tmp, []byte("sandbox_mode = \"workspace-write\"\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	raw := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.example.test/notion","headers":{"Authorization":"Bearer test-token","x-api-key":"secret"}}}}`)
	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	got := string(data)

	for _, want := range []string{
		`[mcp_servers.composio]`,
		`url = "https://mcp.example.test/notion"`,
		`http_headers = { Authorization = "Bearer test-token", x-api-key = "secret" }`,
		`experimental_use_rmcp_client = true`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in:\n%s", want, got)
		}
	}
	for _, unexpected := range []string{
		`type = "http"`,
		"\nheaders = ",
	} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("remote HTTP server should not render %q, got:\n%s", unexpected, got)
		}
	}
}

func TestEnsureCodexMcpConfigDropsInternalSelectors(t *testing.T) {
	t.Parallel()

	tmp := filepath.Join(t.TempDir(), "config.toml")
	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"],"env":{"API_KEY":"secret"},"timeout":30,"tools":{"include":["kb_get"]},"prompts":{"include":["daily"]},"resources":{"include":["docs"]}}}}`)
	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	got := string(data)

	for _, want := range []string{
		`[mcp_servers.fetch]`,
		`command = "uvx"`,
		`args = ["mcp-server-fetch"]`,
		`env = { API_KEY = "secret" }`,
		`timeout = 30`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in:\n%s", want, got)
		}
	}
	for _, unexpected := range []string{
		"\ntools = ",
		"\nprompts = ",
		"\nresources = ",
		"kb_get",
		"daily",
		"docs",
	} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("internal MCP selector %q must not render into Codex config.toml:\n%s", unexpected, got)
		}
	}
}

func TestEnsureCodexMcpConfigDropsInternalSelectorsFromRemoteHTTPServer(t *testing.T) {
	t.Parallel()

	tmp := filepath.Join(t.TempDir(), "config.toml")
	raw := json.RawMessage(`{"mcpServers":{"remote":{"type":"http","url":"https://mcp.example.test/session","headers":{"Authorization":"Bearer test-token"},"timeout":45,"tools":{"include":["kb_get"]},"prompts":{"include":["daily"]},"resources":{"include":["docs"]}}}}`)
	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	got := string(data)

	for _, want := range []string{
		`[mcp_servers.remote]`,
		`url = "https://mcp.example.test/session"`,
		`http_headers = { Authorization = "Bearer test-token" }`,
		`timeout = 45`,
		`experimental_use_rmcp_client = true`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in:\n%s", want, got)
		}
	}
	for _, unexpected := range []string{
		"\ntools = ",
		"\nprompts = ",
		"\nresources = ",
		"kb_get",
		"daily",
		"docs",
		`type = "http"`,
		"\nheaders = ",
	} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("internal or provider-incompatible key %q must not render into Codex config.toml:\n%s", unexpected, got)
		}
	}
}

func TestRenderCodexMcpServersBlockTreatsURLOnlyServerAsRemote(t *testing.T) {
	t.Parallel()

	block, hasServers, err := renderCodexMcpServersBlock(json.RawMessage(`{"mcpServers":{"remote":{"url":"https://mcp.example.test/session"}}}`))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !hasServers {
		t.Fatal("expected server to render")
	}
	for _, want := range []string{
		`[mcp_servers.remote]`,
		`url = "https://mcp.example.test/session"`,
		`experimental_use_rmcp_client = true`,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("expected %q in:\n%s", want, block)
		}
	}
}

func TestEnsureCodexMcpConfigForces0600OnPreexistingFile(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions only")
	}

	// `execenv.copyFile` seeds the per-task config.toml at 0o644. Once we
	// add secret-bearing mcp_servers tables to it, the mode must drop to
	// 0o600 — `os.WriteFile` alone keeps the existing mode, so the chmod
	// is the part we need to pin.
	tmp := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(tmp, []byte("sandbox_mode = \"workspace-write\"\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","env":{"API_KEY":"secret"}}}}`)
	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	fi, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected 0o600 after overwrite of pre-existing 0o644 file, got %o", mode)
	}
}

func TestEnsureCodexMcpConfigStripsUserMcpServersWhenManaged(t *testing.T) {
	t.Parallel()

	// When agent.mcp_config is non-empty, ALL user-defined `[mcp_servers.*]`
	// tables (inherited from ~/.codex/config.toml) are stripped to avoid
	// (a) TOML "table already exists" errors when names collide and (b) the
	// user's global servers silently being mixed in with the strict
	// agent-managed list. Sub-tables like `[mcp_servers.x.env]` are also
	// dropped as part of their parent.
	tmp := filepath.Join(t.TempDir(), "config.toml")
	initial := "sandbox_mode = \"workspace-write\"\n\n" +
		"[mcp_servers.global_fetch]\ncommand = \"uvx-old\"\n\n" +
		"[mcp_servers.global_fetch.env]\nOLD_KEY = \"old\"\n\n" +
		"[other_section]\nkeep_me = true\n"
	if err := os.WriteFile(tmp, []byte(initial), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	raw := json.RawMessage(`{"mcpServers":{"new_server":{"command":"new"}}}`)
	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	data, _ := os.ReadFile(tmp)
	got := string(data)

	if strings.Contains(got, "global_fetch") {
		t.Fatalf("user mcp_servers tables must be stripped when agent has its own mcp_config, got:\n%s", got)
	}
	if strings.Contains(got, "OLD_KEY") {
		t.Fatalf("user mcp_servers sub-tables must be stripped too, got:\n%s", got)
	}
	if !strings.Contains(got, "[other_section]") || !strings.Contains(got, "keep_me = true") {
		t.Fatalf("unrelated tables must survive, got:\n%s", got)
	}
	if !strings.Contains(got, "[mcp_servers.new_server]") {
		t.Fatalf("managed server should be written, got:\n%s", got)
	}
}

func TestEnsureCodexMcpConfigIdempotent(t *testing.T) {
	t.Parallel()

	// Running ensure twice with the same input must produce byte-identical
	// output — needed because Prepare and Reuse may both call into this on
	// the same per-task config.toml across a task's lifetime.
	tmp := filepath.Join(t.TempDir(), "config.toml")
	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["a","b"]}}}`)

	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	first, _ := os.ReadFile(tmp)

	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	second, _ := os.ReadFile(tmp)

	if string(first) != string(second) {
		t.Fatalf("non-idempotent write:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestEnsureCodexMcpConfigRejectsBadShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{"non-json", `not json`},
		{"server is array", `{"mcpServers":{"x":[1,2]}}`},
		{"server is string", `{"mcpServers":{"x":"oops"}}`},
		{"null value inside server", `{"mcpServers":{"x":{"command":null}}}`},
		{"bad server name", `{"mcpServers":{"has space":{"command":"a"}}}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmp := filepath.Join(t.TempDir(), "config.toml")
			if err := ensureCodexMcpConfig(tmp, json.RawMessage(tc.raw), slog.Default()); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestEnsureCodexMcpConfigAbsentLeavesUserTablesAlone(t *testing.T) {
	t.Parallel()

	// nil / `null` map to the API's "absent" state: the agent has no
	// managed mcp_config, so the daemon must not touch the user's
	// inherited `[mcp_servers.*]` tables — the run falls back to the
	// user's global CLI config.
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`)} {
		tmp := filepath.Join(t.TempDir(), "config.toml")
		initial := "sandbox_mode = \"workspace-write\"\n\n" +
			"[mcp_servers.user_global]\ncommand = \"keep\"\n"
		if err := os.WriteFile(tmp, []byte(initial), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
			t.Fatalf("ensure (%q): %v", string(raw), err)
		}
		data, _ := os.ReadFile(tmp)
		got := string(data)
		if !strings.Contains(got, "[mcp_servers.user_global]") {
			t.Fatalf("absent mcp_config (%q) must leave user MCP tables alone, got:\n%s", string(raw), got)
		}
		if strings.Contains(got, multicaCodexMcpBeginMarker) {
			t.Fatalf("absent mcp_config (%q) must not write managed markers, got:\n%s", string(raw), got)
		}
	}
}

func TestEnsureCodexMcpConfigEmptyManagedSetStripsUserMcp(t *testing.T) {
	t.Parallel()

	// `{}` / `{"mcpServers":{}}` map to the API's "present, empty" state.
	// The admin saved an explicit (empty) MCP list, so the daemon must
	// strip inherited user `[mcp_servers.*]` tables and pin the managed
	// markers — equivalent to Claude's --strict-mcp-config with an empty
	// servers map. Falling back to the user's global MCP would defeat
	// the affordance.
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(`{"mcpServers":{}}`),
	} {
		tmp := filepath.Join(t.TempDir(), "config.toml")
		initial := "sandbox_mode = \"workspace-write\"\n\n" +
			"[mcp_servers.user_global]\ncommand = \"keep\"\n"
		if err := os.WriteFile(tmp, []byte(initial), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
			t.Fatalf("ensure (%q): %v", string(raw), err)
		}
		data, _ := os.ReadFile(tmp)
		got := string(data)
		if strings.Contains(got, "user_global") {
			t.Fatalf("managed empty set (%q) must strip user MCP tables, got:\n%s", string(raw), got)
		}
		if !strings.Contains(got, multicaCodexMcpBeginMarker) || !strings.Contains(got, multicaCodexMcpEndMarker) {
			t.Fatalf("managed empty set (%q) must still write markers so future runs find them, got:\n%s", string(raw), got)
		}
		if !strings.Contains(got, `sandbox_mode = "workspace-write"`) {
			t.Fatalf("unrelated content must survive (%q), got:\n%s", string(raw), got)
		}
	}
}

func TestEnsureCodexMcpConfigEmptyManagedSetIdempotent(t *testing.T) {
	t.Parallel()

	// Running ensure twice with the same `{}` input must produce
	// byte-identical output — guards against the empty-marker block
	// accreting blank lines or duplicate markers across reruns.
	tmp := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(tmp, []byte("sandbox_mode = \"workspace-write\"\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := json.RawMessage(`{}`)
	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	first, _ := os.ReadFile(tmp)
	if err := ensureCodexMcpConfig(tmp, raw, slog.Default()); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	second, _ := os.ReadFile(tmp)
	if string(first) != string(second) {
		t.Fatalf("non-idempotent write:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestHasManagedCodexMcpConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"nil", nil, false},
		{"empty bytes", json.RawMessage(""), false},
		{"whitespace only", json.RawMessage("   \n\t"), false},
		{"json null", json.RawMessage(`null`), false},
		{"json null with whitespace", json.RawMessage(" null \n"), false},
		{"empty object", json.RawMessage(`{}`), true},
		{"empty mcp servers map", json.RawMessage(`{"mcpServers":{}}`), true},
		{"populated", json.RawMessage(`{"mcpServers":{"x":{"command":"a"}}}`), true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasManagedCodexMcpConfig(tc.raw); got != tc.want {
				t.Fatalf("hasManagedCodexMcpConfig(%q) = %v, want %v", string(tc.raw), got, tc.want)
			}
		})
	}
}
