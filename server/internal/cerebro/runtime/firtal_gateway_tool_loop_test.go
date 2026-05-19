package runtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/mcp"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeGatewayScript runs the test against a scripted series of gateway
// responses — one per call. The script is consumed in order; over-read fails
// the test so an unbounded loop is caught.
func fakeGatewayScript(t *testing.T, responses []string) (*GatewayClient, *int32) {
	t.Helper()
	var idx int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := atomic.AddInt32(&idx, 1) - 1
		if int(i) >= len(responses) {
			t.Fatalf("gateway called %d times; script only has %d responses", i+1, len(responses))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(responses[i]))
	}))
	t.Cleanup(srv.Close)
	client := NewGatewayClient(FirtalGatewayRuntimeConfig{
		BaseURL:   srv.URL,
		APIKey:    "rk_test",
		Model:     "claude-sonnet-4-6",
		MaxTokens: 4096,
	}, srv.Client())
	return client, &idx
}

func TestRunToolLoopReturnsFinalTextAfterToolDispatch(t *testing.T) {
	gateway, calls := fakeGatewayScript(t, []string{
		// Turn 1: model asks to call get_issue.
		`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hej\"}"}}]}}],"firtal":{"input_tokens":5,"output_tokens":2}}`,
		// Turn 2: model returns text — loop should exit.
		`{"choices":[{"message":{"content":"final answer"}}],"firtal":{"input_tokens":3,"output_tokens":4}}`,
	})

	srv := mcp.NewServer("test-tools", "0.0.0")
	srv.RegisterTool(mcp.Tool{Name: "echo", Description: "echo back the text arg"}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult("echoed:" + args["text"].(string)), nil
	})

	e := &FirtalGatewayExecutor{
		gateway: gateway,
		logger:  testLogger(),
	}
	completion, err := e.runToolLoopWithServer(context.Background(),
		FirtalGatewayRuntimeConfig{BaseURL: "https://x", APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096},
		db.Agent{},
		[]GatewayMessage{
			{Role: "system", Content: "be helpful"},
			{Role: "user", Content: "do something"},
		},
		GatewayRequestMeta{TaskID: "t1"},
		srv,
		[]GatewayToolDef{{Type: "function", Function: GatewayToolFunction{Name: "echo"}}},
	)
	if err != nil {
		t.Fatalf("runToolLoop error = %v", err)
	}
	if completion.Output != "final answer" {
		t.Fatalf("Output = %q", completion.Output)
	}
	if atomic.LoadInt32(calls) != 2 {
		t.Fatalf("gateway called %d times, want 2", *calls)
	}
	if completion.Usage.InputTokens != 8 || completion.Usage.OutputTokens != 6 {
		t.Fatalf("usage not summed across iterations: %+v", completion.Usage)
	}
}

func TestRunToolLoopForcesFinalAnswerWhenModelKeepsCallingTools(t *testing.T) {
	// When the model insists on calling tools every round, the loop should
	// exhaust the per-round budget and then make ONE final call so the model
	// is forced to produce a text answer. This is what guarantees the
	// acceptance flow `get_issue` → `list_comments` → `add_comment` returns
	// success — the third round dispatches add_comment, then the forced
	// no-tool call collects the model's confirmation text.
	infinite := `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"echo","arguments":"{}"}}]}}],"firtal":{"input_tokens":1,"output_tokens":1}}`
	final := `{"choices":[{"message":{"content":"forced final"}}],"firtal":{"input_tokens":7,"output_tokens":11}}`

	script := make([]string, 0, firtalGatewayMaxToolRounds+1)
	for i := 0; i < firtalGatewayMaxToolRounds; i++ {
		script = append(script, infinite)
	}
	script = append(script, final)
	gateway, calls := fakeGatewayScript(t, script)

	toolSrv := mcp.NewServer("test-tools", "0.0.0")
	toolSrv.RegisterTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult("ok"), nil
	})

	e := &FirtalGatewayExecutor{
		gateway: gateway,
		logger:  testLogger(),
	}
	completion, err := e.runToolLoopWithServer(context.Background(),
		FirtalGatewayRuntimeConfig{BaseURL: "https://x", APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096},
		db.Agent{},
		[]GatewayMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "go"}},
		GatewayRequestMeta{TaskID: "t1"},
		toolSrv,
		[]GatewayToolDef{{Type: "function", Function: GatewayToolFunction{Name: "echo"}}},
	)
	if err != nil {
		t.Fatalf("runToolLoop error = %v", err)
	}
	if completion.Output != "forced final" {
		t.Fatalf("Output = %q, want %q", completion.Output, "forced final")
	}
	wantCalls := firtalGatewayMaxToolRounds + 1
	if int(atomic.LoadInt32(calls)) != wantCalls {
		t.Fatalf("gateway called %d times, want %d (cap+forced final)", *calls, wantCalls)
	}
	// Usage must accumulate across all calls, including the forced final one.
	wantInput := int64(firtalGatewayMaxToolRounds) + 7
	if completion.Usage.InputTokens != wantInput {
		t.Fatalf("input usage = %d, want %d", completion.Usage.InputTokens, wantInput)
	}
}

func TestRunToolLoopForcedFinalCallOmitsTools(t *testing.T) {
	// Verify the mechanism that prevents an infinite loop: the FINAL gateway
	// request (the one made after the tool-round budget is exhausted) must
	// not include a `tools` field, otherwise the model can keep calling tools
	// forever.
	var captured []map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		captured = append(captured, body)
		w.Header().Set("Content-Type", "application/json")
		if len(captured) <= firtalGatewayMaxToolRounds {
			w.Write([]byte(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"echo","arguments":"{}"}}]}}]}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"final"}}]}`))
	}))
	defer srv.Close()
	gateway := NewGatewayClient(FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096}, srv.Client())

	toolSrv := mcp.NewServer("test-tools", "0.0.0")
	toolSrv.RegisterTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult("ok"), nil
	})

	e := &FirtalGatewayExecutor{gateway: gateway, logger: testLogger()}
	if _, err := e.runToolLoopWithServer(context.Background(),
		FirtalGatewayRuntimeConfig{BaseURL: "https://x", APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096},
		db.Agent{},
		[]GatewayMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "go"}},
		GatewayRequestMeta{TaskID: "t1"},
		toolSrv,
		[]GatewayToolDef{{Type: "function", Function: GatewayToolFunction{Name: "echo"}}},
	); err != nil {
		t.Fatalf("runToolLoop error = %v", err)
	}
	if len(captured) != firtalGatewayMaxToolRounds+1 {
		t.Fatalf("captured %d requests, want %d", len(captured), firtalGatewayMaxToolRounds+1)
	}
	// All tool-round requests MUST include `tools`.
	for i := 0; i < firtalGatewayMaxToolRounds; i++ {
		if _, ok := captured[i]["tools"]; !ok {
			t.Errorf("request %d (tool round) is missing `tools`", i+1)
		}
	}
	// The final request MUST NOT include `tools` — that's how the loop forces
	// the model to produce text instead of another tool call.
	if _, ok := captured[firtalGatewayMaxToolRounds]["tools"]; ok {
		t.Fatalf("final request still has `tools`; loop will not terminate cleanly")
	}
}

func TestRunToolLoopThreeStepAcceptanceFlow(t *testing.T) {
	// The Kristian acceptance scenario from JEH-1089: model dispatches
	// get_issue → list_comments → add_comment in three sequential rounds,
	// then returns text on the 4th call — the loop exits naturally because
	// the model produces text before hitting the cap. Prior to JEH-1092 this
	// failed with "tool loop budget exceeded"; after the fix it succeeds in
	// exactly 4 gateway calls (3 tool rounds + 1 text response).
	gateway, calls := fakeGatewayScript(t, []string{
		`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_issue","arguments":"{\"issue_id\":\"JEH-1089\"}"}}]}}],"firtal":{"input_tokens":10,"output_tokens":5}}`,
		`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c2","type":"function","function":{"name":"list_comments","arguments":"{\"issue_id\":\"JEH-1089\"}"}}]}}],"firtal":{"input_tokens":12,"output_tokens":6}}`,
		`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c3","type":"function","function":{"name":"add_comment","arguments":"{\"issue_id\":\"JEH-1089\",\"content\":\"Resumé på dansk.\"}"}}]}}],"firtal":{"input_tokens":14,"output_tokens":8}}`,
		`{"choices":[{"message":{"content":"Færdig — resumé posted."}}],"firtal":{"input_tokens":16,"output_tokens":4}}`,
	})

	srv := mcp.NewServer("test-tools", "0.0.0")
	srv.RegisterTool(mcp.Tool{Name: "get_issue"}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult(`{"id":"...","title":"POC"}`), nil
	})
	srv.RegisterTool(mcp.Tool{Name: "list_comments"}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult(`[{"author":"sara","content":"PR åbnet"}]`), nil
	})
	srv.RegisterTool(mcp.Tool{Name: "add_comment"}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult(`{"id":"new-comment-id"}`), nil
	})

	e := &FirtalGatewayExecutor{gateway: gateway, logger: testLogger()}
	completion, err := e.runToolLoopWithServer(context.Background(),
		FirtalGatewayRuntimeConfig{BaseURL: "https://x", APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096},
		db.Agent{},
		[]GatewayMessage{
			{Role: "system", Content: "you are kristian"},
			{Role: "user", Content: "Find JEH-1089, læs comment-tråden, og post et resumé."},
		},
		GatewayRequestMeta{TaskID: "t1"},
		srv,
		[]GatewayToolDef{
			{Type: "function", Function: GatewayToolFunction{Name: "get_issue"}},
			{Type: "function", Function: GatewayToolFunction{Name: "list_comments"}},
			{Type: "function", Function: GatewayToolFunction{Name: "add_comment"}},
		},
	)
	if err != nil {
		t.Fatalf("runToolLoop error = %v", err)
	}
	if completion.Output != "Færdig — resumé posted." {
		t.Fatalf("Output = %q, want confirmation text", completion.Output)
	}
	// This flow completes naturally in 4 calls (3 tool rounds then text),
	// regardless of the cap value — as long as cap >= 3.
	const wantCalls = 4
	if int(atomic.LoadInt32(calls)) != wantCalls {
		t.Fatalf("gateway called %d times, want %d (3 tool rounds + 1 text answer)", *calls, wantCalls)
	}
	// Usage must sum across all four gateway calls.
	wantInput := int64(10 + 12 + 14 + 16)
	wantOutput := int64(5 + 6 + 8 + 4)
	if completion.Usage.InputTokens != wantInput || completion.Usage.OutputTokens != wantOutput {
		t.Fatalf("usage not summed across rounds: got %+v, want input=%d output=%d", completion.Usage, wantInput, wantOutput)
	}
}

func TestRunToolLoopSendsToolResultsAsRoleToolMessages(t *testing.T) {
	var captured [][]GatewayMessage

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []GatewayMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		captured = append(captured, body.Messages)
		w.Header().Set("Content-Type", "application/json")
		if len(captured) == 1 {
			w.Write([]byte(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hej\"}"}}]}}]}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer srv.Close()
	gateway := NewGatewayClient(FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096}, srv.Client())

	toolSrv := mcp.NewServer("test-tools", "0.0.0")
	toolSrv.RegisterTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult("echoed:" + args["text"].(string)), nil
	})

	e := &FirtalGatewayExecutor{gateway: gateway, logger: testLogger()}
	if _, err := e.runToolLoopWithServer(context.Background(),
		FirtalGatewayRuntimeConfig{BaseURL: "https://x", APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096},
		db.Agent{},
		[]GatewayMessage{{Role: "system", Content: "be helpful"}, {Role: "user", Content: "summarise"}},
		GatewayRequestMeta{TaskID: "t1"},
		toolSrv,
		[]GatewayToolDef{{Type: "function", Function: GatewayToolFunction{Name: "echo"}}},
	); err != nil {
		t.Fatalf("runToolLoop error = %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("captured %d requests, want 2", len(captured))
	}
	// Second request must include the tool result.
	last := captured[1]
	if last[len(last)-1].Role != "tool" {
		t.Fatalf("last message role = %q, want tool: %+v", last[len(last)-1].Role, last[len(last)-1])
	}
	if last[len(last)-1].ToolCallID != "c1" {
		t.Fatalf("tool_call_id = %q", last[len(last)-1].ToolCallID)
	}
	if !strings.Contains(last[len(last)-1].Content, "echoed:hej") {
		t.Fatalf("tool result content = %q", last[len(last)-1].Content)
	}
}

type fallbackTestTool struct{}

func (fallbackTestTool) Name() string { return "echo" }
func (fallbackTestTool) Description() string {
	return "Echo input text."
}
func (fallbackTestTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"text"},
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
	}
}
func (fallbackTestTool) Call(ctx context.Context, args map[string]any) (string, error) {
	return "echoed:" + args["text"].(string), nil
}

func TestGatewayCompatRegistryToolLoopDispatchesTools(t *testing.T) { // CEREBRO-PATCH(firtal-gateway-tool-loop-fallback): malformed Anthropic-native tool requests must still run via chat-completions fallback.
	var captured []map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ai/proxy/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		captured = append(captured, body)
		w.Header().Set("Content-Type", "application/json")
		if len(captured) == 1 {
			w.Write([]byte(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hej\"}"}}]}}],"firtal":{"input_tokens":3,"output_tokens":2}}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"done"}}],"firtal":{"input_tokens":5,"output_tokens":7}}`))
	}))
	defer srv.Close()

	reg := NewRegistry(nil)
	reg.Register(fallbackTestTool{})
	e := &FirtalGatewayExecutor{gateway: NewGatewayClient(FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096}, srv.Client()), logger: testLogger()}
	completion, err := e.runGatewayCompatRegistryToolLoop(context.Background(),
		FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096},
		db.Agent{},
		[]GatewayMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "go"}},
		GatewayRequestMeta{TaskID: "t1"},
		pgtype.UUID{},
		pgtype.UUID{},
		reg,
		[]Tool{fallbackTestTool{}},
	)
	if err != nil {
		t.Fatalf("runGatewayCompatRegistryToolLoop error = %v", err)
	}
	if completion.Output != "done" {
		t.Fatalf("Output = %q, want done", completion.Output)
	}
	if len(captured) != 2 {
		t.Fatalf("captured %d requests, want 2", len(captured))
	}
	if _, ok := captured[0]["tools"]; !ok {
		t.Fatal("first fallback request omitted tools")
	}
	messages, ok := captured[1]["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("second fallback request missing messages: %+v", captured[1])
	}
	last, _ := messages[len(messages)-1].(map[string]any)
	if last["role"] != "tool" || !strings.Contains(last["content"].(string), "echoed:hej") {
		t.Fatalf("second fallback request missing tool result: %+v", last)
	}
}

func TestRunToolLoopUsesGatewayCompatTransportForToolEnabledTasks(t *testing.T) { // CEREBRO-PATCH(firtal-gateway-tool-loop-fallback): tool-enabled tasks must not fail before fallback on Anthropic-native /messages.
	var paths []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/ai/proxy/v1/messages" {
			http.Error(w, `{"error":"upstream_rejected_request","message":"Upstream provider anthropic rejected the request as malformed."}`, http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/api/ai/proxy/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"compat final"}}],"firtal":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer srv.Close()

	e := &FirtalGatewayExecutor{
		gateway: NewGatewayClient(FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096}, srv.Client()),
		logger:  testLogger(),
	}
	completion, err := e.runToolLoop(context.Background(),
		FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096},
		db.Agent{},
		[]GatewayMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "go"}},
		GatewayRequestMeta{TaskID: "t1"},
		pgtype.UUID{},
		pgtype.UUID{},
		pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("runToolLoop error = %v", err)
	}
	if completion.Output != "compat final" {
		t.Fatalf("Output = %q, want compat final", completion.Output)
	}
	if len(paths) != 1 || paths[0] != "/api/ai/proxy/v1/chat/completions" {
		t.Fatalf("paths = %v, want only chat-completions", paths)
	}
}

func TestWithToolUsageHintAppendsToSystemPrompt(t *testing.T) {
	in := []GatewayMessage{
		{Role: "system", Content: "Base prompt."},
		{Role: "user", Content: "go"},
	}
	out := withToolUsageHint(in)
	if !strings.HasPrefix(out[0].Content, "Base prompt.") {
		t.Fatalf("base prompt mangled: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "get_issue") || !strings.Contains(out[0].Content, "add_comment") {
		t.Fatalf("hint missing tool names: %q", out[0].Content)
	}
	if in[0].Content == out[0].Content {
		t.Fatal("withToolUsageHint should mutate a copy, not the caller's slice; the system prompt should change")
	}
	if in[0].Content != "Base prompt." {
		t.Fatalf("withToolUsageHint mutated caller's slice: original is now %q", in[0].Content)
	}
}

func TestParseIssueIdentifierNumber(t *testing.T) {
	cases := []struct {
		in     string
		want   int32
		wantOk bool
	}{
		{"JEH-1089", 1089, true},
		{"MUL-1", 1, true},
		{"abc-12", 12, true},
		{"bdc9336a-7d39-48df-904d-d2be09b45441", 0, false},
		{"JEH-", 0, false},
		{"-12", 0, false},
		{"JEH 12", 0, false},
		{"JEH1-12", 0, false}, // digit in prefix
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseIssueIdentifierNumber(tc.in)
		if got != tc.want || ok != tc.wantOk {
			t.Errorf("parseIssueIdentifierNumber(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.wantOk)
		}
	}
}

func TestGatewayToolDefsExposesPOCTools(t *testing.T) {
	defs := GatewayToolDefs()
	names := map[string]bool{}
	for _, d := range defs {
		if d.Type != "function" {
			t.Errorf("tool %q has type %q, want \"function\"", d.Function.Name, d.Type)
		}
		names[d.Function.Name] = true
	}
	for _, want := range []string{"get_issue", "list_comments", "add_comment"} {
		if !names[want] {
			t.Errorf("missing tool %q in GatewayToolDefs", want)
		}
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
