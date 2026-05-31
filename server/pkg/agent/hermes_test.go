package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

// ── extractACPCurrentModelID ──

func TestExtractACPCurrentModelID(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"sessionId": "ses_123",
		"models": {
			"currentModelId": "nous:moonshotai/kimi-k2.6"
		}
	}`)
	got := extractACPCurrentModelID(raw)
	if got != "nous:moonshotai/kimi-k2.6" {
		t.Errorf("got %q, want current model", got)
	}
}

func TestExtractACPCurrentModelIDSnakeCase(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"session_id": "ses_123",
		"models": {
			"current_model_id": "openrouter:anthropic/claude-sonnet-4.6"
		}
	}`)
	got := extractACPCurrentModelID(raw)
	if got != "openrouter:anthropic/claude-sonnet-4.6" {
		t.Errorf("got %q, want current model", got)
	}
}

func TestExtractACPCurrentModelIDMissing(t *testing.T) {
	t.Parallel()
	if got := extractACPCurrentModelID(json.RawMessage(`{"sessionId":"ses_123"}`)); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ── resolveResumedSessionID ──

func TestResolveResumedSessionIDMatching(t *testing.T) {
	t.Parallel()
	// Server confirms our requested id — happy resume path. No change.
	got, changed := resolveResumedSessionID(
		"ses_alpha",
		json.RawMessage(`{"sessionId":"ses_alpha"}`),
	)
	if got != "ses_alpha" {
		t.Errorf("got %q, want ses_alpha", got)
	}
	if changed {
		t.Errorf("changed: got true, want false")
	}
}

func TestResolveResumedSessionIDDifferent(t *testing.T) {
	t.Parallel()
	// Server returned a different id — local state was lost and the
	// server silently spun up a new session. We trust the server.
	got, changed := resolveResumedSessionID(
		"ses_alpha",
		json.RawMessage(`{"sessionId":"ses_beta_new"}`),
	)
	if got != "ses_beta_new" {
		t.Errorf("got %q, want ses_beta_new", got)
	}
	if !changed {
		t.Errorf("changed: got false, want true")
	}
}

func TestResolveResumedSessionIDEmptyResponse(t *testing.T) {
	t.Parallel()
	// Older / non-conforming server returns no sessionId — defensive
	// fallback to the requested id. This preserves the legacy happy
	// path; a stale id will eventually fail downstream and be retried
	// via the daemon's session-resume fallback (daemon.go).
	for _, body := range []string{
		`{}`,
		`{"sessionId":""}`,
		`not json`,
	} {
		got, changed := resolveResumedSessionID(
			"ses_alpha",
			json.RawMessage(body),
		)
		if got != "ses_alpha" {
			t.Errorf("body=%q: got %q, want ses_alpha", body, got)
		}
		if changed {
			t.Errorf("body=%q: changed: got true, want false", body)
		}
	}
}

// ── buildHermesSessionParams ──

func TestBuildHermesSessionParamsIncludesModel(t *testing.T) {
	t.Parallel()
	params := buildHermesSessionParams("/tmp/work", "gpt-4o", nil)
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
	params := buildHermesSessionParams("/tmp/work", "", nil)
	if _, present := params["model"]; present {
		t.Error("expected model key to be omitted when model is empty")
	}
}

func TestBuildHermesSessionParamsPassesThroughMcpServers(t *testing.T) {
	t.Parallel()
	servers := []any{map[string]any{"name": "fetch", "command": "uvx", "args": []string{}, "env": []map[string]any{}}}
	params := buildHermesSessionParams("/tmp/work", "", servers)
	got, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(got) != 1 {
		t.Fatalf("len(mcpServers): got %d, want 1", len(got))
	}
}

func TestBuildHermesSessionParamsNilMcpServersBecomesEmptyArray(t *testing.T) {
	t.Parallel()
	// ACP requires the field; nil must surface as `[]` so the wire request
	// stays well-formed even when no MCP servers are configured.
	params := buildHermesSessionParams("/tmp/work", "", nil)
	got, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(got) != 0 {
		t.Errorf("len(mcpServers): got %d, want 0", len(got))
	}
}

// ── buildACPMcpServers ──

func TestBuildACPMcpServersEmptyInputReturnsEmpty(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{nil, {}, json.RawMessage("null"), json.RawMessage(" null "), json.RawMessage("{}"), json.RawMessage(`{"mcpServers":{}}`)} {
		got, err := buildACPMcpServers(raw, slog.Default())
		if err != nil {
			t.Fatalf("raw=%q: unexpected error: %v", string(raw), err)
		}
		if got == nil {
			t.Errorf("raw=%q: got nil, want non-nil empty slice", string(raw))
		}
		if len(got) != 0 {
			t.Errorf("raw=%q: got %d entries, want 0", string(raw), len(got))
		}
	}
}

func TestBuildACPMcpServersTranslatesStdioEntry(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"],"env":{"API_KEY":"secret","HOME":"/tmp"}}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	entry, ok := got[0].(map[string]any)
	if !ok {
		t.Fatalf("entry type: got %T, want map[string]any", got[0])
	}
	if entry["name"] != "fetch" {
		t.Errorf("name: got %v, want fetch", entry["name"])
	}
	if entry["command"] != "uvx" {
		t.Errorf("command: got %v, want uvx", entry["command"])
	}
	if _, hasType := entry["type"]; hasType {
		t.Errorf("stdio entry should not include type field, got %v", entry["type"])
	}
	args, ok := entry["args"].([]string)
	if !ok || len(args) != 1 || args[0] != "mcp-server-fetch" {
		t.Errorf("args: got %v, want [mcp-server-fetch]", entry["args"])
	}
	envArr, ok := entry["env"].([]map[string]any)
	if !ok {
		t.Fatalf("env type: got %T, want []map[string]any", entry["env"])
	}
	// Env entries sorted by key for determinism.
	if len(envArr) != 2 {
		t.Fatalf("len(env): got %d, want 2", len(envArr))
	}
	if envArr[0]["name"] != "API_KEY" || envArr[0]["value"] != "secret" {
		t.Errorf("env[0]: got %v, want {name:API_KEY,value:secret}", envArr[0])
	}
	if envArr[1]["name"] != "HOME" || envArr[1]["value"] != "/tmp" {
		t.Errorf("env[1]: got %v, want {name:HOME,value:/tmp}", envArr[1])
	}
}

func TestBuildACPMcpServersStdioWithoutArgsOrEnvUsesEmptyArrays(t *testing.T) {
	t.Parallel()
	// ACP requires args and env to be arrays; missing fields must become
	// `[]` rather than null so the wire shape passes server-side validation.
	raw := json.RawMessage(`{"mcpServers":{"minimal":{"command":"echo"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry := got[0].(map[string]any)
	if args, ok := entry["args"].([]string); !ok || len(args) != 0 {
		t.Errorf("args: got %v, want []", entry["args"])
	}
	if env, ok := entry["env"].([]map[string]any); !ok || len(env) != 0 {
		t.Errorf("env: got %v, want []", entry["env"])
	}
}

func TestBuildACPMcpServersTranslatesHttpEntry(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"remote":{"type":"http","url":"https://example.com/mcp","headers":{"Authorization":"Bearer x","X-Trace":"abc"}}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	entry := got[0].(map[string]any)
	if entry["type"] != "http" {
		t.Errorf("type: got %v, want http", entry["type"])
	}
	if entry["name"] != "remote" {
		t.Errorf("name: got %v, want remote", entry["name"])
	}
	if entry["url"] != "https://example.com/mcp" {
		t.Errorf("url: got %v, want https://example.com/mcp", entry["url"])
	}
	headers, ok := entry["headers"].([]map[string]any)
	if !ok || len(headers) != 2 {
		t.Fatalf("headers: got %v, want 2 entries", entry["headers"])
	}
	if headers[0]["name"] != "Authorization" {
		t.Errorf("headers[0].name: got %v, want Authorization", headers[0]["name"])
	}
}

func TestBuildACPMcpServersDefaultsRemoteTypeToHttp(t *testing.T) {
	t.Parallel()
	// A `url` without `type` should default to "http" rather than be classified
	// as stdio or get dropped.
	raw := json.RawMessage(`{"mcpServers":{"remote":{"url":"https://example.com/mcp"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry := got[0].(map[string]any)
	if entry["type"] != "http" {
		t.Errorf("type: got %v, want http (default)", entry["type"])
	}
}

func TestBuildACPMcpServersSupportsSseTransport(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"mcpServers":{"remote":{"type":"sse","url":"https://example.com/sse"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry := got[0].(map[string]any)
	if entry["type"] != "sse" {
		t.Errorf("type: got %v, want sse", entry["type"])
	}
}

func TestBuildACPMcpServersAcceptsStreamableHttpAlias(t *testing.T) {
	t.Parallel()
	// Claude's MCP CLI uses "streamable-http" / "http_streamable" as
	// aliases for the http transport; ACP only knows "http", so the
	// translator must collapse the alias.
	for _, alias := range []string{"streamable-http", "http_streamable", "Streamable-HTTP"} {
		raw := json.RawMessage(`{"mcpServers":{"remote":{"type":"` + alias + `","url":"https://example.com/mcp"}}}`)
		got, err := buildACPMcpServers(raw, slog.Default())
		if err != nil {
			t.Fatalf("alias=%s: unexpected error: %v", alias, err)
		}
		entry := got[0].(map[string]any)
		if entry["type"] != "http" {
			t.Errorf("alias=%s: type got %v, want http", alias, entry["type"])
		}
	}
}

func TestBuildACPMcpServersSortsEntriesByName(t *testing.T) {
	t.Parallel()
	// Map iteration is randomized in Go; the translator sorts by name so
	// the wire request and test assertions are deterministic.
	raw := json.RawMessage(`{"mcpServers":{"zeta":{"command":"z"},"alpha":{"command":"a"},"mid":{"command":"m"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"alpha", "mid", "zeta"}
	for i, w := range want {
		if got[i].(map[string]any)["name"] != w {
			t.Errorf("position %d: got %v, want %s", i, got[i].(map[string]any)["name"], w)
		}
	}
}

func TestBuildACPMcpServersSkipsInvalidEntriesAndContinues(t *testing.T) {
	t.Parallel()
	// An entry with neither command nor url is invalid — drop it with a
	// warning rather than failing the whole launch, so a single bad entry
	// in the agent UI doesn't take MCP down for the rest of the agent.
	raw := json.RawMessage(`{"mcpServers":{"bad":{"args":["nothing"]},"good":{"command":"uvx"}}}`)
	got, err := buildACPMcpServers(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1 (bad entry should be skipped)", len(got))
	}
	if got[0].(map[string]any)["name"] != "good" {
		t.Errorf("kept the wrong entry: %v", got[0])
	}
}

func TestBuildACPMcpServersReturnsErrorOnMalformedJSON(t *testing.T) {
	t.Parallel()
	_, err := buildACPMcpServers(json.RawMessage(`not json`), slog.Default())
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse mcp_config json") {
		t.Errorf("error message: got %q, want it to mention parsing", err.Error())
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

// TestHermesClientHandleLineErrorWithData guards #2192-class regressions: when
// an ACP backend returns -32603 (Internal error), the meaningful reason lives
// in the `data` field. Dropping it leaves operators with a bare "Internal
// error" and no way to tell apart "session expired", "model unavailable",
// "auth lost", etc. Kiro CLI 2.2.x emits `data` as a string; some backends use
// objects/arrays — both must round-trip into the wrapped Go error.
func TestHermesClientHandleLineErrorWithStringData(t *testing.T) {
	t.Parallel()

	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "session/prompt"}
	c.pending[3] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":3,"error":{"code":-32603,"message":"Internal error","data":"No session found with id"}}`)

	res := <-pr.ch
	if res.err == nil {
		t.Fatal("expected error")
	}
	want := "session/prompt: Internal error (code=-32603, data=No session found with id)"
	if got := res.err.Error(); got != want {
		t.Errorf("error: got %q, want %q", got, want)
	}
}

func TestHermesClientHandleLineErrorWithObjectData(t *testing.T) {
	t.Parallel()

	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "session/prompt"}
	c.pending[5] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":5,"error":{"code":-32000,"message":"quota","data":{"reason":"limit","remaining":0}}}`)

	res := <-pr.ch
	if res.err == nil {
		t.Fatal("expected error")
	}
	want := `session/prompt: quota (code=-32000, data={"reason":"limit","remaining":0})`
	if got := res.err.Error(); got != want {
		t.Errorf("error: got %q, want %q", got, want)
	}
}

func TestHermesClientHandleLineErrorWithNullData(t *testing.T) {
	t.Parallel()

	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
	}
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: "initialize"}
	c.pending[7] = pr

	c.handleLine(`{"jsonrpc":"2.0","id":7,"error":{"code":-32600,"message":"bad request","data":null}}`)

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

// Regression for #1997: Hermes ACP can flush queued session updates from
// the previous turn (history replay on session/resume, or chunks queued
// before our session/prompt response is sent) before the current turn
// actually starts. Until acceptNotification gates them out, those updates
// were appended to output and re-sent to the UI, making the previous
// answer appear duplicated alongside the new one. The Backend wires the
// gate to a streamingCurrentTurn flag set just before session/prompt; here
// we exercise the gate directly on hermesClient.
func TestHermesClientAcceptNotificationGate(t *testing.T) {
	t.Parallel()

	var (
		got    []Message
		accept bool
	)
	c := &hermesClient{
		pending: make(map[int]*pendingRPC),
		acceptNotification: func(string) bool {
			return accept
		},
		onMessage: func(msg Message) {
			got = append(got, msg)
		},
	}

	replay := `{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"history should be ignored"}}}}`
	c.handleLine(replay)
	if len(got) != 0 {
		t.Fatalf("expected gate to drop replay before turn starts, got %+v", got)
	}

	accept = true
	live := `{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_1","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"current"}}}}`
	c.handleLine(live)
	if len(got) != 1 {
		t.Fatalf("expected current-turn update to pass the gate, got %+v", got)
	}
	if got[0].Content != "current" {
		t.Fatalf("got content %q, want \"current\"", got[0].Content)
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

func fakeHermesACPUsageWithDefaultModelScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_model","models":{"currentModelId":"nous:moonshotai/kimi-k2.6","availableModels":[{"modelId":"nous:moonshotai/kimi-k2.6","name":"moonshotai/kimi-k2.6"}]}}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":17,"outputTokens":5,"cachedReadTokens":3}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestHermesBackendAttributesUsageToACPDefaultModel(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeHermesACPUsageWithDefaultModelScript()))

	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
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
		if result.Status != "completed" {
			t.Fatalf("expected completed result, got %q: %s", result.Status, result.Error)
		}
		if _, ok := result.Usage["unknown"]; ok {
			t.Fatalf("usage should not be attributed to unknown: %+v", result.Usage)
		}
		usage, ok := result.Usage["nous:moonshotai/kimi-k2.6"]
		if !ok {
			t.Fatalf("expected usage under Hermes current model, got %+v", result.Usage)
		}
		if usage.InputTokens != 17 || usage.OutputTokens != 5 || usage.CacheReadTokens != 3 {
			t.Fatalf("usage = %+v, want input=17 output=5 cache_read=3", usage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// fakeHermesACPRateLimitScript impersonates hermes for the GitHub
// multica#1952 scenario: the upstream LLM returns HTTP 429 (rate
// limited / no credit), hermes retries internally and ultimately
// emits both a sniffable stderr error block AND a synthetic agent
// text turn ("API call failed after 3 retries..."), then completes
// session/prompt with stopReason=end_turn (NOT an RPC error). The
// daemon must still treat this as a failed run, not a successful
// one — which means the hermes backend has to promote the status
// to "failed" even though `output` is non-empty.
func fakeHermesACPRateLimitScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_429"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      # Mimic hermes' real-world stderr block on a 429.
      printf '%s\n' '⚠️  API call failed (attempt 3/3): RateLimitError [HTTP 429]' >&2
      printf '%s\n' '   📝 Error: HTTP 429: The usage limit has been reached' >&2
      # Mimic hermes injecting the failure as a synthetic agent turn so
      # the chat shows *something*; this puts text in output and used to
      # mask the failure from the daemon.
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_429","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"API call failed after 3 retries: HTTP 429: The usage limit has been reached"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestHermesProviderErrorSnifferTerminalVsTransient verifies the
// sniffer reports terminalMessage()=="" for a per-attempt warning
// that did NOT escalate to an exhausted/non-retryable failure, but
// still returns the same string from message() so callers wanting
// diagnostic text can use it. This is what prevents the
// promote-on-any-sniff false positive (a transient `attempt 1/3`
// followed by a successful retry must stay "completed").
func TestHermesProviderErrorSnifferTerminalVsTransient(t *testing.T) {
	t.Parallel()

	// Transient: the sniffer DID see something matching acpErrorHeaderRe
	// (so `message()` is non-empty for diagnostic purposes), but the
	// signal is just "attempt 1/3 against a retryable rate limit" — no
	// terminal markers at all.
	s := newACPProviderErrorSniffer("hermes")
	s.Write([]byte("⚠️  API call failed (attempt 1/3): retryable upstream blip\n"))
	if msg := s.message(); msg == "" {
		t.Fatalf("sniffer should still capture transient warnings for diagnostics")
	}
	if msg := s.terminalMessage(); msg != "" {
		t.Fatalf("transient attempt should NOT be a terminal failure, got %q", msg)
	}

	// Now feed a follow-on terminal marker. terminalMessage must turn on.
	s.Write([]byte("❌  API call failed after 3 retries: usage limit reached\n"))
	if msg := s.terminalMessage(); msg == "" {
		t.Fatalf("after-N-retries / ❌ should switch terminalMessage on")
	}
}

// TestHermesProviderErrorSnifferTerminalNonRetryable verifies that a
// non-retryable error (BadRequest / Authentication / Non-retryable)
// is treated as terminal even on attempt 1/3 — those errors don't
// retry, so the very first failure is the final disposition. Also
// covers ❌ / [ERROR] / "after N retries" markers that adapters
// emit on give-up.
func TestHermesProviderErrorSnifferTerminalNonRetryable(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		`⚠️  API call failed (attempt 1/3): BadRequestError [HTTP 400]`,
		`⚠️  API call failed (attempt 1/3): AuthenticationError [HTTP 401]`,
		`⚠️  API call failed (HTTP 400) attempt a: Non-retryable error`,
		`❌ API call failed after 3 retries: RateLimitError [HTTP 429]`,
		`[ERROR] API call failed: upstream returned HTTP 500`,
	} {
		s := newACPProviderErrorSniffer("hermes")
		s.Write([]byte(line + "\n"))
		if msg := s.terminalMessage(); msg == "" {
			t.Errorf("expected %q to be classified as terminal", line)
		}
	}
}

// TestHermesBackendPromotesProviderErrorWithNonEmptyOutput pins the
// fix for GitHub multica#1952: a hermes run that hits a 429 (or any
// upstream provider error) must surface as Status=failed even though
// hermes' synthetic "API call failed..." agent turn means the output
// buffer is non-empty. Before the fix the sniffer-promotion was
// gated on `finalOutput == ""`, so the run silently completed.
func TestHermesBackendPromotesProviderErrorWithNonEmptyOutput(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeHermesACPRateLimitScript()))

	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
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
		if result.Status != "failed" {
			t.Fatalf("expected status=failed (sniffer should promote on 429 even with non-empty output), got %q (error=%q output=%q)", result.Status, result.Error, result.Output)
		}
		if !strings.Contains(result.Error, "429") && !strings.Contains(result.Error, "usage limit") {
			t.Errorf("expected error to surface the 429 / usage-limit message, got %q", result.Error)
		}
		if result.SessionID != "ses_429" {
			t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// fakeHermesACPTransientRetryScript emits a single retryable per-
// attempt warning to stderr and then completes with a normal agent
// text turn — the situation where the upstream LLM blipped on
// attempt 1/3 but a subsequent attempt succeeded and produced a
// real answer. The previous (too-broad) promotion logic would have
// flipped this to status=failed; the fix must keep it as completed.
func fakeHermesACPTransientRetryScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_ok"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      # Per-attempt rate-limit warning that hermes routinely logs on
      # transient blips — the request DOES retry and succeed below.
      printf '%s\n' '⚠️  API call failed (attempt 1/3): RateLimitError [HTTP 429]' >&2
      # Real agent answer streamed back as a normal text turn.
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_ok","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Here is the answer you asked for."}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestHermesBackendDoesNotPromoteOnTransientRetry pins the
// regression GPT-Boy flagged on the multica#1952 fix: a per-attempt
// ⚠️ warning on stderr that does NOT include any terminal marker
// ("after N retries", Non-retryable, ❌, [ERROR], BadRequest /
// Authentication errors) and is followed by a successful agent
// turn must stay status=completed. The previous "any sniffer line
// → fail" rule would have wrongly marked this run as failed.
func TestHermesBackendDoesNotPromoteOnTransientRetry(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeHermesACPTransientRetryScript()))

	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
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
		if result.Status != "completed" {
			t.Fatalf("transient retry that ultimately succeeded must stay status=completed, got %q (error=%q output=%q)", result.Status, result.Error, result.Output)
		}
		if !strings.Contains(result.Output, "Here is the answer") {
			t.Errorf("expected the successful agent turn to be in output, got %q", result.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// ── extractACPMcpCapabilities ──

func TestExtractACPMcpCapabilities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		wantHTTP bool
		wantSSE  bool
	}{
		{
			name:     "both true",
			raw:      `{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"http":true,"sse":true}}}`,
			wantHTTP: true,
			wantSSE:  true,
		},
		{
			name:     "http only",
			raw:      `{"agentCapabilities":{"mcpCapabilities":{"http":true}}}`,
			wantHTTP: true,
			wantSSE:  false,
		},
		{
			name:     "sse only",
			raw:      `{"agentCapabilities":{"mcpCapabilities":{"sse":true}}}`,
			wantHTTP: false,
			wantSSE:  true,
		},
		{
			name:     "block missing",
			raw:      `{"agentCapabilities":{}}`,
			wantHTTP: false,
			wantSSE:  false,
		},
		{
			name:     "agentCapabilities missing",
			raw:      `{"protocolVersion":1}`,
			wantHTTP: false,
			wantSSE:  false,
		},
		{
			name:     "malformed json",
			raw:      `not json`,
			wantHTTP: false,
			wantSSE:  false,
		},
	}
	for _, tc := range tests {
		got := extractACPMcpCapabilities(json.RawMessage(tc.raw))
		if got.HTTP != tc.wantHTTP || got.SSE != tc.wantSSE {
			t.Errorf("%s: got {HTTP:%v SSE:%v}, want {HTTP:%v SSE:%v}", tc.name, got.HTTP, got.SSE, tc.wantHTTP, tc.wantSSE)
		}
	}
}

// ── filterACPMcpServersByCapability ──

func TestFilterACPMcpServersByCapabilityStdioAlwaysPassesThrough(t *testing.T) {
	t.Parallel()
	// Stdio entries have no `type` field — the ACP spec doesn't gate stdio,
	// so the filter must pass them through regardless of capabilities.
	servers := []any{
		map[string]any{"name": "fetch", "command": "uvx"},
	}
	got := filterACPMcpServersByCapability(servers, acpMcpTransportCapabilities{}, "hermes", slog.Default())
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
}

func TestFilterACPMcpServersByCapabilityDropsUnsupportedHttp(t *testing.T) {
	t.Parallel()
	servers := []any{
		map[string]any{"name": "stdio-ok", "command": "uvx"},
		map[string]any{"type": "http", "name": "http-drop", "url": "https://x/mcp"},
		map[string]any{"type": "sse", "name": "sse-keep", "url": "https://x/sse"},
	}
	got := filterACPMcpServersByCapability(servers, acpMcpTransportCapabilities{SSE: true}, "hermes", slog.Default())
	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2 (http should be dropped, sse kept)", len(got))
	}
	names := []string{got[0].(map[string]any)["name"].(string), got[1].(map[string]any)["name"].(string)}
	wantNames := map[string]bool{"stdio-ok": true, "sse-keep": true}
	for _, n := range names {
		if !wantNames[n] {
			t.Errorf("unexpected entry kept: %q", n)
		}
	}
}

func TestFilterACPMcpServersByCapabilityDropsUnsupportedSse(t *testing.T) {
	t.Parallel()
	servers := []any{
		map[string]any{"type": "sse", "name": "sse-drop", "url": "https://x/sse"},
		map[string]any{"type": "http", "name": "http-keep", "url": "https://x/mcp"},
	}
	got := filterACPMcpServersByCapability(servers, acpMcpTransportCapabilities{HTTP: true}, "kimi", slog.Default())
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].(map[string]any)["name"] != "http-keep" {
		t.Errorf("kept wrong entry: %v", got[0])
	}
}

func TestFilterACPMcpServersByCapabilityKeepsAllWhenBothSupported(t *testing.T) {
	t.Parallel()
	servers := []any{
		map[string]any{"name": "stdio", "command": "uvx"},
		map[string]any{"type": "http", "name": "http", "url": "https://x/mcp"},
		map[string]any{"type": "sse", "name": "sse", "url": "https://x/sse"},
	}
	got := filterACPMcpServersByCapability(servers, acpMcpTransportCapabilities{HTTP: true, SSE: true}, "kiro", slog.Default())
	if len(got) != 3 {
		t.Fatalf("len: got %d, want 3", len(got))
	}
}

func TestFilterACPMcpServersByCapabilityEmptyInputReturnsEmpty(t *testing.T) {
	t.Parallel()
	got := filterACPMcpServersByCapability(nil, acpMcpTransportCapabilities{HTTP: true, SSE: true}, "hermes", slog.Default())
	if len(got) != 0 {
		t.Errorf("len: got %d, want 0", len(got))
	}
}

// TestHermesExecuteFailsClosedOnMalformedMcpConfig pins the contract that
// a malformed mcp_config aborts the launch *before* the child is spawned.
// Silently launching with no MCP servers would look indistinguishable
// from "the saved config was applied" and is exactly the surprise the
// MCP Tab is meant to remove.
func TestHermesExecuteFailsClosedOnMalformedMcpConfig(t *testing.T) {
	t.Parallel()

	// Any existing executable is fine — Execute returns before the spawn.
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nexit 0\n"))

	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
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

// fakeACPRecordingScript impersonates an ACP agent that records every
// JSON-RPC frame it receives to a file (one per line) before responding.
// The runtime name parameter lets the same script drive Hermes / Kimi /
// Kiro fakes — only the session/load vs session/resume method differs.
//
// `caps` is the JSON for `agentCapabilities` returned from initialize so
// tests can pin the capability gate (e.g. `{"mcpCapabilities":{"http":false}}`).
//
// session/new / session/resume both echo back the requested sessionId so
// tests don't need to thread one through; session/prompt returns
// end_turn so Execute completes cleanly.
func fakeACPRecordingScript(recordPath, sessionID, caps string) string {
	return `#!/bin/sh
RECORD_PATH=` + recordPath + `
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$RECORD_PATH"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":` + caps + `}}\n' "$id"
      ;;
    *'"method":"session/new"'*|*'"method":"session/resume"'*|*'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"` + sessionID + `"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// findRecordedFrame returns the first recorded JSON-RPC frame whose
// `method` matches the requested one. Used by the resume / capability
// tests below to inspect what we actually sent on the wire.
func findRecordedFrame(t *testing.T, recordPath, method string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read record file: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		if frame["method"] == method {
			return frame
		}
	}
	t.Fatalf("no recorded frame for method %q in %s", method, string(data))
	return nil
}

// TestHermesResumeIncludesMcpServers pins the contract that
// session/resume carries the managed MCP set. Without this, a resumed
// Hermes task lost access to MCP tools that a fresh task on the same
// agent would have — which is the inconsistency Elon's review flagged.
func TestHermesResumeIncludesMcpServers(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_resume", `{}`)))

	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_resume",
		McpConfig:       json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/resume")
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("session/resume params: got %T, want map", frame["params"])
	}
	servers, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("session/resume.mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(servers) != 1 {
		t.Fatalf("session/resume.mcpServers: got %d entries, want 1", len(servers))
	}
	entry := servers[0].(map[string]any)
	if entry["name"] != "fetch" || entry["command"] != "uvx" {
		t.Errorf("session/resume.mcpServers[0]: got %v, want {name:fetch,command:uvx,...}", entry)
	}
}

// TestHermesDropsRemoteMcpWhenCapabilityNotAdvertised pins the contract
// that when the runtime's initialize response advertises no http/sse
// support, those entries are filtered out of session/new — sending them
// anyway is a protocol violation that reliably tanks the request.
func TestHermesDropsRemoteMcpWhenCapabilityNotAdvertised(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "hermes")
	// agentCapabilities = {} → neither http nor sse advertised.
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_new", `{}`)))

	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{
			"local":{"command":"uvx"},
			"remote-http":{"type":"http","url":"https://x/mcp"},
			"remote-sse":{"type":"sse","url":"https://x/sse"}
		}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/new")
	params := frame["params"].(map[string]any)
	servers, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("session/new.mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(servers) != 1 {
		t.Fatalf("session/new.mcpServers: got %d entries, want 1 (only stdio should remain)", len(servers))
	}
	if servers[0].(map[string]any)["name"] != "local" {
		t.Errorf("kept the wrong entry: %v", servers[0])
	}
}

// TestHermesKeepsRemoteMcpWhenCapabilityAdvertised confirms the gate
// doesn't over-filter: when the runtime advertises http+sse, all entries
// must pass through to session/new.
func TestHermesKeepsRemoteMcpWhenCapabilityAdvertised(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_new", `{"mcpCapabilities":{"http":true,"sse":true}}`)))

	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{
			"local":{"command":"uvx"},
			"remote-http":{"type":"http","url":"https://x/mcp"},
			"remote-sse":{"type":"sse","url":"https://x/sse"}
		}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/new")
	params := frame["params"].(map[string]any)
	servers := params["mcpServers"].([]any)
	if len(servers) != 3 {
		t.Fatalf("session/new.mcpServers: got %d entries, want 3", len(servers))
	}
}
