package agent

import (
	"encoding/json"
	"log/slog"
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
