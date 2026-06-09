package agent

// CEREBRO-PATCH(agent-firtal-local): TECH-3226 tests for the local Ollama tool-loop backend.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// localChatRecorder is a fake Ollama /v1/chat/completions endpoint that returns
// a scripted sequence of responses, one per call.
type localChatRecorder struct {
	mu        sync.Mutex
	responses []string
	calls     int
	bodies    []map[string]any
}

func (r *localChatRecorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)

		r.mu.Lock()
		idx := r.calls
		r.calls++
		r.bodies = append(r.bodies, parsed)
		r.mu.Unlock()

		if idx >= len(r.responses) {
			http.Error(w, "no scripted response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, r.responses[idx])
	}
}

func toolCallResponse(name, argsJSON string) string {
	return `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"` +
		name + `","arguments":` + strconv.Quote(argsJSON) + `}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`
}

func textResponse(text string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` + strconv.Quote(text) + `}}],"usage":{"prompt_tokens":12,"completion_tokens":8}}`
}

func runLocalBackend(t *testing.T, b *firtalLocalBackend, prompt string, opts ExecOptions) Result {
	t.Helper()
	sess, err := b.Execute(context.Background(), prompt, opts)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Drain messages so the goroutine is never blocked on the channel buffer.
	go func() {
		for range sess.Messages {
		}
	}()
	select {
	case res := <-sess.Result:
		return res
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for result")
		return Result{}
	}
}

func TestFirtalLocalToolLoop(t *testing.T) {
	rec := &localChatRecorder{responses: []string{
		toolCallResponse("get_issue", `{"issue_id":"TECH-123"}`),
		textResponse("The issue title is Hello and I have answered it."),
	}}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	var gotName string
	var gotArgs map[string]any
	b := &firtalLocalBackend{
		cfg:        Config{Env: map[string]string{"FIRTAL_LOCAL_OLLAMA_URL": srv.URL}},
		httpClient: srv.Client(),
		runTool: func(ctx context.Context, name string, args map[string]any) (string, error) {
			gotName = name
			gotArgs = args
			return `{"title":"Hello","status":"todo"}`, nil
		},
	}

	res := runLocalBackend(t, b, "Please solve issue TECH-123", ExecOptions{Model: "gemma4:12b-it-qat"})

	if res.Status != "completed" {
		t.Fatalf("status = %q (err=%q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Output, "answered it") {
		t.Fatalf("unexpected output: %q", res.Output)
	}
	if gotName != "get_issue" {
		t.Fatalf("tool name = %q, want get_issue", gotName)
	}
	if gotArgs["issue_id"] != "TECH-123" {
		t.Fatalf("tool args = %v, want issue_id=TECH-123", gotArgs)
	}
	if rec.calls != 2 {
		t.Fatalf("model calls = %d, want 2", rec.calls)
	}
	// First call must advertise tools; the issue request carries the tool defs.
	if _, ok := rec.bodies[0]["tools"]; !ok {
		t.Fatalf("first request missing tools")
	}
	if got := res.Usage["gemma4:12b-it-qat"].InputTokens; got != 22 {
		t.Fatalf("accumulated input tokens = %d, want 22", got)
	}
}

func TestFirtalLocalChatNoTools(t *testing.T) {
	rec := &localChatRecorder{responses: []string{textResponse("Hej Jesper, hvordan kan jeg hjælpe?")}}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	b := &firtalLocalBackend{
		cfg:        Config{Env: map[string]string{"FIRTAL_LOCAL_OLLAMA_URL": srv.URL}},
		httpClient: srv.Client(),
	}
	res := runLocalBackend(t, b, "Hej", ExecOptions{})
	if res.Status != "completed" {
		t.Fatalf("status = %q (err=%q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Output, "hjælpe") {
		t.Fatalf("unexpected output: %q", res.Output)
	}
	if rec.calls != 1 {
		t.Fatalf("model calls = %d, want 1", rec.calls)
	}
}

func TestFirtalLocalToolErrorIsRecoverable(t *testing.T) {
	// Tool fails on first round; the model should still get a textual error and
	// be able to produce a final answer on the next round.
	rec := &localChatRecorder{responses: []string{
		toolCallResponse("get_issue", `{"issue_id":"X-1"}`),
		textResponse("I could not read the issue, please check the id."),
	}}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	b := &firtalLocalBackend{
		cfg:        Config{Env: map[string]string{"FIRTAL_LOCAL_OLLAMA_URL": srv.URL}},
		httpClient: srv.Client(),
		runTool: func(ctx context.Context, name string, args map[string]any) (string, error) {
			return "", io.ErrUnexpectedEOF
		},
	}
	res := runLocalBackend(t, b, "solve X-1", ExecOptions{})
	if res.Status != "completed" {
		t.Fatalf("status = %q (err=%q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Output, "could not read") {
		t.Fatalf("unexpected output: %q", res.Output)
	}
}

func TestFirtalLocalConfigDefaults(t *testing.T) {
	b := &firtalLocalBackend{cfg: Config{}}
	cfg := b.config(ExecOptions{})
	if cfg.BaseURL != firtalLocalDefaultURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, firtalLocalDefaultURL)
	}
	if cfg.Model != firtalLocalDefaultModel {
		t.Fatalf("Model = %q, want %q", cfg.Model, firtalLocalDefaultModel)
	}
	if cfg.MaxToolRound != firtalLocalMaxToolRounds {
		t.Fatalf("MaxToolRound = %d, want %d", cfg.MaxToolRound, firtalLocalMaxToolRounds)
	}

	b2 := &firtalLocalBackend{cfg: Config{Env: map[string]string{"FIRTAL_LOCAL_MODEL": "qwen2.5:14b"}}}
	if got := b2.config(ExecOptions{}).Model; got != "qwen2.5:14b" {
		t.Fatalf("env model override = %q", got)
	}
	if got := b2.config(ExecOptions{Model: "gemma4:26b-a4b-it-qat"}).Model; got != "gemma4:26b-a4b-it-qat" {
		t.Fatalf("opts model override = %q", got)
	}
}

func TestFirtalLocalNewBackend(t *testing.T) {
	be, err := New(firtalLocalProvider, Config{})
	if err != nil {
		t.Fatalf("New(firtal-local): %v", err)
	}
	if _, ok := be.(*firtalLocalBackend); !ok {
		t.Fatalf("New returned %T, want *firtalLocalBackend", be)
	}
}

// TestFirtalLocalAddCommentSuppressesFinalOutput verifies that when the model
// uses add_comment during the tool loop, the final plain-text output is
// suppressed so the daemon does not post a second comment.
func TestFirtalLocalAddCommentSuppressesFinalOutput(t *testing.T) {
	rec := &localChatRecorder{responses: []string{
		toolCallResponse("add_comment", `{"issue_id":"TECH-1","content":"My reply."}`),
		textResponse("Done, I posted the reply."),
	}}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	b := &firtalLocalBackend{
		cfg:        Config{Env: map[string]string{"FIRTAL_LOCAL_OLLAMA_URL": srv.URL}},
		httpClient: srv.Client(),
		runTool: func(_ context.Context, _ string, _ map[string]any) (string, error) {
			return `{"id":"c1","issue_id":"TECH-1"}`, nil
		},
	}
	res := runLocalBackend(t, b, "Handle TECH-1", ExecOptions{})
	if res.Status != "completed" {
		t.Fatalf("status = %q (err=%q)", res.Status, res.Error)
	}
	// Output must be empty — daemon should NOT post a second comment.
	if res.Output != "" {
		t.Fatalf("expected empty output after add_comment, got %q", res.Output)
	}
}

// TestFirtalLocalFetchGrantedToolsFilters verifies that buildToolList only
// exposes tools that are enabled in the grant list returned by the server.
func TestFirtalLocalFetchGrantedToolsFilters(t *testing.T) {
	// Fake /api/agents/{id}/tools endpoint returning only get_issue + list_comments enabled.
	grantSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"name":"get_issue","enabled":true},
			{"name":"list_comments","enabled":true},
			{"name":"add_comment","enabled":false},
			{"name":"list_issues","enabled":false}
		]`)
	}))
	defer grantSrv.Close()

	b := &firtalLocalBackend{
		cfg: Config{Env: map[string]string{
			"MULTICA_SERVER_URL": grantSrv.URL,
			"MULTICA_TOKEN":      "test-token",
			"MULTICA_AGENT_ID":   "agent-uuid",
		}},
		httpClient: grantSrv.Client(),
	}

	tools := b.buildToolList(context.Background())
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools (get_issue + list_comments), got %d: %v", len(tools), toolNames(tools))
	}
	if tools[0].Function.Name != "get_issue" || tools[1].Function.Name != "list_comments" {
		t.Fatalf("unexpected tools: %v", toolNames(tools))
	}
}

// TestFirtalLocalFetchGrantedToolsFallback verifies that when the grant server
// returns no enabled tools (empty config), all local tools are exposed.
func TestFirtalLocalFetchGrantedToolsFallback(t *testing.T) {
	grantSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// All disabled — no grants configured yet.
		_, _ = io.WriteString(w, `[{"name":"get_issue","enabled":false}]`)
	}))
	defer grantSrv.Close()

	b := &firtalLocalBackend{
		cfg: Config{Env: map[string]string{
			"MULTICA_SERVER_URL": grantSrv.URL,
			"MULTICA_TOKEN":      "test-token",
			"MULTICA_AGENT_ID":   "agent-uuid",
		}},
		httpClient: grantSrv.Client(),
	}

	tools := b.buildToolList(context.Background())
	if len(tools) != len(firtalLocalAllToolDefs()) {
		t.Fatalf("expected all %d tools, got %d", len(firtalLocalAllToolDefs()), len(tools))
	}
}

func toolNames(tools []firtalLocalToolDef) []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = t.Function.Name
	}
	return out
}

// TestFirtalLocalRunToolHTTP verifies that runToolHTTP posts to the invoke
// endpoint with the correct Authorization header and returns the result field.
func TestFirtalLocalRunToolHTTP(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":"{\"title\":\"Hello\"}"}`)
	}))
	defer srv.Close()

	b := &firtalLocalBackend{
		cfg: Config{Env: map[string]string{
			"MULTICA_SERVER_URL": srv.URL,
			"MULTICA_TOKEN":      "tok-abc",
			"MULTICA_AGENT_ID":   "agent-1",
		}},
		httpClient: srv.Client(),
	}

	result, err := b.runToolHTTP(context.Background(), "get_issue", map[string]any{"issue_id": "TECH-1"})
	if err != nil {
		t.Fatalf("runToolHTTP: %v", err)
	}
	if gotPath != "/api/agents/agent-1/tools/get_issue/invoke" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Fatalf("unexpected auth: %q", gotAuth)
	}
	if args, _ := gotBody["args"].(map[string]any); args["issue_id"] != "TECH-1" {
		t.Fatalf("unexpected args: %v", gotBody)
	}
	if !strings.Contains(result, "Hello") {
		t.Fatalf("unexpected result: %q", result)
	}
}

// TestFirtalLocalDispatchPrefersHTTP verifies that dispatch uses runToolHTTP
// when HTTP env vars are set and runTool is nil (no test override).
func TestFirtalLocalDispatchPrefersHTTP(t *testing.T) {
	invoked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		invoked = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":"ok"}`)
	}))
	defer srv.Close()

	b := &firtalLocalBackend{
		cfg: Config{Env: map[string]string{
			"MULTICA_SERVER_URL": srv.URL,
			"MULTICA_TOKEN":      "tok",
			"MULTICA_AGENT_ID":   "a1",
		}},
		httpClient: srv.Client(),
	}

	result := b.dispatch(context.Background(), "get_issue", map[string]any{"issue_id": "X"})
	if !invoked {
		t.Fatal("expected runToolHTTP to be called, not CLI")
	}
	if result != "ok" {
		t.Fatalf("unexpected result: %q", result)
	}
}

// TestFirtalLocalBuildToolListFromServer verifies that buildToolList returns
// server-provided tools (with schemas) when HTTP env is set and the server
// returns input_schema fields.
func TestFirtalLocalBuildToolListFromServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"name":"web_fetch","enabled":true,"description":"Fetch a URL","input_schema":{"type":"object","properties":{"url":{"type":"string"}}}},
			{"name":"get_issue","enabled":true,"description":"Get an issue","input_schema":{"type":"object","properties":{"issue_id":{"type":"string"}}}},
			{"name":"add_comment","enabled":false,"description":"Add comment","input_schema":{"type":"object"}}
		]`)
	}))
	defer srv.Close()

	b := &firtalLocalBackend{
		cfg: Config{Env: map[string]string{
			"MULTICA_SERVER_URL": srv.URL,
			"MULTICA_TOKEN":      "tok",
			"MULTICA_AGENT_ID":   "a1",
		}},
		httpClient: srv.Client(),
	}

	tools := b.buildToolList(context.Background())
	// Only 2 tools enabled; add_comment disabled should be excluded.
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools (web_fetch + get_issue), got %d: %v", len(tools), toolNames(tools))
	}
	if tools[0].Function.Name != "web_fetch" || tools[1].Function.Name != "get_issue" {
		t.Fatalf("unexpected tools: %v", toolNames(tools))
	}
	// Schema should be propagated.
	if tools[0].Function.Parameters == nil {
		t.Fatalf("web_fetch should have a non-nil Parameters")
	}
}
