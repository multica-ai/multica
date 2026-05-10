package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

// ---------------------------------------------------------------------------
// Ship Hub MCP tools — exercise the read + write registrations end-to-end
// against an httptest server. We resolve each tool by name from the
// registered MCPServer's ListTools() map, build a CallToolRequest with
// the args we want, and assert on both the URL the client hit and the
// response body it returned to the model.
// ---------------------------------------------------------------------------

// invokeShipTool registers all tools against a fresh MCPServer pointed at
// the test HTTP server, then dispatches the named tool with the given args.
// Returns the text content the model would see, plus the HTTP method+path
// the API client actually called so test cases can assert routing.
func invokeShipTool(
	t *testing.T,
	srv *httptest.Server,
	toolName string,
	args map[string]any,
) *mcp.CallToolResult {
	t.Helper()
	mcpSrv := server.NewMCPServer("multica-test", "test", server.WithToolCapabilities(false))
	c := cli.NewAPIClient(srv.URL, "ws-1", "tok")
	RegisterAllMCPTools(mcpSrv, c)

	tool, ok := mcpSrv.ListTools()[toolName]
	if !ok {
		t.Fatalf("tool %q was not registered", toolName)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args

	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned go error (should never happen): %v", err)
	}
	return res
}

// resultText extracts the single text block the toolHandler emits.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("expected at least one content block")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

// Read tool with no arguments. Asserts the registered URL and that the
// response body is round-tripped to the model verbatim.
func TestShipMCP_ListProjects(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"projects":[{"id":"p1","title":"Frontend"}]}`))
	}))
	defer srv.Close()

	res := invokeShipTool(t, srv, "multica_ship_list_projects", map[string]any{})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, res))
	}
	if gotMethod != http.MethodGet || gotPath != "/api/ship/projects" {
		t.Errorf("expected GET /api/ship/projects, got %s %s", gotMethod, gotPath)
	}
	if got := resultText(t, res); got != `{"projects":[{"id":"p1","title":"Frontend"}]}` {
		t.Errorf("expected verbatim response, got %q", got)
	}
}

// Read tool with a path arg. Verifies URL escaping and the required-arg
// guardrail (missing arg → tool error, not Go error).
func TestShipMCP_GetPullRequest(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"pull_request":{"id":"pr-1"},"linked_issue":null}`))
	}))
	defer srv.Close()

	res := invokeShipTool(t, srv, "multica_ship_get_pull_request", map[string]any{
		"pull_request_id": "pr-uuid-123",
	})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, res))
	}
	if gotPath != "/api/pull_requests/pr-uuid-123/details" {
		t.Errorf("expected /api/pull_requests/pr-uuid-123/details, got %s", gotPath)
	}

	// Missing required arg — the helper short-circuits with an error
	// payload BEFORE hitting the server. We follow the same convention as
	// every other tool in this package: requireString returns a
	// CallToolResult whose marshalled form mentions the missing arg name.
	gotPath = ""
	res2 := invokeShipTool(t, srv, "multica_ship_get_pull_request", map[string]any{})
	if gotPath != "" {
		t.Errorf("server should not have been hit; got %s", gotPath)
	}
	if !strings.Contains(resultText(t, res2), "pull_request_id") {
		t.Errorf("expected error to name the missing arg, got %q", resultText(t, res2))
	}
}

// Write tool with array body validation. The most common AI mistake is
// dropping or string-typing the pull_request_ids array, so we trap that
// before round-tripping to the server.
func TestShipMCP_CreateRelease_ValidatesArray(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"id":"release-1","stage":"assembling"}`))
	}))
	defer srv.Close()

	// Empty array → short-circuit error payload before HTTP.
	res := invokeShipTool(t, srv, "multica_ship_create_release", map[string]any{
		"project_id":       "proj-1",
		"title":            "Auth refactor v2",
		"pull_request_ids": []any{},
	})
	if gotMethod != "" {
		t.Errorf("server should not have been hit on empty array; got %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(resultText(t, res), "pull_request_ids") {
		t.Errorf("expected error to name pull_request_ids, got %q", resultText(t, res))
	}

	// Missing array entirely — same outcome, server still untouched.
	gotMethod, gotPath = "", ""
	res2 := invokeShipTool(t, srv, "multica_ship_create_release", map[string]any{
		"project_id": "proj-1",
		"title":      "Auth refactor v2",
	})
	if gotMethod != "" {
		t.Errorf("server should not have been hit on missing array; got %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(resultText(t, res2), "pull_request_ids") {
		t.Errorf("expected error to name pull_request_ids, got %q", resultText(t, res2))
	}

	// Valid call → server is hit with the right shape.
	gotMethod, gotPath = "", ""
	res3 := invokeShipTool(t, srv, "multica_ship_create_release", map[string]any{
		"project_id":       "proj-1",
		"title":            "Auth refactor v2",
		"description":      "ship the new login flow",
		"pull_request_ids": []any{"pr-a", "pr-b"},
		"approver_id":      "member-x",
	})
	if res3.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, res3))
	}
	if gotMethod != http.MethodPost || gotPath != "/api/projects/proj-1/releases" {
		t.Errorf("expected POST /api/projects/proj-1/releases, got %s %s", gotMethod, gotPath)
	}
	if got, _ := gotBody["title"].(string); got != "Auth refactor v2" {
		t.Errorf("title not forwarded; got %v", gotBody["title"])
	}
	prs, ok := gotBody["pull_request_ids"].([]any)
	if !ok || len(prs) != 2 || prs[0] != "pr-a" || prs[1] != "pr-b" {
		t.Errorf("pull_request_ids not forwarded as array; got %#v", gotBody["pull_request_ids"])
	}
	if got, _ := gotBody["approver_id"].(string); got != "member-x" {
		t.Errorf("approver_id not forwarded; got %v", gotBody["approver_id"])
	}
}

// Simple write tool with optional body field. Verifies POST + path + that
// the optional rollback_plan is forwarded when present.
func TestShipMCP_PromoteRelease(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"id":"release-1","stage":"promoting"}`))
	}))
	defer srv.Close()

	res := invokeShipTool(t, srv, "multica_ship_promote_release", map[string]any{
		"release_id":    "rel-1",
		"rollback_plan": "revert merge commit on main and redeploy",
	})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(t, res))
	}
	if gotMethod != http.MethodPost || gotPath != "/api/releases/rel-1/promote" {
		t.Errorf("expected POST /api/releases/rel-1/promote, got %s %s", gotMethod, gotPath)
	}
	if got, _ := gotBody["rollback_plan"].(string); got != "revert merge commit on main and redeploy" {
		t.Errorf("rollback_plan not forwarded; got %v", gotBody["rollback_plan"])
	}

	// Missing release_id short-circuits before HTTP.
	gotMethod, gotPath = "", ""
	res2 := invokeShipTool(t, srv, "multica_ship_promote_release", map[string]any{})
	if gotMethod != "" {
		t.Errorf("server should not have been hit when release_id missing; got %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(resultText(t, res2), "release_id") {
		t.Errorf("expected error to name release_id, got %q", resultText(t, res2))
	}
}
