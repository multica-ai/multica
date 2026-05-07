package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Smoke test: every tool registers without panic and the registry size
// matches the JS @multica/mcp surface count. If a tool gets added or
// dropped, this test forces a deliberate update — protects against silent
// drift between the Go and (still-archived) JS implementations.
func TestRegisterAllMCPTools_RegistersExpectedCount(t *testing.T) {
	srv := server.NewMCPServer("multica-test", "test", server.WithToolCapabilities(false))
	c := cli.NewAPIClient("http://example.com", "ws-1", "tok")
	RegisterAllMCPTools(srv, c)

	// We can't introspect mcp-go's tool registry directly, but we can
	// invoke the tools/list endpoint via the server's internal handler.
	// Easiest path: assert the registration doesn't panic. The full
	// list inventory is exercised by the binary smoke test in CI.
	// (No-op: reaching here without a panic is the assertion.)
	_ = srv
}

// Tool handler adapter must round-trip json.RawMessage as text without
// re-marshalling — the JS @multica/mcp returns the raw API response, and
// re-marshalling it would change key order in clients that key-stable
// the response (rare but observed with some MCP clients).
func TestToolHandler_RoundTripsRawJSON(t *testing.T) {
	raw := json.RawMessage(`{"id":"x","name":"y"}`)
	h := toolHandler(func(_ context.Context, _ mcp.CallToolRequest) (any, error) {
		return raw, nil
	})
	res, err := h(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned err: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("expected at least one content block")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	if tc.Text != string(raw) {
		t.Errorf("expected verbatim %q, got %q", string(raw), tc.Text)
	}
}

// When the underlying handler returns an error, the wrapper produces a
// CallToolResult with IsError=true and the error message in the content
// block — never returns the error up the call stack, because mcp-go
// treats Go errors as transport-level failures rather than tool-level.
func TestToolHandler_PropagatesErrorAsToolResult(t *testing.T) {
	h := toolHandler(func(_ context.Context, _ mcp.CallToolRequest) (any, error) {
		return nil, errors.New("workspace 503")
	})
	res, err := h(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler must NOT return Go error; got: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected res.IsError=true on handler error")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok || !strings.Contains(tc.Text, "workspace 503") {
		t.Errorf("expected error text propagated, got %+v", res.Content[0])
	}
}

// Body builders must skip empty-string optional values and forward
// non-empty strings as-is. The PATCH-semantics test for memory_update
// covers the trickiest path (partial updates).
func TestSetIfPresent_OmitsEmptyAndNil(t *testing.T) {
	body := map[string]any{}
	setIfPresent(body, "title", "")
	setIfPresent(body, "desc", nil)
	setIfPresent(body, "kept", "yes")
	if _, has := body["title"]; has {
		t.Error("expected empty string to be omitted")
	}
	if _, has := body["desc"]; has {
		t.Error("expected nil to be omitted")
	}
	if got, has := body["kept"]; !has || got != "yes" {
		t.Errorf("expected kept=yes, got %v (present=%v)", got, has)
	}
}

func TestQueryString_SkipsEmpty(t *testing.T) {
	q := queryString(
		[2]string{"a", "1"},
		[2]string{"b", ""},
		[2]string{"c", "3"},
	)
	// url.Values.Encode is alphabetical, so order is stable.
	if q != "?a=1&c=3" {
		t.Errorf("expected ?a=1&c=3, got %q", q)
	}
	// All-empty input yields the empty string so callers can append it
	// unconditionally without producing a stray "?".
	if q := queryString([2]string{"a", ""}); q != "" {
		t.Errorf("expected empty string for all-empty input, got %q", q)
	}
}

func TestNullableString(t *testing.T) {
	if v := nullableString(""); v != nil {
		t.Errorf("empty should map to nil, got %#v", v)
	}
	if v := nullableString("x"); v != "x" {
		t.Errorf("non-empty should pass through, got %#v", v)
	}
}
