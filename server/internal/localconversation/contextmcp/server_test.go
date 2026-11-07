package contextmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func startServer(t *testing.T) *Server {
	t.Helper()
	s, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func post(t *testing.T, b Binding, token, origin string, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, b.URL, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func echoExecution() Execution {
	return Execution{Instructions: "test instructions", Tools: []Tool{
		{Name: "echo", ReadOnly: true, Description: "echo", Call: func(_ context.Context, args json.RawMessage) (Result, error) {
			var v any
			_ = json.Unmarshal(args, &v)
			return Result{Value: v}, nil
		}},
		{Name: "fail", Description: "fail", Call: func(context.Context, json.RawMessage) (Result, error) {
			return Result{}, errors.New("boom")
		}},
		{Name: "picture", Description: "image", Call: func(context.Context, json.RawMessage) (Result, error) {
			return Result{Value: map[string]any{"name": "p"}, Image: &Image{MimeType: "image/png", Data: "iVBORw0KGgo="}}, nil
		}},
	}}
}

func TestServerAuthenticatesEachRoute(t *testing.T) {
	t.Parallel()

	s := startServer(t)
	b, err := s.Bind(context.Background(), echoExecution())
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.Bind(context.Background(), echoExecution())
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if status, _ := post(t, b, "", "", call); status != http.StatusUnauthorized {
		t.Fatalf("no token = %d", status)
	}
	if status, _ := post(t, b, other.Token, "", call); status != http.StatusUnauthorized {
		t.Fatalf("another route's token = %d", status)
	}
	if status, _ := post(t, b, b.Token, "https://evil.example", call); status != http.StatusForbidden {
		t.Fatalf("browser origin = %d", status)
	}
	if status, _ := post(t, b, b.Token, "", call); status != http.StatusOK {
		t.Fatalf("valid = %d", status)
	}
	s.Release(b)
	if status, _ := post(t, b, b.Token, "", call); status != http.StatusUnauthorized {
		t.Fatalf("released route = %d", status)
	}
}

func TestServerAuthenticatesTokenInPathRoutes(t *testing.T) {
	t.Parallel()

	s := startServer(t)
	exec := echoExecution()
	exec.TokenInPath = true
	b, err := s.Bind(context.Background(), exec)
	if err != nil {
		t.Fatal(err)
	}
	header, _ := s.Bind(context.Background(), echoExecution())
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if !strings.HasSuffix(b.URL, "/"+b.Token) {
		t.Fatalf("token missing from URL %q", b.URL)
	}
	if status, _ := post(t, b, "", "", call); status != http.StatusOK {
		t.Fatalf("path token = %d", status)
	}
	wrong := Binding{URL: strings.TrimSuffix(b.URL, b.Token) + header.Token}
	if status, _ := post(t, wrong, b.Token, "", call); status != http.StatusUnauthorized {
		t.Fatalf("header cannot replace a path token: %d", status)
	}
	if status, _ := post(t, Binding{URL: header.URL + "/" + header.Token}, "", "", call); status != http.StatusUnauthorized {
		t.Fatalf("header route accepted a path token: %d", status)
	}
	if status, _ := post(t, b, "", "https://evil.example", call); status != http.StatusForbidden {
		t.Fatalf("browser origin = %d", status)
	}
	s.Release(b)
	if status, _ := post(t, b, "", "", call); status != http.StatusUnauthorized {
		t.Fatalf("released route = %d", status)
	}
}

func TestServerSpeaksMCP(t *testing.T) {
	t.Parallel()

	s := startServer(t)
	b, _ := s.Bind(context.Background(), echoExecution())
	_, init := post(t, b, b.Token, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	result := init["result"].(map[string]any)
	if result["protocolVersion"] != "2025-03-26" || result["instructions"] != "test instructions" || result["serverInfo"].(map[string]any)["name"] != Name {
		t.Fatalf("initialize = %v", result)
	}
	if _, unknown := post(t, b, b.Token, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`); unknown["result"].(map[string]any)["protocolVersion"] != protocolVersions[0] {
		t.Fatalf("unknown version answer = %v", unknown)
	}
	_, list := post(t, b, b.Token, "", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := list["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 3 || tools[0].(map[string]any)["inputSchema"] == nil {
		t.Fatalf("tools = %v", tools)
	}

	_, echo := post(t, b, b.Token, "", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"x":1}}}`)
	if text := echo["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; text != `{"x":1}` {
		t.Fatalf("echo = %v", echo)
	}
	_, fail := post(t, b, b.Token, "", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fail"}}`)
	if r := fail["result"].(map[string]any); r["isError"] != true || !strings.Contains(r["content"].([]any)[0].(map[string]any)["text"].(string), "boom") {
		t.Fatalf("fail = %v", fail)
	}
	_, pic := post(t, b, b.Token, "", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"picture"}}`)
	if content := pic["result"].(map[string]any)["content"].([]any); len(content) != 2 || content[1].(map[string]any)["type"] != "image" {
		t.Fatalf("picture = %v", pic)
	}
	_, unknown := post(t, b, b.Token, "", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nope"}}`)
	if unknown["error"] == nil {
		t.Fatalf("unknown tool = %v", unknown)
	}
}

func TestServerReportsEndedExecutions(t *testing.T) {
	t.Parallel()

	s := startServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	b, _ := s.Bind(ctx, Execution{Tools: []Tool{{Name: "wait", Call: func(ctx context.Context, _ json.RawMessage) (Result, error) {
		close(release)
		<-ctx.Done()
		return Result{}, ctx.Err()
	}}}})
	done := make(chan map[string]any, 1)
	go func() {
		_, out := post(t, b, b.Token, "", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wait"}}`)
		done <- out
	}()
	<-release
	cancel()
	out := <-done
	r := out["result"].(map[string]any)
	if r["isError"] != true || r["content"].([]any)[0].(map[string]any)["text"] != "This Multica execution has ended" {
		t.Fatalf("ended call = %v", out)
	}
}

func TestBindRejectsDuplicateToolNames(t *testing.T) {
	t.Parallel()

	s := startServer(t)
	tool := Tool{Name: "dup", Call: func(context.Context, json.RawMessage) (Result, error) { return Result{}, nil }}
	if _, err := s.Bind(context.Background(), Execution{Tools: []Tool{tool, tool}}); err == nil {
		t.Fatal("duplicate tools accepted")
	}
}
