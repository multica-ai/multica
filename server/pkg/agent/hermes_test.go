package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
)

func newTestHermesClient(t *testing.T) (*hermesClient, *fakeStdin, *[]Message) {
	t.Helper()
	fs := &fakeStdin{}
	var mu sync.Mutex
	messages := &[]Message{}

	c := &hermesClient{
		cfg:     Config{Logger: slog.Default()},
		stdin:   fs,
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			mu.Lock()
			*messages = append(*messages, msg)
			mu.Unlock()
		},
	}
	return c, fs, messages
}

func TestNewReturnsHermesBackend(t *testing.T) {
	t.Parallel()
	b, err := New("hermes", Config{ExecutablePath: "/nonexistent/hermes"})
	if err != nil {
		t.Fatalf("New(hermes) error: %v", err)
	}
	if _, ok := b.(*hermesBackend); !ok {
		t.Fatalf("expected *hermesBackend, got %T", b)
	}
}

func TestHermesHandleResponseSuccess(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestHermesClient(t)

	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "test"}
	c.mu.Lock()
	c.pending[1] = pr
	c.mu.Unlock()

	c.handleLine(`{"jsonrpc":"2.0","id":1,"result":{"sessionId":"ses_123"}}`)

	res := <-pr.ch
	if res.err != nil {
		t.Fatalf("expected no error, got %v", res.err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(res.result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed["sessionId"] != "ses_123" {
		t.Fatalf("expected sessionId=ses_123, got %v", parsed["sessionId"])
	}
}

func TestHermesHandleResponseError(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestHermesClient(t)

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

func TestHermesHandleServerRequestPermissionAutoApproves(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestHermesClient(t)

	c.handleLine(`{"jsonrpc":"2.0","id":10,"method":"session/request_permission","params":{"options":[{"id":"allow_once","kind":"allow_once"}]}}`)

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
	if result["outcome"] != "allow_once" {
		t.Fatalf("expected outcome=allow_once, got %v", result["outcome"])
	}
}

func TestHermesHandleServerRequestUnknown(t *testing.T) {
	t.Parallel()

	c, fs, _ := newTestHermesClient(t)

	c.handleLine(`{"jsonrpc":"2.0","id":11,"method":"unknown_method","params":{}}`)

	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	result := resp["result"].(map[string]any)
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %v", result)
	}
}

func TestHermesSessionUpdateContentText(t *testing.T) {
	t.Parallel()

	c, _, messages := newTestHermesClient(t)

	// ACP spec: agent_message_chunk with content object.
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hello from hermes"}}}}`)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	if (*messages)[0].Type != MessageText {
		t.Errorf("type: got %v, want MessageText", (*messages)[0].Type)
	}
	if (*messages)[0].Content != "Hello from hermes" {
		t.Errorf("content: got %q, want %q", (*messages)[0].Content, "Hello from hermes")
	}
}

func TestHermesSessionUpdateContentThinking(t *testing.T) {
	t.Parallel()

	c, _, messages := newTestHermesClient(t)

	// ACP spec: thinking content block.
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"thinking","text":"Let me think..."}}}}`)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	if (*messages)[0].Type != MessageThinking {
		t.Errorf("type: got %v, want MessageThinking", (*messages)[0].Type)
	}
	if (*messages)[0].Content != "Let me think..." {
		t.Errorf("content: got %q", (*messages)[0].Content)
	}
}

func TestHermesSessionUpdateToolCallStarted(t *testing.T) {
	t.Parallel()

	c, _, messages := newTestHermesClient(t)

	// ACP spec: tool_call with toolCallId, title, status pending.
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"call_1","title":"terminal","kind":"other","status":"pending"}}}`)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	if (*messages)[0].Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", (*messages)[0].Type)
	}
	if (*messages)[0].Tool != "terminal" {
		t.Errorf("tool: got %q, want %q", (*messages)[0].Tool, "terminal")
	}
	if (*messages)[0].CallID != "call_1" {
		t.Errorf("callID: got %q, want %q", (*messages)[0].CallID, "call_1")
	}
}

func TestHermesSessionUpdateToolCallCompleted(t *testing.T) {
	t.Parallel()

	c, _, messages := newTestHermesClient(t)

	// ACP spec: tool_call_update with status completed and content array.
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"call_1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"file1.go\nfile2.go\n"}}]}}}`)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	if (*messages)[0].Type != MessageToolResult {
		t.Errorf("type: got %v, want MessageToolResult", (*messages)[0].Type)
	}
	if (*messages)[0].CallID != "call_1" {
		t.Errorf("callID: got %q, want %q", (*messages)[0].CallID, "call_1")
	}
	if (*messages)[0].Output != "file1.go\nfile2.go\n" {
		t.Errorf("output: got %q", (*messages)[0].Output)
	}
}

func TestHermesSessionUpdateToolCallFailed(t *testing.T) {
	t.Parallel()

	c, _, messages := newTestHermesClient(t)

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"call_2","title":"read_file","status":"failed","content":[{"type":"content","content":{"type":"text","text":"file not found"}}]}}}`)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	if (*messages)[0].Type != MessageToolResult {
		t.Errorf("type: got %v, want MessageToolResult", (*messages)[0].Type)
	}
}

func TestHermesSessionUpdateToolCallNoStatus(t *testing.T) {
	t.Parallel()

	c, _, messages := newTestHermesClient(t)

	// tool_call without explicit status should be treated as started.
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"call_3","title":"write_file","kind":"other"}}}`)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	if (*messages)[0].Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", (*messages)[0].Type)
	}
}

func TestHermesSessionUpdateWithUsage(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestHermesClient(t)

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"call_4","title":"terminal","status":"completed","content":[],"_meta":{"usage":{"inputTokens":100,"outputTokens":50,"cacheReadTokens":10,"cacheWriteTokens":5}}}}}`)

	c.usageMu.Lock()
	u := c.usage
	c.usageMu.Unlock()

	if u.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", u.InputTokens)
	}
	if u.OutputTokens != 50 {
		t.Errorf("OutputTokens: got %d, want 50", u.OutputTokens)
	}
	if u.CacheReadTokens != 10 {
		t.Errorf("CacheReadTokens: got %d, want 10", u.CacheReadTokens)
	}
	if u.CacheWriteTokens != 5 {
		t.Errorf("CacheWriteTokens: got %d, want 5", u.CacheWriteTokens)
	}
}

func TestHermesSessionUpdateSingularUpdate(t *testing.T) {
	t.Parallel()

	c, _, messages := newTestHermesClient(t)

	// Test the singular "update" field path (which is the only ACP spec path).
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"singular update"}}}}`)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	if (*messages)[0].Content != "singular update" {
		t.Errorf("content: got %q, want %q", (*messages)[0].Content, "singular update")
	}
}

func TestHermesSessionUpdateNilParams(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestHermesClient(t)

	// Should not panic.
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update"}`)
}

func TestHermesHandleInvalidJSON(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestHermesClient(t)
	// Should not panic.
	c.handleLine("not json at all")
	c.handleLine("")
	c.handleLine("{}")
}

func TestHermesCloseAllPending(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestHermesClient(t)

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

func TestExtractHermesSessionID(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"sessionId":"ses_hermes_123"}`)
	got := extractHermesSessionID(data)
	if got != "ses_hermes_123" {
		t.Fatalf("expected ses_hermes_123, got %q", got)
	}
}

func TestExtractHermesSessionIDMissing(t *testing.T) {
	t.Parallel()

	got := extractHermesSessionID(json.RawMessage(`{}`))
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractHermesStopReason(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"stopReason":"end_turn"}`)
	got := extractHermesStopReason(data)
	if got != "end_turn" {
		t.Fatalf("expected end_turn, got %q", got)
	}
}

func TestExtractHermesStopReasonCancelled(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"stopReason":"cancelled"}`)
	got := extractHermesStopReason(data)
	if got != "cancelled" {
		t.Fatalf("expected cancelled, got %q", got)
	}
}

func TestExtractACPContentOutput(t *testing.T) {
	t.Parallel()

	// ACP content array with nested content objects.
	input := []any{
		map[string]any{
			"type": "content",
			"content": map[string]any{
				"type": "text",
				"text": "result line 1",
			},
		},
		map[string]any{
			"type": "content",
			"content": map[string]any{
				"type": "text",
				"text": "result line 2",
			},
		},
	}

	got := extractACPContentOutput(input)
	want := "result line 1\nresult line 2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractACPContentOutputString(t *testing.T) {
	t.Parallel()

	// Fallback: plain string.
	got := extractACPContentOutput("plain text")
	if got != "plain text" {
		t.Errorf("got %q, want %q", got, "plain text")
	}
}

func TestHermesInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data map[string]any
		keys []string
		want int64
	}{
		{
			name: "float64 value",
			data: map[string]any{"inputTokens": float64(100)},
			keys: []string{"inputTokens"},
			want: 100,
		},
		{
			name: "int64 value",
			data: map[string]any{"inputTokens": int64(200)},
			keys: []string{"inputTokens"},
			want: 200,
		},
		{
			name: "zero float64 skipped",
			data: map[string]any{"inputTokens": float64(0)},
			keys: []string{"inputTokens", "input_tokens"},
			want: 0,
		},
		{
			name: "fallback key",
			data: map[string]any{"input_tokens": float64(300)},
			keys: []string{"inputTokens", "input_tokens"},
			want: 300,
		},
		{
			name: "missing key",
			data: map[string]any{},
			keys: []string{"inputTokens"},
			want: 0,
		},
		{
			name: "wrong type",
			data: map[string]any{"inputTokens": "not a number"},
			keys: []string{"inputTokens"},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hermesInt64(tt.data, tt.keys...); got != tt.want {
				t.Errorf("hermesInt64() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ── Full integration test with simulated ACP event stream ──

func TestHermesSessionUpdateHappyPath(t *testing.T) {
	t.Parallel()

	c, _, messages := newTestHermesClient(t)

	// Simulate: text -> tool call -> tool result -> text
	updates := []string{
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Analyzing the issue..."}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"call_1","title":"terminal","kind":"other","status":"pending"}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"call_1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"file.go\n"}}]}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":" Done."}}}}`,
	}

	for _, line := range updates {
		c.handleLine(line)
	}

	msgs := *messages
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Type != MessageText || msgs[0].Content != "Analyzing the issue..." {
		t.Errorf("msg[0]: got %+v", msgs[0])
	}
	if msgs[1].Type != MessageToolUse || msgs[1].Tool != "terminal" {
		t.Errorf("msg[1]: got %+v, want tool-use(terminal)", msgs[1])
	}
	if msgs[2].Type != MessageToolResult || msgs[2].Output != "file.go\n" {
		t.Errorf("msg[2]: got %+v, want tool-result", msgs[2])
	}
	if msgs[3].Type != MessageText || msgs[3].Content != " Done." {
		t.Errorf("msg[3]: got %+v", msgs[3])
	}
}

func TestHermesSessionUpdateThinkingAndText(t *testing.T) {
	t.Parallel()

	c, _, messages := newTestHermesClient(t)

	// Simulate thinking followed by text output.
	updates := []string{
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"thinking","text":"I need to check the files..."}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"I found the issue."}}}}`,
	}

	for _, line := range updates {
		c.handleLine(line)
	}

	msgs := *messages
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Type != MessageThinking {
		t.Errorf("msg[0]: got type %v, want MessageThinking", msgs[0].Type)
	}
	if msgs[1].Type != MessageText {
		t.Errorf("msg[1]: got type %v, want MessageText", msgs[1].Type)
	}
}
