package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestCodexClient(t *testing.T) (*codexClient, *fakeStdin, []Message) {
	t.Helper()
	fs := &fakeStdin{}
	var mu sync.Mutex
	var messages []Message

	c := &codexClient{
		cfg:     Config{Logger: slog.Default()},
		stdin:   fs,
		pending: make(map[int]*pendingRPC),
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

	c.handleLine(`{"jsonrpc":"2.0","method":"error","params":{"error":{"message":"boom"},"willRetry":false}}`)

	if got := c.getTurnError(); got != "boom" {
		t.Fatalf("expected terminal error captured, got %q", got)
	}
}

func TestCodexRawErrorNotificationRetryingIgnored(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"

	c.handleLine(`{"jsonrpc":"2.0","method":"error","params":{"error":{"message":"reconnecting"},"willRetry":true}}`)

	if got := c.getTurnError(); got != "" {
		t.Fatalf("retrying error should not be captured, got %q", got)
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
