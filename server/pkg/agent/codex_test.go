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

	"github.com/multica-ai/multica/server/pkg/redact"
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

func TestCodexTurnNotificationGateDropsResumeReplayAndOtherTurns(t *testing.T) {
	t.Parallel()

	gate := &codexTurnNotificationGate{}
	previousCompleted := map[string]any{
		"threadId": "thr-resumed",
		"turn": map[string]any{
			"id":     "turn-previous",
			"status": "completed",
		},
	}
	if gate.accept("turn/completed", previousCompleted) {
		t.Fatal("resume replay must stay blocked before the current turn is armed")
	}

	gate.arm()
	if !gate.accept("turn/completed", previousCompleted) {
		t.Fatal("armed gate must preserve older streams that omit turn/started")
	}
	if !gate.accept("turn/started", map[string]any{
		"threadId": "thr-resumed",
		"turn":     map[string]any{"id": "turn-current"},
	}) {
		t.Fatal("current turn/started should open the lifecycle gate")
	}
	if gate.accept("turn/completed", previousCompleted) {
		t.Fatal("completion from another turn must not finish the current turn")
	}
	if !gate.accept("item/completed", map[string]any{
		"threadId": "thr-resumed",
		"turnId":   "turn-current",
	}) {
		t.Fatal("current-turn item should pass the gate")
	}
	if !gate.accept("turn/completed", map[string]any{
		"threadId": "thr-resumed",
		"turn": map[string]any{
			"id":     "turn-current",
			"status": "completed",
		},
	}) {
		t.Fatal("current turn completion should pass the gate")
	}

	legacy := &codexTurnNotificationGate{}
	legacyComplete := map[string]any{"msg": map[string]any{"type": "task_complete"}}
	if legacy.accept("codex/event", legacyComplete) {
		t.Fatal("legacy resume replay must stay blocked before the current turn is armed")
	}
	legacy.arm()
	if !legacy.accept("codex/event", legacyComplete) {
		t.Fatal("armed gate must preserve legacy streams that omit task_started")
	}
	if !legacy.accept("codex/event", map[string]any{"msg": map[string]any{"type": "task_started"}}) {
		t.Fatal("legacy current task_started should open the lifecycle gate")
	}
	if !legacy.accept("codex/event", legacyComplete) {
		t.Fatal("legacy current task completion should pass the gate")
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

	line := `{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed","usage":{"input_tokens":100,"output_tokens":10}}}}`
	c.handleLine(line)
	c.handleLine(line)

	if doneCount != 1 {
		t.Fatalf("expected deduplication, but onTurnDone called %d times", doneCount)
	}
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.usage.InputTokens != 100 || c.usage.OutputTokens != 10 {
		t.Fatalf("duplicate completion double-counted usage: %+v", c.usage)
	}
}

func TestCodexRawTurnCompletedWithoutIDDeduplicatesLifecycleAndUsage(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var doneCount int
	c.onTurnDone = func(aborted bool) { doneCount++ }
	line := `{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"status":"completed","usage":{"input_tokens":100,"output_tokens":10}}}}`
	c.handleLine(line)
	// An empty-ID start cannot prove that a new turn began, so it must not
	// reopen an already completed turn.
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/started","params":{"turn":{}}}`)
	c.handleLine(line)

	if doneCount != 1 {
		t.Fatalf("completion count = %d, want 1", doneCount)
	}
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.usage.InputTokens != 100 || c.usage.OutputTokens != 10 {
		t.Fatalf("duplicate empty-id completion double-counted usage: %+v", c.usage)
	}
}

func TestCodexRawTurnCompletedReplayDoesNotReopenSameTurn(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var doneCount int
	c.onTurnDone = func(aborted bool) { doneCount++ }
	started := `{"jsonrpc":"2.0","method":"turn/started","params":{"turn":{"id":"turn-1"}}}`
	completed := `{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed","usage":{"input_tokens":100,"output_tokens":10}}}}`
	c.handleLine(started)
	c.handleLine(completed)
	c.handleLine(started)
	c.handleLine(completed)

	if doneCount != 1 {
		t.Fatalf("completion count = %d, want 1 after replay", doneCount)
	}
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.usage.InputTokens != 100 || c.usage.OutputTokens != 10 {
		t.Fatalf("replayed turn double-counted usage: %+v", c.usage)
	}
}

func TestCodexRawLateCompletionWithoutIDCannotReopenClient(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var doneCount int
	c.onTurnDone = func(aborted bool) { doneCount++ }
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/started","params":{"turn":{"id":"turn-1"}}}`)
	completedWithoutID := `{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"status":"completed","usage":{"input_tokens":100,"output_tokens":10}}}}`
	c.handleLine(completedWithoutID)
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/started","params":{"turn":{"id":"turn-2"}}}`)
	c.handleLine(completedWithoutID)

	if doneCount != 1 {
		t.Fatalf("completion count = %d, want 1 after late no-ID replay", doneCount)
	}
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.usage.InputTokens != 100 || c.usage.OutputTokens != 10 {
		t.Fatalf("late no-ID completion double-counted usage: %+v", c.usage)
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
	c.notificationProtocol = "unknown"
	done := false
	var activities []string
	c.onSemanticActivity = func(activity string) {
		activities = append(activities, activity)
	}
	c.onTurnDone = func(aborted bool) {
		done = true
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"error","params":{"error":{"message":"boom"},"willRetry":false}}`)

	if got := c.getTurnError(); got != "boom" {
		t.Fatalf("expected terminal error captured, got %q", got)
	}
	if done {
		t.Fatal("terminal error must wait for turn/completed")
	}
	if got, want := strings.Join(activities, ","), "error:terminal"; got != want {
		t.Fatalf("semantic activity = %q, want %q", got, want)
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-error","status":"failed","error":{"message":"boom"}}}}`)
	if !done {
		t.Fatal("turn/completed should finish the failed turn")
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

// TestCodexFirstTurnNoProgressTimeoutClamp pins both halves of the first-turn
// window: the 60s ceiling that a healthy gpt-5.5 turn has to fit under
// (MUL-5542), and the fact that the configured semantic inactivity timeout can
// only ever shrink it, never raise it. The second half is easy to misread as a
// knob for the ceiling — it is not, and GH #5959 proposed removing the ceiling
// entirely on that reading.
func TestCodexFirstTurnNoProgressTimeoutClamp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		semantic time.Duration
		want     time.Duration
	}{
		{name: "unset falls back to the ceiling", semantic: 0, want: 60 * time.Second},
		{name: "negative falls back to the ceiling", semantic: -1 * time.Second, want: 60 * time.Second},
		{name: "default 10m is capped at the ceiling", semantic: 10 * time.Minute, want: 60 * time.Second},
		{name: "raising semantic cannot raise the ceiling", semantic: 2 * time.Minute, want: 60 * time.Second},
		{name: "equal to the ceiling scales to 4/5", semantic: 60 * time.Second, want: 48 * time.Second},
		{name: "below the ceiling scales to 4/5", semantic: 30 * time.Second, want: 24 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexFirstTurnNoProgressTimeout(tc.semantic, 0); got != tc.want {
				t.Fatalf("codexFirstTurnNoProgressTimeout(%s, 0) = %s, want %s", tc.semantic, got, tc.want)
			}
		})
	}
}

// TestCodexFirstTurnNoProgressTimeoutExplicitOverride covers the
// MULTICA_CODEX_FIRST_TURN_TIMEOUT path added for GH #3262 / #5959: a positive
// configured value is honored as-is for the first-turn watchdog ceiling,
// including upward past the default that the semantic inactivity timeout alone
// can never raise. This resolver only sets that one timer's duration; the
// effective first-item wait is still min(ceiling, semantic, execution) because
// the semantic timer runs concurrently — see
// TestCodexExecuteFirstTurnOverrideAboveSemanticIsTruncated for that runtime
// interaction. A non-positive override changes nothing — the function falls back
// to the pinned default/scaling behaviour asserted by
// TestCodexFirstTurnNoProgressTimeoutClamp.
func TestCodexFirstTurnNoProgressTimeoutExplicitOverride(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		semantic   time.Duration
		configured time.Duration
		want       time.Duration
	}{
		{name: "override above the ceiling is honored in full", semantic: 0, configured: 5 * time.Minute, want: 5 * time.Minute},
		{name: "override wins over the default 10m semantic", semantic: 10 * time.Minute, configured: 2 * time.Minute, want: 2 * time.Minute},
		{name: "override wins over a value that would otherwise scale down", semantic: 30 * time.Second, configured: 90 * time.Second, want: 90 * time.Second},
		{name: "override raises the resolver ceiling above the default", semantic: 0, configured: 30 * time.Minute, want: 30 * time.Minute},
		{name: "zero override keeps the default ceiling", semantic: 0, configured: 0, want: 60 * time.Second},
		{name: "negative override is ignored and falls back to the ceiling", semantic: 10 * time.Minute, configured: -1 * time.Second, want: 60 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexFirstTurnNoProgressTimeout(tc.semantic, tc.configured); got != tc.want {
				t.Fatalf("codexFirstTurnNoProgressTimeout(%s, %s) = %s, want %s", tc.semantic, tc.configured, got, tc.want)
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
	threadID := "task-thread"
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
		fmt.Sprintf(`{"timestamp":%q,"type":"turn_context","payload":{"model":"gpt-5.6-sol"}}`, startTime.Add(time.Second).UTC().Format(time.RFC3339Nano)),
		fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":500,"output_tokens":20},"model":"gpt-5.6-sol"}}}`, startTime.Add(2*time.Second).UTC().Format(time.RFC3339Nano)),
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dateDir, "rollout-2026-07-13T00-00-00-"+threadID+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	got := scanCodexSessionUsage(startTime, taskHome, threadID, false)
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

func TestScanCodexSessionUsageSubtractsResumeBaseline(t *testing.T) {
	t.Parallel()
	taskHome := t.TempDir()
	threadID := "resumed-thread"
	startTime := time.Date(2026, time.July, 13, 0, 0, 10, 0, time.UTC)
	dateDir := filepath.Join(taskHome, "sessions", "2026", "07", "13")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir date dir: %v", err)
	}
	content := strings.Join([]string{
		`{"timestamp":"2026-07-13T00:00:00.000Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}`,
		`{"timestamp":"2026-07-13T00:00:05.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":700,"output_tokens":50,"reasoning_output_tokens":10},"model":"gpt-5.6-sol"}}}`,
		`{"timestamp":"2026-07-13T00:00:11.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1300,"cached_input_tokens":900,"output_tokens":70,"reasoning_output_tokens":15},"model":"gpt-5.6-sol"}}}`,
		`{"timestamp":"2026-07-13T00:00:12.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1800,"cached_input_tokens":1400,"output_tokens":100,"reasoning_output_tokens":25},"model":"gpt-5.6-sol"}}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dateDir, "rollout-2026-07-13T00-00-00-"+threadID+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	got := scanCodexSessionUsage(startTime, taskHome, threadID, true)
	if got == nil {
		t.Fatal("expected usage")
	}
	// total_token_usage is cumulative for the resumed Codex session. This task
	// should report only the delta after startTime, not the whole session total.
	want := TokenUsage{InputTokens: 100, OutputTokens: 65, CacheReadTokens: 700}
	if got.usage != want {
		t.Fatalf("usage = %+v, want resumed-task delta %+v", got.usage, want)
	}
}

func TestParseCodexSessionFileSinceResumeEdgeCases(t *testing.T) {
	t.Parallel()
	startTime := time.Date(2026, time.July, 13, 0, 0, 10, 0, time.UTC)
	tests := []struct {
		name  string
		lines []string
		want  TokenUsage
	}{
		{
			name: "final last usage wins over earlier total",
			lines: []string{
				`{"timestamp":"2026-07-13T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":10}}}}`,
				`{"timestamp":"2026-07-13T00:00:11Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"output_tokens":20}}}}`,
				`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":7,"output_tokens":3}}}}`,
			},
			want: TokenUsage{InputTokens: 7, OutputTokens: 3},
		},
		{
			name: "final total wins over earlier last usage",
			lines: []string{
				`{"timestamp":"2026-07-13T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":10}}}}`,
				`{"timestamp":"2026-07-13T00:00:11Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":7,"output_tokens":3}}}}`,
				`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"output_tokens":20}}}}`,
			},
			want: TokenUsage{InputTokens: 60, OutputTokens: 10},
		},
		{
			name: "cache alias changes from cache read to cached input",
			lines: []string{
				`{"timestamp":"2026-07-13T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cache_read_input_tokens":700}}}}`,
				`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1800,"cached_input_tokens":1400}}}}`,
			},
			want: TokenUsage{InputTokens: 100, CacheReadTokens: 700},
		},
		{
			name: "cache alias changes from cached input to cache read",
			lines: []string{
				`{"timestamp":"2026-07-13T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":700}}}}`,
				`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1800,"cache_read_input_tokens":1400}}}}`,
			},
			want: TokenUsage{InputTokens: 100, CacheReadTokens: 700},
		},
		{
			name: "counter reset accumulates every segment",
			lines: []string{
				`{"timestamp":"2026-07-13T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":50}}}}`,
				`{"timestamp":"2026-07-13T00:00:11Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":120,"output_tokens":60}}}}`,
				`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5}}}}`,
				`{"timestamp":"2026-07-13T00:00:13Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"output_tokens":70}}}}`,
			},
			want: TokenUsage{InputTokens: 170, OutputTokens: 80},
		},
		{
			name: "one counter reset does not reset monotonic fields",
			lines: []string{
				`{"timestamp":"2026-07-13T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":50}}}}`,
				`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":120,"output_tokens":5}}}}`,
			},
			want: TokenUsage{InputTokens: 20, OutputTokens: 5},
		},
		{
			name: "missing timestamp before boundary is baseline only",
			lines: []string{
				`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":10}}}}`,
				`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"output_tokens":25}}}}`,
			},
			want: TokenUsage{InputTokens: 60, OutputTokens: 15},
		},
		{
			name: "missing timestamp after boundary remains in delta",
			lines: []string{
				`{"timestamp":"2026-07-13T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":10}}}}`,
				`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"output_tokens":25}}}}`,
				`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":180,"output_tokens":30}}}}`,
			},
			want: TokenUsage{InputTokens: 80, OutputTokens: 20},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			content := strings.Join(append(tc.lines, ""), "\n")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			got := parseCodexSessionFileSince(path, startTime, true)
			if got == nil {
				t.Fatal("expected usage")
			}
			if got.usage != tc.want {
				t.Fatalf("usage = %+v, want %+v", got.usage, tc.want)
			}
		})
	}
}

func TestParseCodexSessionFileSinceAllMissingTimestamps(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":10}}}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	startTime := time.Date(2026, time.July, 13, 0, 0, 10, 0, time.UTC)

	if got := parseCodexSessionFileSince(path, startTime, true); got != nil {
		t.Fatalf("ambiguous resumed usage = %+v, want nil", got.usage)
	}
	got := parseCodexSessionFileSince(path, startTime, false)
	if got == nil {
		t.Fatal("fresh rollout should retain timestamp-less usage")
	}
	want := TokenUsage{InputTokens: 100, OutputTokens: 10}
	if got.usage != want {
		t.Fatalf("fresh usage = %+v, want %+v", got.usage, want)
	}
}

func TestScanCodexSessionUsageFindsCrossDayResumeWithCacheOnlyDelta(t *testing.T) {
	t.Parallel()
	taskHome := t.TempDir()
	threadID := "cross-day-thread"
	startTime := time.Date(2026, time.July, 13, 0, 0, 10, 0, time.UTC)
	previousDateDir := filepath.Join(taskHome, "sessions", "2026", "07", "12")
	if err := os.MkdirAll(previousDateDir, 0o755); err != nil {
		t.Fatalf("mkdir previous date dir: %v", err)
	}
	content := strings.Join([]string{
		`{"timestamp":"2026-07-12T23:59:59Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":100}}}}`,
		`{"timestamp":"2026-07-13T00:00:11Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":150}}}}`,
		"",
	}, "\n")
	path := filepath.Join(previousDateDir, "rollout-2026-07-12T23-59-59-"+threadID+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	if err := os.Chtimes(path, startTime.Add(time.Second), startTime.Add(time.Second)); err != nil {
		t.Fatalf("set session mtime: %v", err)
	}

	got := scanCodexSessionUsage(startTime, taskHome, threadID, true)
	if got == nil {
		t.Fatal("expected cross-day cache-only usage")
	}
	want := TokenUsage{CacheReadTokens: 50}
	if got.usage != want {
		t.Fatalf("usage = %+v, want %+v", got.usage, want)
	}
}

func TestScanCodexSessionUsageFollowsLinkedSessionsRoot(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("directory symlink setup requires Windows developer mode")
	}

	root := t.TempDir()
	taskHome := filepath.Join(root, "task-home")
	store := filepath.Join(root, "session-store")
	threadID := "linked-thread"
	startTime := time.Date(2026, time.July, 13, 0, 0, 10, 0, time.UTC)
	dateDir := filepath.Join(store, "2026", "07", "12")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir store date dir: %v", err)
	}
	if err := os.MkdirAll(taskHome, 0o755); err != nil {
		t.Fatalf("mkdir task home: %v", err)
	}
	if err := os.Symlink(store, filepath.Join(taskHome, "sessions")); err != nil {
		t.Fatalf("link sessions root: %v", err)
	}

	content := strings.Join([]string{
		`{"timestamp":"2026-07-13T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":10}}}}`,
		`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"output_tokens":25}}}}`,
		"",
	}, "\n")
	path := filepath.Join(dateDir, "rollout-2026-07-12T23-59-59-"+threadID+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write linked rollout: %v", err)
	}
	if err := os.Chtimes(path, startTime.Add(time.Second), startTime.Add(time.Second)); err != nil {
		t.Fatalf("set rollout mtime: %v", err)
	}

	got := scanCodexSessionUsage(startTime, taskHome, threadID, true)
	if got == nil {
		t.Fatal("expected usage through linked sessions root")
	}
	want := TokenUsage{InputTokens: 60, OutputTokens: 15}
	if got.usage != want {
		t.Fatalf("usage = %+v, want %+v", got.usage, want)
	}
}

func TestScanCodexSessionUsageSelectsCurrentThread(t *testing.T) {
	t.Parallel()
	taskHome := t.TempDir()
	startTime := time.Date(2026, time.July, 13, 0, 0, 10, 0, time.UTC)
	dateDir := filepath.Join(taskHome, "sessions", "2026", "07", "13")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir date dir: %v", err)
	}

	writeRollout := func(threadID string, input int, modTime time.Time) {
		t.Helper()
		content := fmt.Sprintf(`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d}}}}`+"\n", input)
		path := filepath.Join(dateDir, "rollout-2026-07-13T00-00-00-"+threadID+".jsonl")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s rollout: %v", threadID, err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("set %s rollout mtime: %v", threadID, err)
		}
	}

	writeRollout("current-thread", 60, startTime.Add(time.Second))
	writeRollout("newer-subagent-thread", 900, startTime.Add(2*time.Second))

	got := scanCodexSessionUsage(startTime, taskHome, "current-thread", false)
	if got == nil {
		t.Fatal("expected current thread usage")
	}
	want := TokenUsage{InputTokens: 60}
	if got.usage != want {
		t.Fatalf("usage = %+v, want current thread %+v", got.usage, want)
	}
}

func TestScanCodexSessionUsageUsesSessionMetadataWhenFilenameDrifts(t *testing.T) {
	t.Parallel()
	taskHome := t.TempDir()
	startTime := time.Date(2026, time.July, 13, 0, 0, 10, 0, time.UTC)
	dateDir := filepath.Join(taskHome, "sessions", "2026", "07", "13")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatalf("mkdir date dir: %v", err)
	}

	writeRollout := func(filenameID, metadataID string, input int, modTime time.Time) {
		t.Helper()
		content := strings.Join([]string{
			fmt.Sprintf(`{"timestamp":"2026-07-13T00:00:11Z","type":"session_meta","payload":{"id":%q}}`, metadataID),
			fmt.Sprintf(`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d}}}}`, input),
			"",
		}, "\n")
		path := filepath.Join(dateDir, "rollout-2026-07-13T00-00-00-"+filenameID+".jsonl")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s rollout: %v", filenameID, err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("set %s rollout mtime: %v", filenameID, err)
		}
	}

	// The apparent filename match belongs to another thread and must be rejected.
	writeRollout("current-thread", "other-thread", 900, startTime.Add(2*time.Second))
	// A future filename format can still be attributed by canonical session_meta.
	writeRollout("future-filename", "current-thread", 60, startTime.Add(time.Second))

	got := scanCodexSessionUsage(startTime, taskHome, "current-thread", false)
	if got == nil {
		t.Fatal("expected current thread usage from session metadata")
	}
	want := TokenUsage{InputTokens: 60}
	if got.usage != want {
		t.Fatalf("usage = %+v, want metadata-owned thread %+v", got.usage, want)
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

func TestCodexRawItemMCPToolCall(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var messages []Message
	c.onMessage = func(msg Message) {
		messages = append(messages, msg)
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"item/started","params":{"item":{"type":"mcpToolCall","id":"mcp-1","server":"plugin-exa-search","tool":"web_search_exa","arguments":{"query":"latest Multica news","credentials":{"api_key":"sk-12345678901234567890"}},"status":"inProgress"}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"mcpToolCall","id":"mcp-1","server":"plugin-exa-search","tool":"web_search_exa","arguments":{"query":"latest Multica news"},"status":"completed","durationMs":1429,"result":{"content":[{"type":"text","text":"private provider payload"}]}}}}`)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	begin := messages[0]
	if begin.Type != MessageToolUse || begin.Tool != "web_search_exa" || begin.CallID != "mcp-1" {
		t.Fatalf("unexpected start message: %+v", begin)
	}
	if begin.Input["server"] != "plugin-exa-search" {
		t.Fatalf("expected MCP server provenance, got %#v", begin.Input)
	}
	arguments, ok := begin.Input["arguments"].(map[string]any)
	if !ok || arguments["query"] != "latest Multica news" {
		t.Fatalf("expected MCP arguments, got %#v", begin.Input["arguments"])
	}
	credentials, ok := arguments["credentials"].(map[string]any)
	if !ok || credentials["api_key"] != "[REDACTED API KEY]" {
		t.Fatalf("expected nested MCP secret to be redacted, got %#v", arguments["credentials"])
	}

	end := messages[1]
	if end.Type != MessageToolResult || end.Tool != "web_search_exa" || end.CallID != "mcp-1" || end.Status != "completed" {
		t.Fatalf("unexpected complete message: %+v", end)
	}
	if end.Output != "completed\nduration: 1429 ms" {
		t.Fatalf("unexpected MCP result summary: %q", end.Output)
	}
	if strings.Contains(end.Output, "private provider payload") {
		t.Fatalf("MCP result content leaked into transcript summary: %q", end.Output)
	}
}

func TestCodexRawItemMCPToolCallFailureIsSanitized(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	c.handleLine(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"mcpToolCall","id":"mcp-2","server":"plugin-exa-search","tool":"web_search_exa","status":"failed","error":{"message":"Bearer secret-token-value"}}}}`)

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if got := messages[0].Output; got != "failed\nerror: Bearer [REDACTED]" {
		t.Fatalf("unexpected sanitized MCP failure summary: %q", got)
	}
}

func TestCodexRawItemAgentMessageFinalAnswerWaitsForTurnCompleted(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

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
	if turnDone {
		t.Fatal("final_answer must not finish the turn")
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed"}}}`)
	if !turnDone {
		t.Fatal("turn/completed should finish the turn")
	}
}

// TestCodexDeliverableOutputExcludesNarration pins Result.Output to the turn's
// deliverable. Codex used to concatenate every agent message, so a tool-using
// run shipped its intermediate narration to Slack and Lark along with the answer
// (GH #6006). The wiring mirrors executeOnce: onMessage tracks the last agent
// message, onFinalAnswer captures the phase-labelled one, and
// codexDeliverableOutput picks between them.
func TestCodexDeliverableOutputExcludesNarration(t *testing.T) {
	t.Parallel()

	const (
		rawNarration   = `{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"agentMessage","id":"m1","text":"Let me check the logs."}}}`
		rawFinal       = `{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"agentMessage","id":"m2","text":"The retry loop is the cause.","phase":"final_answer"}}}`
		legacyFirst    = `{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"agent_message","message":"Let me check the logs."}}}`
		legacySecond   = `{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"agent_message","message":"The retry loop is the cause."}}}`
		wantDeliverabl = "The retry loop is the cause."
	)

	cases := []struct {
		name     string
		protocol string
		lines    []string
		want     string
	}{
		{
			name:     "raw protocol keeps only the labelled final answer",
			protocol: "raw",
			lines:    []string{rawNarration, rawFinal},
			want:     wantDeliverabl,
		},
		{
			name:     "raw protocol without a final_answer falls back to the last message",
			protocol: "raw",
			lines:    []string{rawNarration, `{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"agentMessage","id":"m2","text":"The retry loop is the cause."}}}`},
			want:     wantDeliverabl,
		},
		{
			name:     "legacy protocol carries no phase and falls back to the last message",
			protocol: "legacy",
			lines:    []string{legacyFirst, legacySecond},
			want:     wantDeliverabl,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c, _, _ := newTestCodexClient(t)
			c.notificationProtocol = tc.protocol

			var finalAnswer, lastAgentMessage string
			var streamed []string
			c.onMessage = func(msg Message) {
				if msg.Type == MessageText {
					lastAgentMessage = msg.Content
					streamed = append(streamed, msg.Content)
				}
			}
			c.onFinalAnswer = func(text string) { finalAnswer = text }

			for _, line := range tc.lines {
				c.handleLine(line)
			}

			if got := codexDeliverableOutput(finalAnswer, lastAgentMessage); got != tc.want {
				t.Fatalf("Result.Output = %q, want %q", got, tc.want)
			}
			// Narrowing delivery must not narrow the transcript: both messages
			// still stream to the timeline the Multica UI renders.
			if len(streamed) != 2 || streamed[0] != "Let me check the logs." {
				t.Fatalf("expected both agent messages streamed, got %q", streamed)
			}
		})
	}
}

func TestCodexRawThreadStatusIdleWaitsForTurnCompleted(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	var turnDone bool
	c.onTurnDone = func(aborted bool) { turnDone = true }

	c.handleLine(`{"jsonrpc":"2.0","method":"thread/status/changed","params":{"status":{"type":"idle"}}}`)

	if turnDone {
		t.Fatal("idle status must not finish the turn")
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"id":"turn-1","status":"completed"}}}`)
	if !turnDone {
		t.Fatal("turn/completed should finish the turn")
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

func TestCodexTurnNotificationGateIgnoresSubagentTurnStarted(t *testing.T) {
	t.Parallel()

	gate := &codexTurnNotificationGate{}
	gate.arm()
	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.threadID = "thr_main"
	c.acceptNotification = gate.accept

	var (
		gotText        string
		doneCount      int
		discardedCount int
	)
	c.onMessage = func(msg Message) {
		if msg.Type == MessageText {
			gotText = msg.Content
		}
	}
	c.onTurnDone = func(aborted bool) {
		doneCount++
	}
	c.onDiscardedNotification = func(string, map[string]any) {
		discardedCount++
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_main","turn":{"id":"turn-main"}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr_subagent","turn":{"id":"turn-sub"}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr_main","turnId":"turn-main","item":{"type":"agentMessage","id":"msg-main","text":"Main answer"}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_main","turn":{"id":"turn-main","status":"completed"}}}`)

	if gate.turnID != "turn-main" {
		t.Fatalf("subagent turn/started replaced gate turnID: got %q", gate.turnID)
	}
	if c.turnID != "turn-main" {
		t.Fatalf("subagent turn/started replaced client turnID: got %q", c.turnID)
	}
	if gotText != "Main answer" {
		t.Fatalf("main turn text was lost after subagent start: got %q", gotText)
	}
	if doneCount != 1 {
		t.Fatalf("main turn completion count = %d, want 1", doneCount)
	}
	if discardedCount != 0 {
		t.Fatalf("subagent notification should be filtered before the gate, discarded callback count = %d", discardedCount)
	}
}

// Regression for #1181: subagent agentMessage/final_answer must not
// trigger turn completion or leak text into the main output stream.
func TestCodexRawItemAgentMessageFinalAnswerFromSubagentIgnored(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	c.threadID = "thr_main"

	var messages []Message
	var doneCount int
	var finalAnswer string
	c.onMessage = func(msg Message) {
		messages = append(messages, msg)
	}
	c.onTurnDone = func(aborted bool) {
		doneCount++
	}
	c.onFinalAnswer = func(text string) {
		finalAnswer = text
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr_subagent","item":{"type":"agentMessage","id":"sub-1","text":"subagent leakage","phase":"final_answer"}}}`)

	if len(messages) != 0 {
		t.Fatalf("subagent text must not leak into output builder, got %+v", messages)
	}
	if doneCount != 0 {
		t.Fatalf("subagent final_answer must not trigger onTurnDone, got %d calls", doneCount)
	}
	// onFinalAnswer selects Result.Output, so a subagent reaching it would ship
	// another thread's answer as this run's reply. It rides the same
	// isNotificationFromOtherThread guard; pin that it stays behind it.
	if finalAnswer != "" {
		t.Fatalf("subagent final_answer must not become the deliverable, got %q", finalAnswer)
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

// codexRuntimeBriefCanary stands in for the Multica runtime brief the daemon
// would inline if developerInstructions were ever wired back up.
const codexRuntimeBriefCanary = "MULTICA-RUNTIME-BRIEF-CANARY"

// assertNoDeveloperInstructions pins the MUL-5392 contract: Codex loads the
// per-task AGENTS.md from the thread's cwd, so the daemon never inlines the
// runtime brief here. The field must still be sent — the app-server treats a
// missing key differently from an explicit null — but always as null.
func assertNoDeveloperInstructions(t *testing.T, params map[string]any) {
	t.Helper()
	got, ok := params["developerInstructions"]
	if !ok {
		t.Error("developerInstructions must be sent explicitly, even when null")
		return
	}
	if got != nil {
		t.Errorf("developerInstructions = %v, want null: the runtime brief is delivered via the workdir AGENTS.md, not inline (MUL-5392)", got)
	}
}

func TestCodexThreadStartNeverInlinesSystemPrompt(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	wait := drainRPCScript(t, c, fs, []rpcResponse{
		{
			method:   "thread/start",
			result:   json.RawMessage(`{"thread":{"id":"thr_fresh"}}`),
			assertFn: assertNoDeveloperInstructions,
		},
	})
	defer wait()

	// SystemPrompt is set deliberately; it must not reach the app-server.
	if _, _, err := c.startOrResumeThread(
		context.Background(),
		ExecOptions{Cwd: "/work", SystemPrompt: codexRuntimeBriefCanary},
		slog.Default(),
	); err != nil {
		t.Fatalf("startOrResumeThread: %v", err)
	}
}

func TestCodexThreadResumeNeverInlinesSystemPrompt(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestCodexClient(t)

	wait := drainRPCScript(t, c, fs, []rpcResponse{
		{
			method:   "thread/resume",
			result:   json.RawMessage(`{"thread":{"id":"thr_prior"}}`),
			assertFn: assertNoDeveloperInstructions,
		},
	})
	defer wait()

	// SystemPrompt is set deliberately; it must not reach the app-server.
	if _, _, err := c.startOrResumeThread(
		context.Background(),
		ExecOptions{Cwd: "/work", ResumeSessionID: "thr_prior", SystemPrompt: codexRuntimeBriefCanary},
		slog.Default(),
	); err != nil {
		t.Fatalf("startOrResumeThread: %v", err)
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
	// Stands in for whatever the daemon computed for this surface; the backend
	// only carries it, so the exact wording is the caller's business.
	const chatNotice = "[System notice] the previous conversation context could not be restored.\n\n"
	text := func(input []map[string]any) string {
		if len(input) != 1 {
			t.Fatalf("expected a single input block, got %d", len(input))
		}
		s, _ := input[0]["text"].(string)
		return s
	}

	// Resume expected but the backend fell back to a fresh thread → disclose,
	// and the original prompt must still be delivered.
	fallback := text(codexTurnInput(prompt, true, false, chatNotice))
	if !strings.Contains(fallback, "previous conversation context could not be restored") {
		t.Errorf("expected continuity notice on resume fallback, got:\n%s", fallback)
	}
	if !strings.HasSuffix(fallback, prompt) {
		t.Errorf("prompt must survive alongside the notice, got:\n%s", fallback)
	}

	// Successful resume, or an ordinary fresh start with no resume expected →
	// no notice, prompt delivered verbatim.
	if got := text(codexTurnInput(prompt, true, true, chatNotice)); got != prompt {
		t.Errorf("successful resume must not add a notice, got:\n%s", got)
	}
	if got := text(codexTurnInput(prompt, false, false, chatNotice)); got != prompt {
		t.Errorf("fresh start must not add a notice, got:\n%s", got)
	}
}

// TestCodexTurnInputNoticeMatchesWhatTheSurfaceLost is the MUL-5722 half of the
// continuity notice. An issue's discussion survives in its comments, which the
// agent re-reads every turn, so ordering it to announce "the previous context
// was lost" tells the user the discussion is gone when none of it is. The
// notice still has to fire — the agent must not silently assume continuity —
// but on that surface it informs the agent instead of scripting an apology.
func TestCodexTurnInputSuppressesNoticeWhenCallerAlreadyDisclosed(t *testing.T) {
	t.Parallel()

	// An empty notice is how the caller says "the prompt already carries it".
	// Honouring that is the backend's half of the no-duplicate guarantee: on
	// the daemon's fresh-session retry the prompt already ends with the
	// continuity notice, and before MUL-5722 this path prepended a second copy
	// of the same paragraph into the same turn.
	const prompt = "do the task"
	input := codexTurnInput(prompt, true, false, "")
	got, _ := input[0]["text"].(string)
	if got != prompt {
		t.Fatalf("empty notice must leave the prompt untouched, got:\n%s", got)
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

func TestResolveCodexHandshakeTimeouts(t *testing.T) {
	tests := []struct {
		name       string
		opts       ExecOptions
		wantBase   time.Duration
		wantThread time.Duration
	}{
		{
			name:       "separate defaults",
			wantBase:   defaultCodexHandshakeTimeout,
			wantThread: defaultCodexThreadHandshakeTimeout,
		},
		{
			name:       "legacy global override remains global",
			opts:       ExecOptions{HandshakeTimeout: 42 * time.Second},
			wantBase:   42 * time.Second,
			wantThread: 42 * time.Second,
		},
		{
			name: "dedicated thread override wins",
			opts: ExecOptions{
				HandshakeTimeout:       30 * time.Second,
				ThreadHandshakeTimeout: 75 * time.Second,
			},
			wantBase:   30 * time.Second,
			wantThread: 75 * time.Second,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, thread := resolveCodexHandshakeTimeouts(tc.opts)
			if base != tc.wantBase || thread != tc.wantThread {
				t.Fatalf("timeouts = (%s, %s), want (%s, %s)", base, thread, tc.wantBase, tc.wantThread)
			}
		})
	}
}

func TestCodexHandshakeTimeoutFor(t *testing.T) {
	c := &codexClient{
		handshakeTimeout:       30 * time.Second,
		threadHandshakeTimeout: 60 * time.Second,
	}
	for _, method := range []string{"thread/start", "thread/resume"} {
		if got := c.handshakeTimeoutFor(method); got != 60*time.Second {
			t.Fatalf("handshakeTimeoutFor(%q) = %s, want 60s", method, got)
		}
	}
	for _, method := range []string{"initialize", "thread/name/set", "turn/start"} {
		if got := c.handshakeTimeoutFor(method); got != 30*time.Second {
			t.Fatalf("handshakeTimeoutFor(%q) = %s, want 30s", method, got)
		}
	}
	c.threadHandshakeTimeout = 0
	for _, method := range []string{"thread/start", "thread/resume"} {
		if got := c.handshakeTimeoutFor(method); got != 30*time.Second {
			t.Fatalf("zero thread timeout handshakeTimeoutFor(%q) = %s, want base 30s fallback", method, got)
		}
	}
}

func TestCodexExecuteThreadStartTimeoutLifecycleIsFailClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	fakePath := writeFakeCodexAppServer(t, ""+
		`DIR="$(dirname "$0")"`+"\n"+
		`echo 1 > "$DIR/attempts"`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo 'ERROR codex_models_manager::manager: failed to refresh available models: timeout waiting for child process to exit' >&2`+"\n"+
		`echo 'ERROR mcp_manager_init: transport error: channel closed' >&2`+"\n"+
		`sleep 5`+"\n"+
		// This response is deliberately later than the host timeout. Killing
		// the process tree must prevent it from reaching turn/start.
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-late"}}}'`+"\n"+
		`read line && echo "$line" > "$DIR/turn-start"`+"\n")

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	backend, err := New("codex", Config{
		ExecutablePath: fakePath,
		Logger:         logger,
		TaskID:         "task-thread-start-timeout",
		RuntimeID:      "runtime-thread-start-timeout",
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "secret prompt must not be logged", ExecOptions{
		Timeout:                   5 * time.Second,
		HandshakeTimeout:          3 * time.Second,
		SemanticInactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" || !strings.Contains(result.Error, "thread/start") {
		t.Fatalf("expected thread/start timeout failure, got %+v", result)
	}
	assertCodexAttemptCount(t, fakePath, "1")
	if _, err := os.Stat(filepath.Join(filepath.Dir(fakePath), "turn-start")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("late response reached turn/start; stat err=%v", err)
	}

	entries := parseJSONLogEntries(t, logs.String())
	sent := findCodexLifecyclePhase(t, entries, "thread_start_sent")
	failure := findCodexLifecyclePhase(t, entries, "thread_start_failure")
	for key, want := range map[string]any{
		"task_id":         "task-thread-start-timeout",
		"runtime_id":      "runtime-thread-start-timeout",
		"attempt":         float64(1),
		"active_launches": float64(1),
		"method":          "thread/start",
	} {
		if got := sent[key]; got != want {
			t.Fatalf("sent[%s]=%v, want %v; entry=%v", key, got, want, sent)
		}
	}
	if failure["cleanup_confirmed"] != true || failure["reaped"] != true {
		t.Fatalf("failure lacks confirmed cleanup/reap: %v", failure)
	}
	if failure["retry_safe"] != false || failure["retry_attempted"] != false {
		t.Fatalf("thread/start timeout must remain fail-closed: %v", failure)
	}
	if failure["stderr_model_refresh_failure_count"] != float64(1) ||
		failure["stderr_model_refresh_timeout_count"] != float64(1) ||
		failure["stderr_mcp_init_transport_count"] != float64(1) ||
		failure["stderr_bare_timeout_count"] != float64(0) {
		t.Fatalf("unexpected stderr classification: %v", failure)
	}
	if _, ok := failure["latency_ms"]; !ok {
		t.Fatalf("failure lacks monotonic latency: %v", failure)
	}
	if strings.Contains(logs.String(), "secret prompt must not be logged") {
		t.Fatalf("prompt leaked into lifecycle logs: %s", logs.String())
	}
}

func TestCodexExecuteThreadResumeTimeoutUsesThreadBudgetAndLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	fakePath := writeFakeCodexAppServer(t, ""+
		`DIR="$(dirname "$0")"`+"\n"+
		`echo 1 > "$DIR/attempts"`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`sleep 5`+"\n")

	var logs bytes.Buffer
	backend, err := New("codex", Config{
		ExecutablePath: fakePath,
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		TaskID:         "task-thread-resume-timeout",
		RuntimeID:      "runtime-thread-resume-timeout",
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
		Timeout:                   5 * time.Second,
		HandshakeTimeout:          3 * time.Second,
		ThreadHandshakeTimeout:    500 * time.Millisecond,
		SemanticInactivityTimeout: time.Second,
		ResumeSessionID:           "thr-prior",
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" || !strings.Contains(result.Error, "thread/resume did not respond after 500ms") {
		t.Fatalf("expected thread/resume timeout failure, got %+v", result)
	}
	assertCodexAttemptCount(t, fakePath, "1")

	entries := parseJSONLogEntries(t, logs.String())
	sent := findCodexLifecyclePhase(t, entries, "thread_resume_sent")
	failure := findCodexLifecyclePhase(t, entries, "thread_resume_failure")
	for key, want := range map[string]any{
		"task_id":    "task-thread-resume-timeout",
		"runtime_id": "runtime-thread-resume-timeout",
		"attempt":    float64(1),
		"method":     "thread/resume",
	} {
		if got := sent[key]; got != want {
			t.Fatalf("sent[%s]=%v, want %v; entry=%v", key, got, want, sent)
		}
	}
	if failure["cleanup_confirmed"] != true || failure["reaped"] != true {
		t.Fatalf("failure lacks confirmed cleanup/reap: %v", failure)
	}
	if failure["retry_safe"] != false || failure["retry_attempted"] != false {
		t.Fatalf("thread/resume timeout must remain fail-closed: %v", failure)
	}
}

func TestCodexExecuteThreadStartResponseRequiresThreadID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{}}}'`+"\n")

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
	if result.Status != "failed" || !strings.Contains(result.Error, "returned no thread ID") {
		t.Fatalf("expected missing thread ID failure, got %+v", result)
	}

	phaseCounts := map[string]int{}
	for _, entry := range parseJSONLogEntries(t, logs.String()) {
		if phase, ok := entry["phase"].(string); ok {
			phaseCounts[phase]++
		}
	}
	if phaseCounts["thread_start_sent"] != 1 || phaseCounts["thread_start_failure"] != 1 {
		t.Fatalf("expected one sent and one failure phase, got %v", phaseCounts)
	}
	if phaseCounts["thread_start_response"] != 0 {
		t.Fatalf("invalid thread/start response emitted success phase: %v", phaseCounts)
	}
}

func TestCodexThreadStartTimeoutDoesNotPersistSensitiveInputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	const envSecret = "opaque-env-sentinel-9382"
	const systemSecret = "opaque-system-sentinel-4721"
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo "$OPAQUE_AUTH_VALUE" >&2`+"\n"+
		`echo "$line" >&2`+"\n"+
		`sleep 5`+"\n")

	var logs bytes.Buffer
	backend, err := New("codex", Config{
		ExecutablePath: fakePath,
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		Env:            map[string]string{"OPAQUE_AUTH_VALUE": envSecret},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
		Timeout:          5 * time.Second,
		HandshakeTimeout: time.Second,
		SystemPrompt:     systemSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	combined := result.Error + "\n" + logs.String()
	for _, secret := range []string{envSecret, systemSecret} {
		if strings.Contains(combined, secret) {
			t.Fatalf("sensitive input persisted in timeout diagnostics")
		}
	}
}

func TestCodexExecuteConcurrentThreadStartTimeoutsRemainUnserialized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`sleep 5`+"\n")

	maxActiveCodexLaunchesObserved.Store(0)
	results := make(chan Result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			backend, err := New("codex", Config{ExecutablePath: fakePath, Logger: slog.Default()})
			if err != nil {
				results <- Result{Status: "failed", Error: err.Error()}
				return
			}
			session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
				Timeout:          5 * time.Second,
				HandshakeTimeout: 3 * time.Second,
			})
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
		result := <-results
		if result.Status != "failed" || !strings.Contains(result.Error, "thread/start") {
			t.Fatalf("concurrent timeout result=%+v", result)
		}
	}
	if got := maxActiveCodexLaunchesObserved.Load(); got < 2 {
		t.Fatalf("thread/start timeout handling serialized launches: max active=%d", got)
	}
}

func parseJSONLogEntries(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse log entry: %v: %q", err, line)
		}
		entries = append(entries, entry)
	}
	return entries
}

func findCodexLifecyclePhase(t *testing.T, entries []map[string]any, phase string) map[string]any {
	t.Helper()
	for _, entry := range entries {
		if entry["msg"] == "codex lifecycle" && entry["phase"] == phase {
			return entry
		}
	}
	t.Fatalf("missing codex lifecycle phase %q in %v", phase, entries)
	return nil
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
		Timeout: 10 * time.Second,
		// Forked shell startup regularly exceeds 100ms on loaded CI runners.
		// Keep the first attempt's 5s hang above this bound while giving the
		// successful second initialize enough scheduling headroom.
		HandshakeTimeout:          2 * time.Second,
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
			`sleep 3.2`+"\n")
		result := executeFakeCodex(t, fakePath, ExecOptions{Timeout: 8 * time.Second, HandshakeTimeout: 3 * time.Second})
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
			`sleep 3.2`+"\n")
		result := executeFakeCodex(t, fakePath, ExecOptions{Timeout: 8 * time.Second, HandshakeTimeout: 3 * time.Second})
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
			`sleep 3.2`+"\n")
		result := executeFakeCodex(t, fakePath, ExecOptions{Timeout: 8 * time.Second, HandshakeTimeout: 3 * time.Second})
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

func TestClassifyCodexStartupStderr(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   codexStderrClassification
	}{
		{
			name:   "model refresh timeout",
			stderr: "failed to refresh available models: timeout waiting for child process to exit",
			want:   codexStderrClassification{modelRefreshFailure: 1, modelRefreshTimeout: 1},
		},
		{
			name:   "model refresh non-timeout failure",
			stderr: "failed to refresh available models: stream disconnected before completion",
			want:   codexStderrClassification{modelRefreshFailure: 1},
		},
		{
			name:   "mcp init transport",
			stderr: "mcp_manager_init transport error: channel closed",
			want:   codexStderrClassification{mcpInitTransport: 1},
		},
		{
			name: "bare timeout",
			want: codexStderrClassification{bareTimeout: 1},
		},
		{
			name: "non-timeout error is not bare timeout",
			want: codexStderrClassification{},
		},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			timedOut := i != len(tests)-1
			if got := classifyCodexStartupStderr(tc.stderr, timedOut); got != tc.want {
				t.Fatalf("classification=%+v, want %+v", got, tc.want)
			}
		})
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
	// Not t.Parallel(): this test mutates codexGracefulShutdownTimeoutNanos.
	// The model catalog signal below makes both attempts retry safe, so this
	// exercises the exhausted-retry path and pays two cleanup windows.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

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
		`sleep 2`+"\n")

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
		"Codex could not load its model catalog",
		"codex stderr:",
		codexModelCatalogRefreshFailureSignal,
	} {
		if !strings.Contains(result.Error, want) {
			t.Fatalf("expected error to contain %q, got %q", want, result.Error)
		}
	}
}

// TestCodexExecuteFirstTurnOverrideAboveSemanticIsTruncated pins the competing-
// timer contract for MULTICA_CODEX_FIRST_TURN_TIMEOUT (GH #3262 / #5959): the
// first status:running arms the semantic-inactivity timer and the first-turn
// timer together, so a first-turn override ABOVE the semantic timeout cannot
// extend the first-item wait — the semantic timer fires first. That also
// reclassifies the failure as semantic inactivity, so the model-catalog startup
// retry (GH #3291) does NOT run even though the catalog-refresh-failure signal is
// present in stderr. The resolver tests cannot observe this; this drives the real
// run loop. It is the inverse of TestCodexExecuteFirstTurnNoProgressSurfacesDiagnostics,
// where the first-turn timer is the smaller of the two and wins.
func TestCodexExecuteFirstTurnOverrideAboveSemanticIsTruncated(t *testing.T) {
	// Not t.Parallel(): this test mutates codexGracefulShutdownTimeoutNanos.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	fakePath := writeFakeCodexAppServer(t, ""+
		`STATE="$(dirname "$0")/attempts"`+"\n"+
		`ATTEMPT=$(cat "$STATE" 2>/dev/null || echo 0)`+"\n"+
		`ATTEMPT=$((ATTEMPT+1))`+"\n"+
		`echo "$ATTEMPT" > "$STATE"`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-trunc"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-trunc","turn":{"id":"turn-trunc"}}}'`+"\n"+
		`echo 'ERROR codex_models_manager::manager: failed to refresh available models: timeout waiting for child process to exit' >&2`+"\n"+
		`sleep 2`+"\n")

	// First-turn override (5s) sits far above the semantic timeout (100ms).
	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                    5 * time.Second,
		SemanticInactivityTimeout:  100 * time.Millisecond,
		FirstTurnNoProgressTimeout: 5 * time.Second,
	})
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	// The semantic timer won the race: the failure must be classified as semantic
	// inactivity, not first-turn no-progress. The override did not extend the wait.
	if !strings.Contains(result.Error, CodexSemanticInactivityMarker) {
		t.Fatalf("expected semantic-inactivity classification, got %q", result.Error)
	}
	if strings.Contains(result.Error, CodexFirstTurnNoProgressMarker) {
		t.Fatalf("first-turn override above the semantic timeout must not win the race: %q", result.Error)
	}
	// The catalog-refresh-failure signal is present, but because the failure is
	// classified as semantic (not first-turn) the #3291 startup retry is skipped:
	// exactly one attempt runs.
	assertCodexAttemptCount(t, fakePath, "1")
}

func TestCodexExecuteFirstItemWaitLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })
	// The helper budget owns the complete subtest, including a possible
	// two-attempt retry chain, while ExecOptions.Timeout applies per attempt.
	// Keep the helper deadline away from race-instrumented subprocess jitter.
	const firstItemWaitTestBudget = 20 * time.Second

	t.Run("successful progress emits a latency sample", func(t *testing.T) {
		fakePath := writeFakeCodexAppServer(t, ""+
			`read line`+"\n"+
			`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
			`read line`+"\n"+
			`read line`+"\n"+
			`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-first-item-ok"}}}'`+"\n"+
			`read line`+"\n"+
			`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
			`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-first-item-ok","turn":{"id":"turn-first-item-ok"}}}'`+"\n"+
			`sleep 0.03`+"\n"+
			`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-first-item-ok","item":{"type":"agentMessage","id":"msg-1","text":"Done"}}}'`+"\n"+
			`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-first-item-ok","turn":{"id":"turn-first-item-ok","status":"completed"}}}'`+"\n")

		var logs bytes.Buffer
		result, _ := executeFakeCodexCollectingMessagesWithConfig(t, fakePath, Config{
			Logger:        slog.New(slog.NewJSONHandler(&logs, nil)),
			TaskID:        "task-first-item-ok",
			RuntimeID:     "runtime-first-item-ok",
			DaemonVersion: "daemon-test",
			CodexVersion:  "codex-test",
		}, ExecOptions{
			Timeout:                   5 * time.Second,
			SemanticInactivityTimeout: 5 * time.Second,
		}, firstItemWaitTestBudget)
		if result.Status != "completed" {
			t.Fatalf("expected completed, got %+v", result)
		}

		entry := findCodexLifecyclePhase(t, parseJSONLogEntries(t, logs.String()), "first_item_wait")
		for key, want := range map[string]any{
			"task_id":                     "task-first-item-ok",
			"runtime_id":                  "runtime-first-item-ok",
			"attempt":                     float64(1),
			"active_launches":             float64(1),
			"method":                      "turn/start",
			"thread_id":                   "thr-first-item-ok",
			"turn_id":                     "turn-first-item-ok",
			"outcome":                     "progress",
			"timeout":                     "4s",
			"semantic_inactivity_timeout": "5s",
			"codex_version":               "codex-test",
			"daemon_version":              "daemon-test",
			"cleanup_confirmed":           true,
			"reaped":                      true,
			"retry_safe":                  false,
		} {
			if got := entry[key]; got != want {
				t.Fatalf("entry[%s]=%v, want %v; entry=%v", key, got, want, entry)
			}
		}
		if latency, ok := entry["latency_ms"].(float64); !ok || latency <= 0 {
			t.Fatalf("missing/invalid latency_ms: %v", entry)
		}
		if entry["stderr_model_refresh_failure_count"] != float64(0) ||
			entry["stderr_model_refresh_timeout_count"] != float64(0) ||
			entry["stderr_mcp_init_transport_count"] != float64(0) ||
			entry["stderr_bare_timeout_count"] != float64(0) {
			t.Fatalf("successful wait has unexpected stderr classification: %v", entry)
		}
	})

	t.Run("catalog failure is classified on every timed-out attempt", func(t *testing.T) {
		fakePath := writeFakeCodexAppServer(t, ""+
			`DIR="$(dirname "$0")"`+"\n"+
			`ATTEMPT=$(cat "$DIR/attempts" 2>/dev/null || echo 0)`+"\n"+
			`ATTEMPT=$((ATTEMPT+1))`+"\n"+
			`echo "$ATTEMPT" > "$DIR/attempts"`+"\n"+
			`read line`+"\n"+
			`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
			`read line`+"\n"+
			`read line`+"\n"+
			`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-first-item-timeout"}}}'`+"\n"+
			`read line`+"\n"+
			`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
			`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-first-item-timeout","turn":{"id":"turn-first-item-timeout"}}}'`+"\n"+
			`echo 'ERROR codex_models_manager::manager: failed to refresh available models: stream disconnected before completion' >&2`+"\n"+
			`sleep 2`+"\n")

		var logs bytes.Buffer
		result, _ := executeFakeCodexCollectingMessagesWithConfig(t, fakePath, Config{
			Logger:        slog.New(slog.NewJSONHandler(&logs, nil)),
			TaskID:        "task-first-item-timeout",
			RuntimeID:     "runtime-first-item-timeout",
			DaemonVersion: "daemon-test",
			CodexVersion:  "codex-test",
		}, ExecOptions{
			Timeout:                   5 * time.Second,
			SemanticInactivityTimeout: 100 * time.Millisecond,
		}, firstItemWaitTestBudget)
		if result.Status != "timeout" {
			t.Fatalf("expected timeout after the bounded retry, got %+v", result)
		}
		assertCodexAttemptCount(t, fakePath, "2")

		var waits []map[string]any
		for _, entry := range parseJSONLogEntries(t, logs.String()) {
			if entry["msg"] == "codex lifecycle" && entry["phase"] == "first_item_wait" {
				waits = append(waits, entry)
			}
		}
		if len(waits) != 2 {
			t.Fatalf("expected one first-item sample per attempt, got %d: %v", len(waits), waits)
		}
		for i, entry := range waits {
			if entry["attempt"] != float64(i+1) || entry["outcome"] != "no_progress_timeout" ||
				entry["retry_safe"] != true || entry["cleanup_confirmed"] != true {
				t.Fatalf("unexpected timeout lifecycle entry: %v", entry)
			}
			if entry["stderr_model_refresh_failure_count"] != float64(1) ||
				entry["stderr_model_refresh_timeout_count"] != float64(0) ||
				entry["stderr_mcp_init_transport_count"] != float64(0) ||
				entry["stderr_bare_timeout_count"] != float64(0) {
				t.Fatalf("catalog failure was misclassified: %v", entry)
			}
		}
	})
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
	// single stdout line larger than agentStreamMaxLineBytes, which trips
	// "bufio.Scanner: token too long" in the reader goroutine.
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
		// Emit a single line past the cap with no embedded newline. printf
		// avoids the trailing newline echo would add; head + tr generates
		// the bulk payload in pure POSIX shell. The size is derived from
		// the production constant so raising the cap cannot silently turn
		// this regression test into a no-op.
		`printf '{"jsonrpc":"2.0","id":2,"result":{"big":"'`+"\n"+
		fmt.Sprintf(`head -c %d /dev/zero | tr '\0' 'x'`, agentStreamMaxLineBytes+1024*1024)+"\n"+
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
	// MUL-5722 layer 2: an unreadable resume response is a rejected resume,
	// and this flag is the positive evidence shouldRetryWithFreshSession
	// requires. Without it #5715's gate stops the retry dead (codex is in
	// neither the ResumeRejected-capable nor the undetectable set), and the
	// next turn resumes the same oversized thread forever.
	if !result.ResumeRejected {
		t.Fatalf("expected ResumeRejected=true so the daemon retries on a fresh session; error=%q",
			result.Error)
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

func TestCodexExecuteDoesNotClaimResumeRejectedWhenOverflowIsNotAResume(t *testing.T) {
	// Not t.Parallel(): mutates codexGracefulShutdownTimeoutNanos globally,
	// same as its sibling above.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// The overflow guard is scoped to resumes on purpose. A run with no prior
	// session that overflows on the thread/start response is a different
	// failure: there is no session pointer to drop, so reporting a resume
	// rejection would send the daemon looking for a cure that does not apply
	// — and ResumeRejected is documented as positive evidence, not a generic
	// "something went wrong" flag.
	codexGracefulShutdownTimeoutNanos.Store(int64(500 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		// Same oversized single line, but answering thread/start rather than
		// thread/resume because the caller passed no ResumeSessionID.
		`printf '{"jsonrpc":"2.0","id":2,"result":{"big":"'`+"\n"+
		fmt.Sprintf(`head -c %d /dev/zero | tr '\0' 'x'`, agentStreamMaxLineBytes+1024*1024)+"\n"+
		`printf '"}}\n'`+"\n"+
		`sleep 30`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Cwd:                       t.TempDir(),
		Timeout:                   30 * time.Second,
		SemanticInactivityTimeout: 5 * time.Second,
	})

	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "token too long") {
		t.Fatalf("expected error to surface scanner overflow cause, got %q", result.Error)
	}
	if result.ResumeRejected {
		t.Fatalf("expected ResumeRejected=false without a prior session, error=%q", result.Error)
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

// TestCodexExecuteRetriesAfterModelCatalogRefreshFailure covers the MUL-5110
// hot path: Codex accepts the turn, fails to load its model catalog, and never
// emits an item. The daemon must retry once and surface the second attempt.
func TestCodexExecuteRetriesAfterModelCatalogRefreshFailure(t *testing.T) {
	// Not t.Parallel(): this test mutates codexGracefulShutdownTimeoutNanos.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	// The fixture counts invocations next to itself so attempt 1 can stall and
	// attempt 2 can succeed on a different thread.
	fakePath := writeFakeCodexAppServer(t, ""+
		`STATE="$(dirname "$0")/attempts"`+"\n"+
		`ATTEMPT=$(cat "$STATE" 2>/dev/null || echo 0)`+"\n"+
		`ATTEMPT=$((ATTEMPT+1))`+"\n"+
		`echo "$ATTEMPT" > "$STATE"`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`if [ "$ATTEMPT" = "1" ]; then`+"\n"+
		`  echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-cold"}}}'`+"\n"+
		`  read line`+"\n"+
		`  echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`  echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-cold","turn":{"id":"turn-cold"}}}'`+"\n"+
		`  echo 'ERROR codex_models_manager::manager: failed to refresh available models: stream disconnected before completion' >&2`+"\n"+
		`  sleep 2`+"\n"+
		`else`+"\n"+
		`  echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-warm"}}}'`+"\n"+
		`  read line`+"\n"+
		`  echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`  echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-warm","turn":{"id":"turn-warm"}}}'`+"\n"+
		`  echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-warm","item":{"type":"agentMessage","id":"msg-1","text":"Recovered"}}}'`+"\n"+
		`  echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-warm","turn":{"id":"turn-warm","status":"completed"}}}'`+"\n"+
		`fi`+"\n")

	result, messages := executeFakeCodexCollectingMessages(t, fakePath, ExecOptions{
		Timeout:                   20 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	}, 20*time.Second)

	if result.Status != "completed" {
		t.Fatalf("expected the retry to complete, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "Recovered" {
		t.Fatalf("expected output from the second attempt, got %q", result.Output)
	}
	if result.SessionID != "thr-warm" {
		t.Fatalf("expected the surviving thread id, got %q", result.SessionID)
	}
	// The discarded attempt must not pin the resume pointer to a thread that
	// never produced a turn.
	for _, msg := range messages {
		if msg.SessionID == "thr-cold" {
			t.Fatalf("discarded attempt leaked a session pin for thr-cold: %+v", msg)
		}
	}
}

// TestCodexExecuteRetryAfterCatalogFailureStartsFreshThreadForResume covers the
// resume case: the stalled attempt already reached turn/started on the prior
// thread, so the retry must not resume it again. It starts a fresh thread and
// keeps ResumeExpected so the lost context is disclosed to the agent.
func TestCodexExecuteRetryAfterCatalogFailureStartsFreshThreadForResume(t *testing.T) {
	// Not t.Parallel(): this test mutates codexGracefulShutdownTimeoutNanos.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	// Each invocation records the JSON-RPC lines it received so the test can
	// assert which thread RPC the retry used.
	fakePath := writeFakeCodexAppServer(t, ""+
		`DIR="$(dirname "$0")"`+"\n"+
		`ATTEMPT=$(cat "$DIR/attempts" 2>/dev/null || echo 0)`+"\n"+
		`ATTEMPT=$((ATTEMPT+1))`+"\n"+
		`echo "$ATTEMPT" > "$DIR/attempts"`+"\n"+
		`LOG="$DIR/rpc-$ATTEMPT.log"`+"\n"+
		`read line; echo "$line" >> "$LOG"`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line; echo "$line" >> "$LOG"`+"\n"+
		`read line; echo "$line" >> "$LOG"`+"\n"+
		`if [ "$ATTEMPT" = "1" ]; then`+"\n"+
		`  echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-prior"}}}'`+"\n"+
		`  read line; echo "$line" >> "$LOG"`+"\n"+
		`  echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`  echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-prior","turn":{"id":"turn-prior"}}}'`+"\n"+
		`  echo 'ERROR codex_models_manager::manager: failed to refresh available models: timeout waiting for child process to exit' >&2`+"\n"+
		`  sleep 2`+"\n"+
		`else`+"\n"+
		`  echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-fresh"}}}'`+"\n"+
		`  read line; echo "$line" >> "$LOG"`+"\n"+
		`  echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`  echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-fresh","turn":{"id":"turn-fresh"}}}'`+"\n"+
		`  echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-fresh","item":{"type":"agentMessage","id":"msg-1","text":"Fresh"}}}'`+"\n"+
		`  echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-fresh","turn":{"id":"turn-fresh","status":"completed"}}}'`+"\n"+
		`fi`+"\n")

	result, _ := executeFakeCodexCollectingMessages(t, fakePath, ExecOptions{
		ResumeSessionID: "thr-prior",
		ResumeExpected:  true,
		// Supplied by the daemon since MUL-5722: this package no longer holds
		// the wording, because only the caller knows whether the surface's
		// conversation can still be read.
		ResumeContinuityNotice:    "[System notice] the previous conversation context could not be restored.\n\n",
		Timeout:                   20 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	}, 20*time.Second)

	if result.Status != "completed" {
		t.Fatalf("expected the retry to complete, got status=%q error=%q", result.Status, result.Error)
	}
	if result.SessionID != "thr-fresh" {
		t.Fatalf("expected the retry to land on a fresh thread, got %q", result.SessionID)
	}
	dir := filepath.Dir(fakePath)
	first, err := os.ReadFile(filepath.Join(dir, "rpc-1.log"))
	if err != nil {
		t.Fatalf("read first attempt rpc log: %v", err)
	}
	if !strings.Contains(string(first), "thread/resume") {
		t.Fatalf("expected the first attempt to resume the prior thread, got %s", first)
	}
	second, err := os.ReadFile(filepath.Join(dir, "rpc-2.log"))
	if err != nil {
		t.Fatalf("read second attempt rpc log: %v", err)
	}
	if strings.Contains(string(second), "thread/resume") {
		t.Fatalf("retry must not resume the stalled thread, got %s", second)
	}
	if !strings.Contains(string(second), "thread/start") {
		t.Fatalf("expected the retry to start a fresh thread, got %s", second)
	}
	// ResumeExpected survives the cleared pointer, so the caller's notice is
	// still prepended and the agent is told the prior context is gone.
	if !strings.Contains(string(second), "previous conversation context could not be restored") {
		t.Fatalf("expected the retry input to carry the continuity notice, got %s", second)
	}
}

// TestCodexExecuteDoesNotRetryCatalogFailureWhenCleanupUnconfirmed keeps the
// retry gate tied to a reaped process tree: a surviving app-server would race
// the second attempt, so an unconfirmed cleanup must not retry.
func TestCodexExecuteDoesNotRetryCatalogFailureWhenCleanupUnconfirmed(t *testing.T) {
	// Not t.Parallel(): this test mutates package-level Codex overrides.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })
	codexCleanupConfirmationOverride.Store(-1)
	t.Cleanup(func() { codexCleanupConfirmationOverride.Store(0) })

	fakePath := writeFakeCodexAppServer(t, ""+
		`DIR="$(dirname "$0")"`+"\n"+
		`ATTEMPT=$(cat "$DIR/attempts" 2>/dev/null || echo 0)`+"\n"+
		`ATTEMPT=$((ATTEMPT+1))`+"\n"+
		`echo "$ATTEMPT" > "$DIR/attempts"`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-unreaped"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-unreaped","turn":{"id":"turn-unreaped"}}}'`+"\n"+
		`echo 'ERROR codex_models_manager::manager: failed to refresh available models: timeout waiting for child process to exit' >&2`+"\n"+
		`sleep 2`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	})
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	assertCodexAttemptCount(t, fakePath, "1")
}

// TestCodexExecuteDoesNotRetryCatalogFailureAfterSemanticProgress is the core
// no-duplicate-side-effects guard: once a turn has produced content or run a
// tool, the catalog signal must not buy it a replay.
func TestCodexExecuteDoesNotRetryCatalogFailureAfterSemanticProgress(t *testing.T) {
	// Not t.Parallel(): this test mutates codexGracefulShutdownTimeoutNanos.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	fakePath := writeFakeCodexAppServer(t, ""+
		`DIR="$(dirname "$0")"`+"\n"+
		`ATTEMPT=$(cat "$DIR/attempts" 2>/dev/null || echo 0)`+"\n"+
		`ATTEMPT=$((ATTEMPT+1))`+"\n"+
		`echo "$ATTEMPT" > "$DIR/attempts"`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-worked"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-worked","turn":{"id":"turn-worked"}}}'`+"\n"+
		// A tool ran: this turn has side effects and must never be replayed,
		// even though the catalog signal is present in stderr.
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-worked","item":{"type":"commandExecution","id":"cmd-1","aggregatedOutput":"deployed"}}}'`+"\n"+
		`echo 'ERROR codex_models_manager::manager: failed to refresh available models: timeout waiting for child process to exit' >&2`+"\n"+
		`sleep 2`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	})
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	assertCodexAttemptCount(t, fakePath, "1")
}

func assertCodexAttemptCount(t *testing.T, fakePath, want string) {
	t.Helper()
	attempts, err := os.ReadFile(filepath.Join(filepath.Dir(fakePath), "attempts"))
	if err != nil {
		t.Fatalf("read attempt counter: %v", err)
	}
	if got := strings.TrimSpace(string(attempts)); got != want {
		t.Fatalf("expected %s attempt(s), got %q", want, got)
	}
}

// TestCodexExecuteDoesNotRetryFirstTurnNoProgressWithoutCatalogSignal keeps the
// retry gate narrow: a stalled first turn with no model catalog evidence in
// stderr is not known to be transient, so it must fail on the first attempt.
func TestCodexExecuteDoesNotRetryFirstTurnNoProgressWithoutCatalogSignal(t *testing.T) {
	// Not t.Parallel(): this test mutates codexGracefulShutdownTimeoutNanos.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	fakePath := writeFakeCodexAppServer(t, ""+
		`STATE="$(dirname "$0")/attempts"`+"\n"+
		`ATTEMPT=$(cat "$STATE" 2>/dev/null || echo 0)`+"\n"+
		`ATTEMPT=$((ATTEMPT+1))`+"\n"+
		`echo "$ATTEMPT" > "$STATE"`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-quiet"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-quiet","turn":{"id":"turn-quiet"}}}'`+"\n"+
		`echo 'ERROR something else entirely went wrong' >&2`+"\n"+
		`sleep 2`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	})
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	assertCodexAttemptCount(t, fakePath, "1")
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

func TestCodexExecuteResumeIgnoresPreviousTurnCompletion(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-resumed","turn":{"id":"turn-previous","status":"completed"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-resumed"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-resumed","turn":{"id":"turn-current"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-resumed","turnId":"turn-current","item":{"type":"agentMessage","id":"msg-current","text":"Current answer"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-resumed","turn":{"id":"turn-current","status":"completed"}}}'`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		ResumeSessionID:           "thr-resumed",
		ResumeExpected:            true,
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	})
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "Current answer" {
		t.Fatalf("expected current-turn output, got %q", result.Output)
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
	result, _ := executeFakeCodexCollectingMessages(t, fakePath, opts, 10*time.Second)
	return result
}

// executeFakeCodexCollectingMessages runs the fixture and returns the streamed
// messages alongside the result. budget bounds both the execution context and
// the wait, so retry-exercising tests can ask for more than the default.
func executeFakeCodexCollectingMessages(t *testing.T, fakePath string, opts ExecOptions, budget time.Duration) (Result, []Message) {
	t.Helper()
	return executeFakeCodexCollectingMessagesWithConfig(t, fakePath, Config{Logger: slog.Default()}, opts, budget)
}

func executeFakeCodexCollectingMessagesWithConfig(t *testing.T, fakePath string, cfg Config, opts ExecOptions, budget time.Duration) (Result, []Message) {
	t.Helper()
	cfg.ExecutablePath = fakePath
	backend, err := New("codex", cfg)
	if err != nil {
		t.Fatalf("new codex backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var (
		mu       sync.Mutex
		messages []Message
	)
	go func() {
		for msg := range session.Messages {
			mu.Lock()
			messages = append(messages, msg)
			mu.Unlock()
		}
	}()
	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		mu.Lock()
		collected := append([]Message(nil), messages...)
		mu.Unlock()
		return result, collected
	case <-time.After(budget):
		t.Fatal("timeout waiting for result")
		return Result{}, nil
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

func TestBuildCodexArgsExplicitFastOverridesLowerPriorityDisable(t *testing.T) {
	t.Parallel()

	args := buildCodexArgs(ExecOptions{
		ServiceTier: codexFastServiceTier,
		ExtraArgs: []string{
			"--disable", codexFastModeFeature,
			"--disable", "memory_tool",
			"-c", "features.fast_mode=false",
			"-c", "features.memory_tool=false",
		},
		CustomArgs: []string{
			"'--disable=fast_mode'",
			`--config=features.fast_mode="false"`,
			"--enable", "multi_agent",
		},
	}, slog.Default())

	want := []string{
		"app-server", "--listen", "stdio://",
		"--disable", "memory_tool",
		"-c", "features.memory_tool=false",
		"--enable", "multi_agent",
		"--enable", codexFastModeFeature,
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("buildCodexArgs explicit Fast = %v, want %v", args, want)
	}
}

func TestBuildCodexArgsFutureServiceTierDoesNotForceFastMode(t *testing.T) {
	t.Parallel()

	args := buildCodexArgs(ExecOptions{
		ServiceTier: "future-tier",
		CustomArgs:  []string{"--disable", codexFastModeFeature},
	}, slog.Default())

	want := []string{"app-server", "--listen", "stdio://", "--disable", codexFastModeFeature}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("buildCodexArgs future tier = %v, want %v", args, want)
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

// TestNormalizeCodexLaunchArgsStripsShellQuotes locks the normalization the
// daemon reuses for the Windows sandbox decision (MUL-4957): a shell-quoted
// `-c windows.sandbox=...` opt-in must come out as clean tokens, and
// buildCodexArgs must be exactly the transport prefix followed by this result,
// so the sandbox decision and the launch argv can never diverge.
func TestNormalizeCodexLaunchArgsStripsShellQuotes(t *testing.T) {
	t.Parallel()

	got := NormalizeCodexLaunchArgs(nil, []string{"'-c'", "'windows.sandbox=unelevated'"}, nil, slog.Default())
	want := []string{"-c", "windows.sandbox=unelevated"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeCodexLaunchArgs = %v, want %v", got, want)
	}

	launch := buildCodexArgs(ExecOptions{CustomArgs: []string{"'-c'", "'windows.sandbox=unelevated'"}}, slog.Default())
	wantLaunch := append([]string{"app-server", "--listen", "stdio://"}, want...)
	if !reflect.DeepEqual(launch, wantLaunch) {
		t.Fatalf("buildCodexArgs = %v, want %v", launch, wantLaunch)
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

// --- patch_apply payload capture (GH #6157) -------------------------------
//
// Both Codex protocols used to record a file edit as a bare call ID, leaving
// the transcript with two unexpandable blank rows per edit. These tests pin the
// payload on both paths, including the two shapes that differ between them:
// legacy reports add/delete as whole-file content under a path-keyed map, while
// v2 reports an ordered array whose `kind` is an object rather than a string.

// codexTestChanges extracts the normalized change list from a tool_use input.
func codexTestChanges(t *testing.T, msg Message) []map[string]any {
	t.Helper()
	if msg.Input == nil {
		t.Fatalf("expected input on tool_use message, got nil: %+v", msg)
	}
	raw, ok := msg.Input["changes"].([]any)
	if !ok {
		t.Fatalf("expected changes slice, got %#v", msg.Input["changes"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		m, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("expected change to be an object, got %#v", entry)
		}
		out = append(out, m)
	}
	return out
}

func TestCodexLegacyPatchApplyRecordsChanges(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_begin","call_id":"p1","auto_approved":true,"changes":{"src/b.go":{"type":"update","unified_diff":"@@ -1 +1 @@\n-old\n+new\n","move_path":"src/c.go"},"src/a.txt":{"type":"add","content":"hello"}}}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_end","call_id":"p1","stdout":"Success. Updated the following files:\nM src/b.go","stderr":"","success":true,"status":"completed","changes":{"src/b.go":{"type":"update","unified_diff":"@@ -1 +1 @@\n-old\n+new\n"},"src/a.txt":{"type":"add","content":"hello"}}}}}`)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	begin := messages[0]
	if begin.Type != MessageToolUse || begin.Tool != "patch_apply" || begin.CallID != "p1" {
		t.Fatalf("unexpected begin message: %+v", begin)
	}
	changes := codexTestChanges(t, begin)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %#v", len(changes), changes)
	}
	// Sorted by path so a replayed event does not reshuffle the file list.
	if changes[0]["path"] != "src/a.txt" || changes[1]["path"] != "src/b.go" {
		t.Fatalf("changes not sorted by path: %#v", changes)
	}
	if changes[0]["kind"] != "add" || changes[0]["content"] != "hello" {
		t.Fatalf("add change lost its whole-file content: %#v", changes[0])
	}
	if _, hasDiff := changes[0]["diff"]; hasDiff {
		t.Fatalf("legacy add must not invent a diff: %#v", changes[0])
	}
	if changes[1]["kind"] != "update" {
		t.Fatalf("expected update kind, got %#v", changes[1]["kind"])
	}
	if diff, _ := changes[1]["diff"].(string); !strings.Contains(diff, "-old") || !strings.Contains(diff, "+new") {
		t.Fatalf("update change lost its unified diff: %#v", changes[1])
	}
	if changes[1]["move_path"] != "src/c.go" {
		t.Fatalf("expected move_path to be preserved: %#v", changes[1])
	}

	end := messages[1]
	if end.Type != MessageToolResult || end.CallID != "p1" {
		t.Fatalf("unexpected end message: %+v", end)
	}
	if end.Output == "" {
		t.Fatal("patch_apply_end output is empty, transcript row stays unexpandable")
	}
	if !strings.Contains(end.Output, "completed") {
		t.Fatalf("expected status in output, got %q", end.Output)
	}
	if !strings.Contains(end.Output, "Success. Updated the following files:") {
		t.Fatalf("expected apply_patch stdout in output, got %q", end.Output)
	}
}

func TestCodexLegacyPatchApplyDeleteKeepsContent(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_begin","call_id":"p1","auto_approved":true,"changes":{"old.txt":{"type":"delete","content":"gone"}}}}}`)

	changes := codexTestChanges(t, messages[0])
	if len(changes) != 1 || changes[0]["kind"] != "delete" || changes[0]["content"] != "gone" {
		t.Fatalf("delete change not captured: %#v", changes)
	}
}

func TestCodexLegacyPatchApplyStatusFallsBackToSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		event   string
		wantSub string
	}{
		{
			name:    "success true without status",
			event:   `{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_end","call_id":"p1","stdout":"","stderr":"","success":true,"changes":{"a.txt":{"type":"add","content":"x"}}}}}`,
			wantSub: "completed",
		},
		{
			name:    "success false without status",
			event:   `{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_end","call_id":"p1","stdout":"","stderr":"parse error","success":false,"changes":{"a.txt":{"type":"add","content":"x"}}}}}`,
			wantSub: "failed",
		},
		{
			name:    "explicit declined status wins",
			event:   `{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_end","call_id":"p1","stdout":"","stderr":"","success":false,"status":"declined","changes":{}}}}`,
			wantSub: "declined",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, _, _ := newTestCodexClient(t)
			var messages []Message
			c.onMessage = func(msg Message) { messages = append(messages, msg) }

			c.handleLine(tc.event)

			if len(messages) != 1 {
				t.Fatalf("expected 1 message, got %d", len(messages))
			}
			if !strings.Contains(messages[0].Output, tc.wantSub) {
				t.Fatalf("expected output to contain %q, got %q", tc.wantSub, messages[0].Output)
			}
		})
	}
}

func TestCodexLegacyPatchApplyStderrSurfaced(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_end","call_id":"p1","stdout":"","stderr":"apply_patch: could not parse hunk","success":false,"status":"failed","changes":{}}}}`)

	if !strings.Contains(messages[0].Output, "could not parse hunk") {
		t.Fatalf("expected stderr in output, got %q", messages[0].Output)
	}
}

func TestCodexRawPatchApplyRecordsChanges(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	// Fixtures follow upstream convert_patch_changes: `diff` holds a unified
	// diff only for `update`. For `add` it is the whole file's contents.
	c.handleLine(`{"jsonrpc":"2.0","method":"item/started","params":{"item":{"type":"fileChange","id":"patch-1","status":"inProgress","changes":[{"path":"src/a.go","kind":{"type":"update","move_path":null},"diff":"@@ -1 +1 @@\n-a\n+b\n"},{"path":"src/new.go","kind":{"type":"add"},"diff":"package main\n"}]}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"fileChange","id":"patch-1","status":"completed","changes":[{"path":"src/a.go","kind":{"type":"update"},"diff":"@@ -1 +1 @@\n-a\n+b\n"},{"path":"src/new.go","kind":{"type":"add"},"diff":"package main\n"}]}}}`)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	begin := messages[0]
	if begin.Type != MessageToolUse || begin.Tool != "patch_apply" || begin.CallID != "patch-1" {
		t.Fatalf("unexpected start message: %+v", begin)
	}
	changes := codexTestChanges(t, begin)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %#v", len(changes), changes)
	}
	if changes[0]["path"] != "src/a.go" || changes[1]["path"] != "src/new.go" {
		t.Fatalf("v2 change order not preserved: %#v", changes)
	}
	// `kind` arrives as an object; reading it as a string yields "" and loses
	// the add/delete/update distinction.
	if changes[0]["kind"] != "update" {
		t.Fatalf("expected kind flattened to \"update\", got %#v", changes[0]["kind"])
	}
	if diff, _ := changes[0]["diff"].(string); !strings.Contains(diff, "+b") {
		t.Fatalf("v2 update diff not captured: %#v", changes[0])
	}
	// The add's payload is file content, so it must NOT be labelled a diff:
	// a diff parser would mislabel every line and invert any line starting
	// with '+' or '-'.
	if changes[1]["kind"] != "add" {
		t.Fatalf("expected kind flattened to \"add\", got %#v", changes[1]["kind"])
	}
	if _, isDiff := changes[1]["diff"]; isDiff {
		t.Fatalf("v2 add must not be recorded as a diff: %#v", changes[1])
	}
	if changes[1]["content"] != "package main\n" {
		t.Fatalf("v2 add content not captured: %#v", changes[1])
	}

	end := messages[1]
	if end.Type != MessageToolResult || end.CallID != "patch-1" {
		t.Fatalf("unexpected complete message: %+v", end)
	}
	if end.Output == "" {
		t.Fatal("fileChange completion output is empty, transcript row stays unexpandable")
	}
	if !strings.Contains(end.Output, "completed") || !strings.Contains(end.Output, "2 files") {
		t.Fatalf("expected status and file count, got %q", end.Output)
	}
}

// Upstream format_file_change_diff returns whole-file contents for add and
// delete under the `diff` field. Treating that as a unified diff renders plain
// lines as context, and inverts any line that begins with '+' or '-'.
func TestCodexRawPatchApplyContentKindsAreNotDiffs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind string
		body string
	}{
		{name: "add", kind: "add", body: "package main\n"},
		{name: "delete", kind: "delete", body: "goodbye\n"},
		{
			name: "add whose content looks like a diff",
			kind: "add",
			body: "--- not a header\n+++ also not\n-minus lead\n+plus lead\n",
		},
		{name: "empty add", kind: "add", body: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, _, _ := newTestCodexClient(t)
			c.notificationProtocol = "raw"
			var messages []Message
			c.onMessage = func(msg Message) { messages = append(messages, msg) }

			event, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"method":  "item/started",
				"params": map[string]any{
					"item": map[string]any{
						"type": "fileChange", "id": "patch-1", "status": "inProgress",
						"changes": []any{
							map[string]any{
								"path": "f.txt",
								"kind": map[string]any{"type": tc.kind},
								"diff": tc.body,
							},
						},
					},
				},
			})
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			c.handleLine(string(event))

			changes := codexTestChanges(t, messages[0])
			if len(changes) != 1 {
				t.Fatalf("expected 1 change, got %#v", changes)
			}
			if _, isDiff := changes[0]["diff"]; isDiff {
				t.Fatalf("%s must be recorded as content, not diff: %#v", tc.kind, changes[0])
			}
			content, ok := changes[0]["content"].(string)
			if !ok {
				// Presence, not non-emptiness: an empty added file still has a
				// body, and dropping it would render as "no content reported".
				t.Fatalf("expected content to be present for %s: %#v", tc.kind, changes[0])
			}
			if content != tc.body {
				t.Fatalf("content altered: got %q want %q", content, tc.body)
			}
		})
	}
}

// A moved update has "\n\nMoved to: <path>" appended to its diff upstream. The
// destination is already carried as move_path, and leaving the sentence in
// renders as two stray context rows at the end of the diff.
func TestCodexRawPatchApplyStripsMovedToSuffix(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	c.handleLine(`{"jsonrpc":"2.0","method":"item/started","params":{"item":{"type":"fileChange","id":"patch-1","status":"inProgress","changes":[{"path":"old/name.go","kind":{"type":"update","move_path":"new/name.go"},"diff":"@@ -1 +1 @@\n-x\n+y\n\n\nMoved to: new/name.go"}]}}}`)

	changes := codexTestChanges(t, messages[0])
	if changes[0]["move_path"] != "new/name.go" {
		t.Fatalf("expected move_path lifted out of kind, got %#v", changes[0])
	}
	diff, _ := changes[0]["diff"].(string)
	if strings.Contains(diff, "Moved to:") {
		t.Fatalf("Moved to: sentence should be stripped from the diff: %q", diff)
	}
	if !strings.Contains(diff, "+y") {
		t.Fatalf("stripping removed real diff content: %q", diff)
	}
}

func TestCodexRawPatchApplyMovePathFromKind(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	c.handleLine(`{"jsonrpc":"2.0","method":"item/started","params":{"item":{"type":"fileChange","id":"patch-1","status":"inProgress","changes":[{"path":"old/name.go","kind":{"type":"update","move_path":"new/name.go"},"diff":"@@ -1 +1 @@\n-x\n+y\n"}]}}}`)

	changes := codexTestChanges(t, messages[0])
	if changes[0]["move_path"] != "new/name.go" {
		t.Fatalf("expected move_path lifted out of kind, got %#v", changes[0])
	}
}

func TestCodexLegacyPatchApplyKeepsEmptyAddedFile(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	c.handleLine(`{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_begin","call_id":"p1","auto_approved":true,"changes":{"empty.txt":{"type":"add","content":""}}}}}`)

	changes := codexTestChanges(t, messages[0])
	content, ok := changes[0]["content"].(string)
	if !ok || content != "" {
		t.Fatalf("empty added file should keep an empty body: %#v", changes[0])
	}
}

func TestCodexRawPatchApplyStatusNormalized(t *testing.T) {
	t.Parallel()

	tests := []struct{ status, want string }{
		{"inProgress", "in_progress"},
		{"completed", "completed"},
		{"failed", "failed"},
		{"declined", "declined"},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			c, _, _ := newTestCodexClient(t)
			c.notificationProtocol = "raw"
			var messages []Message
			c.onMessage = func(msg Message) { messages = append(messages, msg) }

			c.handleLine(fmt.Sprintf(`{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"type":"fileChange","id":"patch-1","status":%q,"changes":[{"path":"a.go","kind":{"type":"add"},"diff":"+x"}]}}}`, tc.status))

			if len(messages) != 1 {
				t.Fatalf("expected 1 message, got %d", len(messages))
			}
			if !strings.Contains(messages[0].Output, tc.want) {
				t.Fatalf("status %q: expected output to contain %q, got %q", tc.status, tc.want, messages[0].Output)
			}
		})
	}
}

// A missing or malformed `changes` must degrade to the previous payload-less
// behaviour rather than panicking or emitting a broken payload.
func TestCodexPatchApplyMalformedChangesDegrade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event string
		raw   bool
	}{
		{
			name:  "legacy changes absent",
			event: `{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_begin","call_id":"p1","auto_approved":true}}}`,
		},
		{
			name:  "legacy changes wrong type",
			event: `{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_begin","call_id":"p1","changes":"not-an-object"}}}`,
		},
		{
			name:  "legacy changes empty",
			event: `{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_begin","call_id":"p1","changes":{}}}}`,
		},
		{
			name:  "legacy change entry not an object",
			event: `{"jsonrpc":"2.0","method":"codex/event","params":{"msg":{"type":"patch_apply_begin","call_id":"p1","changes":{"a.txt":42}}}}`,
		},
		{
			name:  "raw changes absent",
			event: `{"jsonrpc":"2.0","method":"item/started","params":{"item":{"type":"fileChange","id":"patch-1"}}}`,
			raw:   true,
		},
		{
			name:  "raw changes wrong type",
			event: `{"jsonrpc":"2.0","method":"item/started","params":{"item":{"type":"fileChange","id":"patch-1","changes":{"path":"a.go"}}}}`,
			raw:   true,
		},
		{
			name:  "raw change entries unusable",
			event: `{"jsonrpc":"2.0","method":"item/started","params":{"item":{"type":"fileChange","id":"patch-1","changes":[42,null]}}}`,
			raw:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, _, _ := newTestCodexClient(t)
			if tc.raw {
				c.notificationProtocol = "raw"
			}
			var messages []Message
			c.onMessage = func(msg Message) { messages = append(messages, msg) }

			c.handleLine(tc.event)

			if len(messages) != 1 {
				t.Fatalf("expected the tool_use message to still be emitted, got %d", len(messages))
			}
			if messages[0].Type != MessageToolUse || messages[0].Tool != "patch_apply" {
				t.Fatalf("unexpected message: %+v", messages[0])
			}
			if messages[0].Input != nil {
				t.Fatalf("expected nil input for unusable changes, got %#v", messages[0].Input)
			}
		})
	}
}

func TestCodexPatchApplyTruncatesOversizePayload(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	// Legacy add/delete report whole-file content, so a single generated file
	// can dwarf the transcript budget.
	huge := strings.Repeat("x", codexPatchInputMaxBytes+5000)
	event, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "item/started",
		"params": map[string]any{
			"item": map[string]any{
				"type":   "fileChange",
				"id":     "patch-1",
				"status": "inProgress",
				"changes": []any{
					map[string]any{"path": "big.txt", "kind": map[string]any{"type": "add"}, "diff": huge},
					map[string]any{"path": "second.txt", "kind": map[string]any{"type": "add"}, "diff": "small"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	c.handleLine(string(event))

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	input := messages[0].Input
	if input["truncated"] != true {
		t.Fatalf("expected truncated marker, got %#v", input["truncated"])
	}
	if input["original_bytes"] == nil {
		t.Fatal("expected original_bytes to be recorded alongside truncation")
	}

	changes := codexTestChanges(t, messages[0])
	total := 0
	for _, change := range changes {
		// An `add` is recorded as content, not diff, so both keys count
		// toward the budget.
		for _, key := range []string{"diff", "content"} {
			body, ok := change[key].(string)
			if !ok {
				continue
			}
			total += len(body)
			if !utf8.ValidString(body) {
				t.Fatalf("truncation split a rune in %v", change["path"])
			}
		}
	}
	if total == 0 {
		t.Fatal("expected some body to survive truncation")
	}
	if total > codexPatchInputMaxBytes {
		t.Fatalf("payload %d bytes exceeds budget %d", total, codexPatchInputMaxBytes)
	}
	// The path survives even when its body is dropped: it is the part a
	// reviewer still needs.
	if changes[0]["path"] != "big.txt" || changes[1]["path"] != "second.txt" {
		t.Fatalf("paths must survive truncation: %#v", changes)
	}
	if changes[0]["truncated"] != true {
		t.Fatalf("expected per-change truncation marker: %#v", changes[0])
	}
}

func TestCodexPatchApplyTruncationKeepsValidUTF8(t *testing.T) {
	t.Parallel()

	// Multi-byte runes straddling the cut must not leave an invalid string,
	// or the payload fails to marshal into the JSONB column.
	body := strings.Repeat("世", codexPatchInputMaxBytes)
	changes := []any{
		map[string]any{"path": "wide.txt", "kind": "add", "content": body},
	}
	input := codexPatchInput(changes)
	if input["truncated"] != true {
		t.Fatalf("expected truncation for %d-byte body", len(body))
	}
	for _, kept := range codexTestBodies(t, input) {
		if !utf8.ValidString(kept) {
			t.Fatal("truncated content is not valid UTF-8")
		}
		if len(kept) > codexPatchInputMaxBytes {
			t.Fatalf("kept %d bytes, budget is %d", len(kept), codexPatchInputMaxBytes)
		}
	}
	if _, err := json.Marshal(input); err != nil {
		t.Fatalf("truncated payload does not marshal: %v", err)
	}
}

func TestCodexPatchApplyUnderBudgetIsNotMarkedTruncated(t *testing.T) {
	t.Parallel()

	input := codexPatchInput([]any{
		map[string]any{"path": "a.go", "kind": "update", "diff": "@@ -1 +1 @@\n-a\n+b\n"},
	})
	if _, marked := input["truncated"]; marked {
		t.Fatalf("small payload must not be marked truncated: %#v", input)
	}
	if _, marked := input["original_bytes"]; marked {
		t.Fatalf("small payload must not carry original_bytes: %#v", input)
	}
}

// codexTestBodies returns the diff/content bodies carried by a tool_use input.
//
// Read the *returned* payload, never the slice handed to codexPatchInput:
// redaction copies before the budget trims, so the caller's originals stay
// untouched and asserting on them would pass vacuously.
func codexTestBodies(t *testing.T, input map[string]any) []string {
	t.Helper()
	raw, ok := input["changes"].([]any)
	if !ok {
		t.Fatalf("expected changes slice, got %#v", input["changes"])
	}
	var bodies []string
	for _, change := range raw {
		entry, ok := change.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"diff", "content"} {
			if body, ok := entry[key].(string); ok {
				bodies = append(bodies, body)
			}
		}
	}
	return bodies
}

// Redaction has to happen before the size budget, not after it. Several rules
// only match a credential as a whole: the PEM rule needs both the BEGIN and the
// END marker. If a key straddles the budget, truncating first strands the
// opening half — marker plus key material — in text that no later pass can
// recognise, so both the daemon's and the server's redaction wave it through
// and it lands in the database and the WebSocket broadcast.
func TestCodexPatchApplyRedactsBeforeTruncating(t *testing.T) {
	t.Parallel()

	const keyChunk = "MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKg"
	// BEGIN lands inside the budget; END lands well past it.
	pem := "-----BEGIN PRIVATE KEY-----\n" +
		strings.Repeat(keyChunk, 2000) +
		"\n-----END PRIVATE KEY-----\n"
	if len(pem) <= codexPatchInputMaxBytes {
		t.Fatalf("fixture must exceed the budget to exercise the boundary: %d", len(pem))
	}

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	var messages []Message
	c.onMessage = func(msg Message) { messages = append(messages, msg) }

	event, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "item/started",
		"params": map[string]any{
			"item": map[string]any{
				"type": "fileChange", "id": "patch-1", "status": "inProgress",
				"changes": []any{
					map[string]any{
						"path": "id_rsa",
						"kind": map[string]any{"type": "add"},
						"diff": pem,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	c.handleLine(string(event))

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	bodies := codexTestBodies(t, messages[0].Input)
	if len(bodies) == 0 {
		t.Fatal("expected a recorded body")
	}

	total := 0
	for _, body := range bodies {
		total += len(body)
		if strings.Contains(body, "BEGIN PRIVATE KEY") {
			t.Errorf("PEM opening marker survived: %q", body[:min(len(body), 120)])
		}
		if strings.Contains(body, keyChunk) {
			t.Error("private key material survived redaction")
		}
		if !strings.Contains(body, "[REDACTED PRIVATE KEY]") {
			t.Errorf("expected the redaction placeholder, got %q", body[:min(len(body), 120)])
		}
	}
	if total > codexPatchInputMaxBytes {
		t.Fatalf("payload %d bytes exceeds budget %d", total, codexPatchInputMaxBytes)
	}
}

// The daemon and the server redact again after the adapter. Those passes must
// not corrupt an already-redacted payload, or the placeholder itself would be
// rewritten on the way to the database.
func TestCodexPatchApplyRedactionIsIdempotent(t *testing.T) {
	t.Parallel()

	input := codexPatchInput([]any{
		map[string]any{
			"path":    ".env",
			"kind":    "delete",
			"content": "GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn\n",
		},
	})

	first, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(first), "ghp_ABCDEFGH") {
		t.Fatalf("token survived the adapter: %s", first)
	}

	second, err := json.Marshal(redact.InputMap(input))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("a second redaction pass changed the payload:\n first=%s\nsecond=%s", first, second)
	}
}

// Redaction running first must not disable the budget for ordinary large
// patches: a big generated file carries no secret to shrink.
func TestCodexPatchApplyStillTruncatesNonSecretPayload(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("x", codexPatchInputMaxBytes+5000)
	changes := []any{map[string]any{"path": "big.txt", "kind": "add", "content": body}}
	input := codexPatchInput(changes)

	if input["truncated"] != true {
		t.Fatalf("expected truncation for a %d-byte body: %#v", len(body), input["truncated"])
	}
	if got := input["original_bytes"]; got != len(body) {
		t.Fatalf("original_bytes should report the pre-redaction size: got %v want %d", got, len(body))
	}
	total := 0
	for _, kept := range codexTestBodies(t, input) {
		total += len(kept)
	}
	if total > codexPatchInputMaxBytes {
		t.Fatalf("kept %d bytes, budget is %d", total, codexPatchInputMaxBytes)
	}
	// The caller's slice is deliberately left alone: redaction copies first.
	if original, _ := changes[0].(map[string]any)["content"].(string); len(original) != len(body) {
		t.Fatalf("codexPatchInput must not mutate its argument: %d != %d", len(original), len(body))
	}
}

func TestCodexResumeOverflowError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		errText string
		want    bool
	}{
		{
			// The exact text executeOnce puts in Result.Error for this failure.
			name:    "resume overflow",
			errText: "codex thread/resume failed: codex process exited: bufio.Scanner: token too long",
			want:    true,
		},
		{
			// Both markers required: an overflow on another RPC leaves the
			// stored thread perfectly resumable, so retiring it cures nothing.
			name:    "overflow on thread/start",
			errText: "codex thread/start failed: codex process exited: bufio.Scanner: token too long",
			want:    false,
		},
		{
			// An ordinary resume rejection already has a recovery path.
			name:    "resume failure without overflow",
			errText: "codex thread/resume failed: thread not found",
			want:    false,
		},
		{
			name:    "unrelated process failure",
			errText: "codex process exited: exit status 2",
			want:    false,
		},
		{name: "empty", errText: "", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CodexResumeOverflowError(tc.errText); got != tc.want {
				t.Fatalf("CodexResumeOverflowError(%q) = %v, want %v", tc.errText, got, tc.want)
			}
		})
	}
}

// TestCodexResumeOverflowErrorMatchesLiveFailureText guards the seam between
// the two halves of MUL-5722's layer 3: in-process the backend detects the
// overflow from the typed bufio.ErrTooLong, but the daemon classifies it at
// report time from the error STRING alone. If the wording produced by
// startOrResumeThread ever drifts from what the predicate matches, the resume
// pointer silently stops being retired and the permanent-stall bug returns
// with every test above still green.
func TestCodexResumeOverflowErrorMatchesLiveFailureText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	codexGracefulShutdownTimeoutNanos.Store(int64(500 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })

	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`printf '{"jsonrpc":"2.0","id":2,"result":{"big":"'`+"\n"+
		fmt.Sprintf(`head -c %d /dev/zero | tr '\0' 'x'`, agentStreamMaxLineBytes+1024*1024)+"\n"+
		`printf '"}}\n'`+"\n"+
		`sleep 30`+"\n")

	result := executeFakeCodex(t, fakePath, ExecOptions{
		Cwd:                       t.TempDir(),
		ResumeSessionID:           "thr_prior",
		Timeout:                   30 * time.Second,
		SemanticInactivityTimeout: 5 * time.Second,
	})

	if !CodexResumeOverflowError(result.Error) {
		t.Fatalf("predicate missed the error the backend actually produced: %q", result.Error)
	}
}
