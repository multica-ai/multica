package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
)

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

// ── extractACPSessionID ──

func TestExtractACPSessionID(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"sessionId":"20260410_141145_47260c"}`)
	got := extractACPSessionID(raw)
	if got != "20260410_141145_47260c" {
		t.Errorf("got %q, want %q", got, "20260410_141145_47260c")
	}
}

func TestExtractACPSessionIDEmpty(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{}`)
	got := extractACPSessionID(raw)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractACPSessionIDInvalidJSON(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`not json`)
	got := extractACPSessionID(raw)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ── buildHermesSessionParams ──

func TestBuildHermesSessionParamsIncludesModel(t *testing.T) {
	t.Parallel()
	params := buildHermesSessionParams("/tmp/work", "gpt-4o")
	if params["cwd"] != "/tmp/work" {
		t.Errorf("cwd: got %v, want /tmp/work", params["cwd"])
	}
	if _, ok := params["mcpServers"]; !ok {
		t.Error("mcpServers missing")
	}
	if got, ok := params["model"].(string); !ok || got != "gpt-4o" {
		t.Errorf("model: got %v, want gpt-4o", params["model"])
	}
}

func TestBuildHermesSessionParamsOmitsEmptyModel(t *testing.T) {
	t.Parallel()
	params := buildHermesSessionParams("/tmp/work", "")
	if _, present := params["model"]; present {
		t.Error("expected model key to be omitted when model is empty")
	}
}

// ── hermesToolNameFromTitle ──

func TestHermesToolNameFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		kind  string
		want  string
	}{
		{"terminal: ls -la", "execute", "terminal"},
		{"read: /tmp/foo.go", "read", "read_file"},
		{"write: /tmp/bar.go", "edit", "write_file"},
		{"patch (replace): /tmp/baz.go", "edit", "patch"},
		{"search: *.go", "search", "search_files"},
		{"web search: golang acp protocol", "fetch", "web_search"},
		{"extract: https://example.com", "fetch", "web_extract"},
		{"delegate: fix the bug", "execute", "delegate_task"},
		{"analyze image: what is this?", "read", "vision_analyze"},
		{"execute code", "execute", "execute_code"},
		// Fallback to kind when no colon in title but kind is known.
		{"unknownTool", "read", "read_file"},
		{"unknownTool", "edit", "write_file"},
		{"unknownTool", "execute", "terminal"},
		{"unknownTool", "search", "search_files"},
		{"unknownTool", "fetch", "web_search"},
		{"unknownTool", "think", "thinking"},
		// Bare title (no colon, no known kind) — preserve the title
		// itself rather than falling back to an unclassified kind.
		// Matters for kimi: its ACP `tool_call` updates emit a bare
		// `title: "Shell"` with no `kind`, and we need downstream
		// normalisation (kimiToolNameFromTitle) to see "Shell" rather
		// than an empty string.
		{"Shell", "", "Shell"},
		{"Read file", "", "Read file"},
		{"unknownTool", "other", "unknownTool"},
		// Empty title falls back to kind, even when kind isn't known.
		{"", "other", "other"},
		// Tool with colon but not in known map.
		{"custom_tool: args", "other", "custom_tool"},
	}
	for _, tt := range tests {
		got := hermesToolNameFromTitle(tt.title, tt.kind)
		if got != tt.want {
			t.Errorf("hermesToolNameFromTitle(%q, %q) = %q, want %q", tt.title, tt.kind, got, tt.want)
		}
	}
}

// ── handleLine routing ──

func TestHermesClientHandleLineResponse(t *testing.T) {
	t.Parallel()

	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "session/new"}
	c.pending[1] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":1,"result":{"sessionId":"ses_abc"}}`)

	res := <-pr.ch
	if res.err != nil {
		t.Fatalf("unexpected error: %v", res.err)
	}
	sid := extractACPSessionID(res.result)
	if sid != "ses_abc" {
		t.Errorf("sessionId: got %q, want %q", sid, "ses_abc")
	}
}

func TestHermesClientHandleLineError(t *testing.T) {
	t.Parallel()

	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "initialize"}
	c.pending[0] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":0,"error":{"code":-32600,"message":"bad request"}}`)

	res := <-pr.ch
	if res.err == nil {
		t.Fatal("expected error")
	}
	if got := res.err.Error(); got != "initialize: bad request (code=-32600)" {
		t.Errorf("error: got %q", got)
	}
}

// ── agent → client request handling ──

// bufferWriter is a test stand-in for cmd.StdinPipe that captures
// writes in-memory so we can assert what handleAgentRequest emitted.
type bufferWriter struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *bufferWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.WriteString(string(p))
}

func (b *bufferWriter) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestHermesClientAutoApprovesPermissionRequest asserts that when an
// ACP agent sends us `session/request_permission` (kimi does this on
// every Shell / file-mutating tool call), the client replies with
// `approve_for_session` — without this the agent blocks 300s and the
// task hangs. The id in the reply must match the agent's request id
// so its in-flight future resolves.
func TestHermesClientAutoApprovesPermissionRequest(t *testing.T) {
	t.Parallel()

	w := &bufferWriter{}
	c := &hermesClient{
		cfg:     Config{Logger: slog.Default()},
		stdin:   w,
		pending: make(map[int]*pendingRPC),
	}

	c.handleLine(`{"jsonrpc":"2.0","id":42,"method":"session/request_permission","params":{"sessionId":"ses_1","options":[{"optionId":"approve","name":"Approve once","kind":"allow_once"},{"optionId":"approve_for_session","name":"Approve for this session","kind":"allow_always"},{"optionId":"reject","name":"Reject","kind":"reject_once"}],"toolCall":{"toolCallId":"tc_1","title":"Shell","content":[]}}}`)

	got := w.String()
	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Outcome struct {
				Outcome  string `json:"outcome"`
				OptionID string `json:"optionId"`
			} `json:"outcome"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &resp); err != nil {
		t.Fatalf("reply is not valid JSON: %q err=%v", got, err)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc: got %q, want 2.0", resp.JSONRPC)
	}
	if resp.ID != 42 {
		t.Errorf("id: got %d, want 42 (must echo agent's request id)", resp.ID)
	}
	if resp.Result.Outcome.Outcome != "selected" {
		t.Errorf("outcome.outcome: got %q, want %q", resp.Result.Outcome.Outcome, "selected")
	}
	if resp.Result.Outcome.OptionID != "approve_for_session" {
		t.Errorf("outcome.optionId: got %q, want %q", resp.Result.Outcome.OptionID, "approve_for_session")
	}
}

// TestHermesClientReplesMethodNotFoundForUnknownAgentRequest ensures
// that any agent → client request we don't explicitly handle gets a
// proper JSON-RPC error back, not silence. Silence would block the
// agent for however long its internal timeout is, same as the
// session/request_permission hang this change fixes.
func TestHermesClientReplesMethodNotFoundForUnknownAgentRequest(t *testing.T) {
	t.Parallel()

	w := &bufferWriter{}
	c := &hermesClient{
		cfg:     Config{Logger: slog.Default()},
		stdin:   w,
		pending: make(map[int]*pendingRPC),
	}
	c.handleLine(`{"jsonrpc":"2.0","id":7,"method":"fs/read_text_file","params":{"path":"/tmp/x"}}`)

	got := w.String()
	var resp struct {
		ID    int `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &resp); err != nil {
		t.Fatalf("reply not valid JSON: %q err=%v", got, err)
	}
	if resp.ID != 7 {
		t.Errorf("id echo: got %d, want 7", resp.ID)
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code: got %d, want -32601 (method not found)", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "fs/read_text_file") {
		t.Errorf("error message should name the unhandled method, got %q", resp.Error.Message)
	}
}

// ── session/update notification handling ──

func TestHermesClientHandleAgentMessage(t *testing.T) {
	t.Parallel()

	var got Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = msg
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hello world"}}}}`
	c.handleLine(line)

	if got.Type != MessageText {
		t.Errorf("type: got %v, want MessageText", got.Type)
	}
	if got.Content != "Hello world" {
		t.Errorf("content: got %q, want %q", got.Content, "Hello world")
	}
}

func TestHermesClientHandleSessionNotificationAgentMessage(t *testing.T) {
	t.Parallel()

	var got Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = msg
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"Hello from Kiro"}}}}`
	c.handleLine(line)

	if got.Type != MessageText {
		t.Errorf("type: got %v, want MessageText", got.Type)
	}
	if got.Content != "Hello from Kiro" {
		t.Errorf("content: got %q, want %q", got.Content, "Hello from Kiro")
	}
}

func TestHermesClientHandleAgentThought(t *testing.T) {
	t.Parallel()

	var got Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = msg
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Let me think..."}}}}`
	c.handleLine(line)

	if got.Type != MessageThinking {
		t.Errorf("type: got %v, want MessageThinking", got.Type)
	}
	if got.Content != "Let me think..." {
		t.Errorf("content: got %q, want %q", got.Content, "Let me think...")
	}
}

func TestHermesClientHandleToolCallStart(t *testing.T) {
	t.Parallel()

	var got Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = msg
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call","toolCallId":"tc-abc123","title":"terminal: ls -la","kind":"execute","status":"pending","rawInput":{"command":"ls -la"}}}}`
	c.handleLine(line)

	if got.Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", got.Type)
	}
	if got.Tool != "terminal" {
		t.Errorf("tool: got %q, want %q", got.Tool, "terminal")
	}
	if got.CallID != "tc-abc123" {
		t.Errorf("callID: got %q, want %q", got.CallID, "tc-abc123")
	}
	if cmd, ok := got.Input["command"].(string); !ok || cmd != "ls -la" {
		t.Errorf("input.command: got %v", got.Input["command"])
	}
}

func TestHermesClientHandleSessionNotificationToolCall(t *testing.T) {
	t.Parallel()

	var got []Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = append(got, msg)
		},
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"ToolCall","toolCallId":"tc-kiro","name":"Shell","status":"pending","parameters":{"command":"pwd"}}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"ToolCallUpdate","toolCallId":"tc-kiro","status":"completed","name":"Shell","output":"/tmp/project\n"}}}`)

	if len(got) != 2 {
		t.Fatalf("expected [ToolUse, ToolResult], got %+v", got)
	}
	if got[0].Type != MessageToolUse {
		t.Errorf("first message: got %v, want MessageToolUse", got[0].Type)
	}
	if got[0].Tool != "Shell" {
		t.Errorf("first tool: got %q, want Shell", got[0].Tool)
	}
	if cmd, _ := got[0].Input["command"].(string); cmd != "pwd" {
		t.Errorf("first input.command: got %v, want pwd", got[0].Input["command"])
	}
	if got[1].Type != MessageToolResult {
		t.Errorf("second message: got %v, want MessageToolResult", got[1].Type)
	}
	if got[1].Output != "/tmp/project\n" {
		t.Errorf("second output: got %q", got[1].Output)
	}
}

func TestHermesClientHandleSessionNotificationTurnEnd(t *testing.T) {
	t.Parallel()

	var got hermesPromptResult
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result hermesPromptResult) {
			got = result
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"TurnEnd","stopReason":"end_turn","usage":{"inputTokens":3,"outputTokens":4,"cachedReadTokens":1}}}}`
	c.handleLine(line)

	if got.stopReason != "end_turn" {
		t.Errorf("stopReason: got %q, want end_turn", got.stopReason)
	}
	if got.usage.InputTokens != 3 || got.usage.OutputTokens != 4 || got.usage.CacheReadTokens != 1 {
		t.Errorf("usage: got %+v", got.usage)
	}
}

func TestHermesClientHandleToolCallComplete(t *testing.T) {
	t.Parallel()

	var got Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = msg
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-abc123","status":"completed","kind":"execute","rawOutput":"file1.go\nfile2.go\n"}}}`
	c.handleLine(line)

	if got.Type != MessageToolResult {
		t.Errorf("type: got %v, want MessageToolResult", got.Type)
	}
	if got.CallID != "tc-abc123" {
		t.Errorf("callID: got %q, want %q", got.CallID, "tc-abc123")
	}
	if got.Output != "file1.go\nfile2.go\n" {
		t.Errorf("output: got %q", got.Output)
	}
}

// TestHermesClientKimiStreamingToolCall walks the real kimi frame
// sequence for a single Shell call:
//  1. tool_call with empty content (LLM hasn't started emitting args yet)
//  2. tool_call_update status=in_progress carrying the cumulative args
//     JSON character-by-character ("{", "{\"command", …)
//  3. tool_call_update status=completed carrying the command's stdout
//
// The client must defer MessageToolUse until we have the full args so
// the UI doesn't show a command like `{"comma` — and the MessageToolUse
// must carry the parsed args as the Input map (`{"command": "echo hi"}`
// → Input["command"] = "echo hi") rather than a raw string.
func TestHermesClientKimiStreamingToolCall(t *testing.T) {
	t.Parallel()

	var got []Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = append(got, msg)
		},
	}

	// 1. tool_call: empty content (classic kimi start frame).
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call","toolCallId":"tc-kimi-1","title":"Shell","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":""}}]}}}`)
	if len(got) != 0 {
		t.Fatalf("expected nothing emitted yet (args empty), got %+v", got)
	}

	// 2. Streaming updates — cumulative args JSON.
	partials := []string{
		`{"`,
		`{"command`,
		`{"command":`,
		`{"command":"echo `,
		`{"command":"echo hi"}`,
	}
	for _, args := range partials {
		// JSON-encode args so embedded quotes are escaped properly.
		argsJSON, _ := json.Marshal(args)
		line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-kimi-1","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":` + string(argsJSON) + `}}]}}}`
		c.handleLine(line)
	}
	if len(got) != 0 {
		t.Fatalf("expected nothing emitted mid-stream, got %+v", got)
	}

	// 3. Completed — stdout.
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-kimi-1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"hi\n"}}]}}}`)

	if len(got) != 2 {
		t.Fatalf("expected [MessageToolUse, MessageToolResult], got %d: %+v", len(got), got)
	}
	if got[0].Type != MessageToolUse {
		t.Errorf("first message: got %v, want MessageToolUse", got[0].Type)
	}
	if got[0].CallID != "tc-kimi-1" {
		t.Errorf("first.callID: got %q", got[0].CallID)
	}
	if cmd, _ := got[0].Input["command"].(string); cmd != "echo hi" {
		t.Errorf("first.Input.command: got %v, want %q", got[0].Input["command"], "echo hi")
	}
	if got[1].Type != MessageToolResult {
		t.Errorf("second message: got %v, want MessageToolResult", got[1].Type)
	}
	if got[1].Output != "hi\n" {
		t.Errorf("second.output: got %q, want %q", got[1].Output, "hi\n")
	}
}

// TestHermesClientKimiMalformedArgsFallback: if the accumulated args
// aren't valid JSON (streaming glitch, tool with non-JSON args), we
// still surface the text under Input.text rather than silently
// dropping it.
func TestHermesClientKimiMalformedArgsFallback(t *testing.T) {
	t.Parallel()

	var got []Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = append(got, msg)
		},
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call","toolCallId":"tc","title":"Shell","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":"not-json"}}]}}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc","status":"completed","content":[{"type":"content","content":{"type":"text","text":"output"}}]}}}`)

	if len(got) < 1 {
		t.Fatalf("expected ToolUse+ToolResult, got %+v", got)
	}
	if text, _ := got[0].Input["text"].(string); text != "not-json" {
		t.Errorf("fallback Input.text: got %v", got[0].Input["text"])
	}
}

// TestHermesClientHandleToolCallCompleteOrphan: if a completion frame
// arrives without a preceding tool_call (out-of-order / missed frame),
// still emit ToolUse synthesised from the update's own title/rawInput
// before ToolResult. Keeps the UI from showing a bare result with no
// header.
func TestHermesClientHandleToolCallCompleteOrphan(t *testing.T) {
	t.Parallel()

	var got []Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = append(got, msg)
		},
	}

	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc","status":"completed","title":"terminal: ls","kind":"execute","rawInput":{"command":"ls"},"content":[{"type":"content","content":{"type":"text","text":"file.go\n"}}]}}}`)

	if len(got) != 2 || got[0].Type != MessageToolUse || got[1].Type != MessageToolResult {
		t.Fatalf("expected [ToolUse, ToolResult], got %+v", got)
	}
	if got[0].Tool != "terminal" {
		t.Errorf("orphan ToolUse tool: got %q", got[0].Tool)
	}
	if cmd, _ := got[0].Input["command"].(string); cmd != "ls" {
		t.Errorf("orphan ToolUse input.command: got %v", got[0].Input["command"])
	}
	if got[1].Output != "file.go\n" {
		t.Errorf("ToolResult output: got %q", got[1].Output)
	}
}

// TestHermesClientHandleToolCallRawOutputTakesPrecedence keeps hermes
// behaviour unchanged: when the update has both `rawOutput` (hermes
// convention) and `content` (would be ambiguous), honour rawOutput.
func TestHermesClientHandleToolCallRawOutputTakesPrecedence(t *testing.T) {
	t.Parallel()

	var got Message
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			got = msg
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc","status":"completed","rawOutput":"raw wins","content":[{"type":"content","content":{"type":"text","text":"ignored"}}]}}}`
	c.handleLine(line)

	if got.Output != "raw wins" {
		t.Errorf("output: got %q, want %q", got.Output, "raw wins")
	}
}

func TestExtractACPToolCallText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "single text block",
			json: `[{"type":"content","content":{"type":"text","text":"hello"}}]`,
			want: "hello",
		},
		{
			name: "multiple text blocks join with newline",
			json: `[{"type":"content","content":{"type":"text","text":"a"}},{"type":"content","content":{"type":"text","text":"b"}}]`,
			want: "a\nb",
		},
		{
			name: "terminal blocks skipped",
			json: `[{"type":"terminal","terminalId":"t1"},{"type":"content","content":{"type":"text","text":"shell out"}}]`,
			want: "shell out",
		},
		{
			name: "diff block renders as mini header",
			json: `[{"type":"diff","path":"foo.go","oldText":"abc","newText":"abcdef"}]`,
			want: "--- foo.go\n+++ foo.go\n(edited: 3 → 6 bytes)",
		},
		{
			name: "new-file diff (no oldText)",
			json: `[{"type":"diff","path":"new.go","oldText":"","newText":"hi"}]`,
			want: "--- new.go\n+++ new.go\n(new file, 2 bytes)",
		},
		{
			name: "empty array returns empty",
			json: `[]`,
			want: "",
		},
		{
			name: "no text content",
			json: `[{"type":"terminal","terminalId":"t1"}]`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var blocks []json.RawMessage
			if err := json.Unmarshal([]byte(tt.json), &blocks); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := extractACPToolCallText(blocks); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHermesClientHandleToolCallInProgressIgnored(t *testing.T) {
	t.Parallel()

	called := false
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			called = true
		},
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-abc123","status":"in_progress"}}}`
	c.handleLine(line)

	if called {
		t.Error("expected in_progress tool_call_update to be ignored")
	}
}

func TestHermesClientHandleUsageUpdate(t *testing.T) {
	t.Parallel()

	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
	}

	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"usage_update","usage":{"inputTokens":500,"outputTokens":200,"cachedReadTokens":100}}}}`
	c.handleLine(line)

	c.usageMu.Lock()
	defer c.usageMu.Unlock()

	if c.usage.InputTokens != 500 {
		t.Errorf("inputTokens: got %d, want 500", c.usage.InputTokens)
	}
	if c.usage.OutputTokens != 200 {
		t.Errorf("outputTokens: got %d, want 200", c.usage.OutputTokens)
	}
	if c.usage.CacheReadTokens != 100 {
		t.Errorf("cacheReadTokens: got %d, want 100", c.usage.CacheReadTokens)
	}
}

func TestHermesClientHandleUsageUpdateCumulative(t *testing.T) {
	t.Parallel()

	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
	}

	// First usage update.
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"usage_update","usage":{"inputTokens":100,"outputTokens":50}}}}`)

	// Second usage update with higher values (should take the max).
	c.handleLine(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"usage_update","usage":{"inputTokens":300,"outputTokens":120}}}}`)

	c.usageMu.Lock()
	defer c.usageMu.Unlock()

	if c.usage.InputTokens != 300 {
		t.Errorf("inputTokens: got %d, want 300", c.usage.InputTokens)
	}
	if c.usage.OutputTokens != 120 {
		t.Errorf("outputTokens: got %d, want 120", c.usage.OutputTokens)
	}
}

// ── extractPromptResult ──

func TestHermesClientExtractPromptResult(t *testing.T) {
	t.Parallel()

	var got hermesPromptResult
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result hermesPromptResult) {
			got = result
		},
	}

	data := json.RawMessage(`{"stopReason":"end_turn","usage":{"inputTokens":1000,"outputTokens":200,"cachedReadTokens":50}}`)
	c.extractPromptResult(data)

	if got.stopReason != "end_turn" {
		t.Errorf("stopReason: got %q, want %q", got.stopReason, "end_turn")
	}
	if got.usage.InputTokens != 1000 {
		t.Errorf("inputTokens: got %d, want 1000", got.usage.InputTokens)
	}
	if got.usage.OutputTokens != 200 {
		t.Errorf("outputTokens: got %d, want 200", got.usage.OutputTokens)
	}
	if got.usage.CacheReadTokens != 50 {
		t.Errorf("cacheReadTokens: got %d, want 50", got.usage.CacheReadTokens)
	}
}

func TestHermesClientExtractPromptResultNoUsage(t *testing.T) {
	t.Parallel()

	var got hermesPromptResult
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result hermesPromptResult) {
			got = result
		},
	}

	data := json.RawMessage(`{"stopReason":"cancelled"}`)
	c.extractPromptResult(data)

	if got.stopReason != "cancelled" {
		t.Errorf("stopReason: got %q, want %q", got.stopReason, "cancelled")
	}
	if got.usage.InputTokens != 0 {
		t.Errorf("inputTokens: got %d, want 0", got.usage.InputTokens)
	}
}

func TestHermesClientIgnoresUnknownNotification(t *testing.T) {
	t.Parallel()

	called := false
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			called = true
		},
	}

	// Unknown method should be silently ignored.
	c.handleLine(`{"jsonrpc":"2.0","method":"unknown/event","params":{}}`)

	if called {
		t.Error("expected unknown notification to be ignored")
	}
}

func TestHermesClientIgnoresInvalidJSON(t *testing.T) {
	t.Parallel()

	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
	}

	// Should not panic.
	c.handleLine("not json at all")
	c.handleLine("")
	c.handleLine("{}")
}

func TestHermesProviderErrorSniffer(t *testing.T) {
	t.Parallel()

	// Real sample of the stderr hermes emits when the configured
	// LLM endpoint rejects the requested model. We verify the
	// sniffer extracts the `Error: ...` line so the task error
	// tells the user *why* it failed.
	s := newACPProviderErrorSniffer("hermes")
	lines := []string{
		"2026-04-20 23:41:47 [INFO] acp_adapter.server: Prompt on session abc",
		`⚠️  API call failed (attempt 1/3): BadRequestError [HTTP 400]`,
		`   🔌 Provider: openai-codex  Model: gpt-5.1-codex-mini`,
		`   📝 Error: HTTP 400: Error code: 400 - {'detail': "The 'gpt-5.1-codex-mini' model is not supported when using Codex with a ChatGPT account."}`,
		`⏱️  Elapsed: 1.17s`,
	}
	for _, line := range lines {
		if _, err := s.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	msg := s.message()
	if msg == "" {
		t.Fatal("expected a non-empty error message")
	}
	if !strings.Contains(msg, "model is not supported") {
		t.Errorf("expected detail about model support, got %q", msg)
	}
}

func TestHermesProviderErrorSnifferIgnoresInfoLines(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	s.Write([]byte("2026-04-20 23:41:45 [INFO] acp_adapter.entry: Loaded env\n"))
	s.Write([]byte("2026-04-20 23:41:47 [INFO] agent.auxiliary_client: Vision auto-detect...\n"))
	if msg := s.message(); msg != "" {
		t.Errorf("info lines should produce no error, got %q", msg)
	}
}

func TestHermesProviderErrorSnifferHandlesPartialLines(t *testing.T) {
	t.Parallel()

	// Writer may be called mid-line; the sniffer must buffer until
	// it sees a newline so the regex doesn't miss the header.
	s := newACPProviderErrorSniffer("hermes")
	s.Write([]byte(`⚠️  API call failed (attempt 1/3):`))
	s.Write([]byte(` BadRequestError [HTTP 400]` + "\n"))
	s.Write([]byte(`   📝 Error: something went wrong` + "\n"))
	msg := s.message()
	if !strings.Contains(msg, "something went wrong") {
		t.Errorf("expected buffered line to be captured, got %q", msg)
	}
}

func TestHermesProviderErrorSnifferBoundedBuffer(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	for i := 0; i < 20; i++ {
		// Each line differs so dedup doesn't merge them.
		s.Write([]byte(`⚠️  API call failed (HTTP 400) attempt ` + string(rune('a'+i%26)) + `: Non-retryable error` + "\n"))
	}
	if len(s.lines) > acpMaxErrorLines {
		t.Errorf("sniffer kept %d lines, limit is %d", len(s.lines), acpMaxErrorLines)
	}
}

// ── isACPStaleSessionError ──

func TestIsACPStaleSessionError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		// Matches — the observed Hermes pattern.
		{"hermes not found", "session/prompt: session 19bbcb1c-b28c-4106-b647-1af0a86a5198 not found (code=-32000)", true},
		{"simple not found", "session/prompt: session abc not found (code=-32601)", true},
		{"uppercase mixed", "session/prompt: Session ABC NOT FOUND (code=0)", true},
		// Non-matches — unrelated errors must not be treated as stale sessions.
		{"no session word", "session/prompt: resource not found (code=-32001)", false},
		{"no not found", "session/prompt: session abc expired (code=-32001)", false},
		{"nil error", "", false},
		{"provider error", "hermes provider error: HTTP 400 bad request", false},
		{"timeout", "hermes timed out after 20m0s", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.msg != "" {
				err = fmt.Errorf("%s", tt.msg)
			}
			got := isACPStaleSessionError(err)
			if got != tt.want {
				t.Errorf("isACPStaleSessionError(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

// ── Execute stale-session recovery (fake ACP server) ──
//
// The fake ACP server is embedded in the test binary itself: when the env var
// FAKE_HERMES_SCENARIO is set, the binary acts as a minimal hermes process
// rather than running tests. This is a standard Go test-helper-binary pattern
// that avoids needing a real hermes installation.

// TestMain is NOT defined here (it would conflict with other tests in the
// package). Instead we use TestFakeHermesHelper as the entrypoint for the
// subprocess, detected via an env var at the top of each Execute test.

func TestHermesExecuteResumeNullFallbackSuccess(t *testing.T) {
	t.Parallel()
	// Scenario: resume returns a JSON null success body (no error), which some
	// providers use to signal "cannot resume". Multica should fall back to a
	// new session and succeed.
	runFakeHermesExecuteTest(t, "resume_null_new_success", func(t *testing.T, result Result) {
		if result.Status != "completed" {
			t.Errorf("status: got %q, want completed", result.Status)
		}
		if result.Output == "" {
			t.Error("expected non-empty output after null-resume fallback")
		}
		if result.SessionID == "stale-session-id" {
			t.Error("result carries stale session ID; expected the new one")
		}
	})
}

func TestHermesExecuteResumeFailFallbackSuccess(t *testing.T) {
	t.Parallel()
	// Scenario: resume returns an error; Multica should fall back to a new
	// session and succeed.
	runFakeHermesExecuteTest(t, "resume_fail_new_success", func(t *testing.T, result Result) {
		if result.Status != "completed" {
			t.Errorf("status: got %q, want completed", result.Status)
		}
		if result.Output == "" {
			t.Error("expected non-empty output")
		}
		if result.SessionID == "" {
			t.Error("expected a session ID in result")
		}
		// The result session ID must NOT be the stale one we tried to resume.
		if result.SessionID == "stale-session-id" {
			t.Error("result carries stale session ID; expected the new one")
		}
	})
}

func TestHermesExecuteResumeSuccessPromptStaleRetrySuccess(t *testing.T) {
	t.Parallel()
	// Scenario: resume appears successful but prompt returns session-not-found;
	// Multica should create a new session and retry the prompt successfully.
	runFakeHermesExecuteTest(t, "resume_ok_prompt_stale_retry_success", func(t *testing.T, result Result) {
		if result.Status != "completed" {
			t.Errorf("status: got %q, want completed", result.Status)
		}
		if result.Output == "" {
			t.Error("expected non-empty output after retry")
		}
		if result.SessionID == "stale-session-id" {
			t.Error("result carries stale session ID after recovery")
		}
	})
}

func TestHermesExecuteResumeSuccessPromptStaleRetryFail(t *testing.T) {
	t.Parallel()
	// Scenario: resume appears successful, prompt returns session-not-found,
	// new session is created, but retry also fails. The real error must be
	// surfaced (not "empty output").
	runFakeHermesExecuteTest(t, "resume_ok_prompt_stale_retry_fail", func(t *testing.T, result Result) {
		if result.Status != "failed" {
			t.Errorf("status: got %q, want failed", result.Status)
		}
		if result.Error == "" {
			t.Error("expected a non-empty error on retry failure")
		}
		// Must not be the generic empty-output message.
		if strings.Contains(result.Error, "empty output") {
			t.Errorf("error should not be the generic empty-output message, got %q", result.Error)
		}
	})
}

// ── Kimi stale-session recovery ──
//
// Kimi uses an identical ACP session-recovery code path. These tests exercise
// the kimiBackend directly with the same fake ACP server.

func TestKimiExecuteResumeFailFallbackSuccess(t *testing.T) {
	t.Parallel()
	runFakeKimiExecuteTest(t, "resume_fail_new_success", func(t *testing.T, result Result) {
		if result.Status != "completed" {
			t.Errorf("status: got %q, want completed", result.Status)
		}
		if result.Output == "" {
			t.Error("expected non-empty output")
		}
		if result.SessionID == "stale-session-id" {
			t.Error("result carries stale session ID; expected the new one")
		}
	})
}

func TestKimiExecuteResumeNullFallbackSuccess(t *testing.T) {
	t.Parallel()
	runFakeKimiExecuteTest(t, "resume_null_new_success", func(t *testing.T, result Result) {
		if result.Status != "completed" {
			t.Errorf("status: got %q, want completed", result.Status)
		}
		if result.SessionID == "stale-session-id" {
			t.Error("result carries stale session ID after null-resume fallback")
		}
	})
}

func TestKimiExecuteResumeSuccessPromptStaleRetrySuccess(t *testing.T) {
	t.Parallel()
	runFakeKimiExecuteTest(t, "resume_ok_prompt_stale_retry_success", func(t *testing.T, result Result) {
		if result.Status != "completed" {
			t.Errorf("status: got %q, want completed", result.Status)
		}
		if result.SessionID == "stale-session-id" {
			t.Error("result carries stale session ID after recovery")
		}
	})
}

// runFakeHermesExecuteTest launches a subprocess running this test binary as a
// fake hermes ACP server (FAKE_HERMES_SCENARIO=<scenario>), then runs
// hermesBackend.Execute against it and calls check on the result.
func runFakeHermesExecuteTest(t *testing.T, scenario string, check func(*testing.T, Result)) {
	t.Helper()

	// Build the test binary path — os.Executable gives us the running binary.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	b := &hermesBackend{cfg: Config{
		ExecutablePath: exe,
		Logger:         slog.Default(),
	}}

	sess, err := b.Execute(context.Background(), "hello", ExecOptions{
		ResumeSessionID: "stale-session-id",
		// Tell the fake server which scenario to run.
		CustomArgs: []string{"--fake-hermes-scenario=" + scenario},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Drain messages (required to unblock the goroutine).
	for range sess.Messages {
	}
	result := <-sess.Result

	check(t, result)
}

// runFakeKimiExecuteTest is the kimi equivalent of runFakeHermesExecuteTest.
// Both backends speak the same ACP protocol so the same fake server handles both.
func runFakeKimiExecuteTest(t *testing.T, scenario string, check func(*testing.T, Result)) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	b := &kimiBackend{cfg: Config{
		ExecutablePath: exe,
		Logger:         slog.Default(),
	}}

	sess, err := b.Execute(context.Background(), "hello", ExecOptions{
		ResumeSessionID: "stale-session-id",
		CustomArgs:      []string{"--fake-hermes-scenario=" + scenario},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for range sess.Messages {
	}
	result := <-sess.Result

	check(t, result)
}

// TestFakeHermesHelper is the entrypoint for the subprocess. When run normally
// (no FAKE_HERMES_SCENARIO env var), it exits immediately. When run as a fake
// hermes binary, it plays back an ACP scenario on stdin/stdout.
func TestFakeHermesHelper(t *testing.T) {
	scenario := ""
	for _, arg := range os.Args {
		const prefix = "--fake-hermes-scenario="
		if strings.HasPrefix(arg, prefix) {
			scenario = strings.TrimPrefix(arg, prefix)
		}
	}
	if scenario == "" {
		t.Skip("not running as fake hermes helper")
	}

	// Run the fake ACP server on the real stdin/stdout of this subprocess.
	if err := runFakeACPServer(scenario); err != nil {
		fmt.Fprintf(os.Stderr, "fake hermes error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// runFakeACPServer speaks the ACP JSON-RPC 2.0 protocol for a given scenario.
// It reads requests from stdin and writes responses to stdout, then exits.
func runFakeACPServer(scenario string) error {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	readReq := func() (map[string]json.RawMessage, error) {
		if !scanner.Scan() {
			return nil, fmt.Errorf("stdin closed")
		}
		var req map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return nil, err
		}
		return req, nil
	}

	sendResult := func(id json.RawMessage, result any) error {
		return encoder.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  result,
		})
	}

	sendError := func(id json.RawMessage, code int, message string) error {
		return encoder.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": code, "message": message},
		})
	}

	sendNotification := func(method string, params any) error {
		return encoder.Encode(map[string]any{
			"jsonrpc": "2.0",
			"method":  method,
			"params":  params,
		})
	}

	// Step 1: initialize — always succeeds.
	req, err := readReq()
	if err != nil {
		return err
	}
	if err := sendResult(req["id"], map[string]any{"protocolVersion": 1}); err != nil {
		return err
	}

	// Step 2: session/resume or session/new.
	req, err = readReq()
	if err != nil {
		return err
	}
	var method string
	_ = json.Unmarshal(req["method"], &method)

	switch scenario {
	case "resume_null_new_success":
		// Resume returns a JSON null body (no error) — provider signals it
		// cannot resume via null rather than an error. Multica must fall back.
		if method != "session/resume" {
			return fmt.Errorf("scenario %q: expected session/resume, got %s", scenario, method)
		}
		if err := sendResult(req["id"], nil); err != nil { // nil → JSON null
			return err
		}
		// Multica falls back to session/new.
		req, err = readReq()
		if err != nil {
			return err
		}
		if err := sendResult(req["id"], map[string]any{"sessionId": "new-session-abc"}); err != nil {
			return err
		}

	case "resume_fail_new_success":
		// Resume must fail so Multica falls back to session/new.
		if method != "session/resume" {
			return fmt.Errorf("scenario %q: expected session/resume, got %s", scenario, method)
		}
		if err := sendError(req["id"], -32000, "session stale-session-id not found"); err != nil {
			return err
		}
		// Multica will now send session/new.
		req, err = readReq()
		if err != nil {
			return err
		}
		if err := sendResult(req["id"], map[string]any{"sessionId": "new-session-abc"}); err != nil {
			return err
		}

	case "resume_ok_prompt_stale_retry_success", "resume_ok_prompt_stale_retry_fail":
		// Resume succeeds but the session is silently dead — prompt will fail.
		if method != "session/resume" {
			return fmt.Errorf("scenario %q: expected session/resume, got %s", scenario, method)
		}
		if err := sendResult(req["id"], map[string]any{"sessionId": "stale-session-id"}); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown scenario: %q", scenario)
	}

	// Step 3: session/prompt (first attempt).
	req, err = readReq()
	if err != nil {
		return err
	}
	_ = json.Unmarshal(req["method"], &method)

	switch scenario {
	case "resume_null_new_success", "resume_fail_new_success":
		// Prompt on the new (fallback) session succeeds.
		if method != "session/prompt" {
			return fmt.Errorf("scenario %q: expected session/prompt, got %s", scenario, method)
		}
		_ = sendNotification("session/update", map[string]any{
			"sessionId": "new-session-abc",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": "recovered output"},
			},
		})
		return sendResult(req["id"], map[string]any{"stopReason": "end_turn"})

	case "resume_ok_prompt_stale_retry_success", "resume_ok_prompt_stale_retry_fail":
		// First prompt fails: session not found.
		if method != "session/prompt" {
			return fmt.Errorf("scenario %q: expected session/prompt, got %s", scenario, method)
		}
		if err := sendError(req["id"], -32000, "session stale-session-id not found"); err != nil {
			return err
		}

		// Multica recovers: sends session/new.
		req, err = readReq()
		if err != nil {
			return err
		}
		_ = json.Unmarshal(req["method"], &method)
		if method != "session/new" {
			return fmt.Errorf("scenario %q: expected session/new for recovery, got %s", scenario, method)
		}
		if err := sendResult(req["id"], map[string]any{"sessionId": "recovery-session-xyz"}); err != nil {
			return err
		}

		// Retry prompt.
		req, err = readReq()
		if err != nil {
			return err
		}
		_ = json.Unmarshal(req["method"], &method)
		if method != "session/prompt" {
			return fmt.Errorf("scenario %q: expected retry session/prompt, got %s", scenario, method)
		}

		if scenario == "resume_ok_prompt_stale_retry_success" {
			_ = sendNotification("session/update", map[string]any{
				"sessionId": "recovery-session-xyz",
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]any{"type": "text", "text": "retry succeeded"},
				},
			})
			return sendResult(req["id"], map[string]any{"stopReason": "end_turn"})
		}

		// retry_fail: the retry also fails with a real (non-stale) error.
		return sendError(req["id"], -32001, "upstream LLM unreachable")
	}

	return nil
}
