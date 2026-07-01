package runtime

// FIR-1449 spike proof. End-to-end, no DB: it wires a representative CLI MCP
// tool (search_issues — a tool the native gateway builtin set LACKS) through
// the generic bridge and proves:
//
//  1. A bridged CLI tool is callable through the gateway Tool interface.
//  2. The calling agent's identity reaches the server unchanged (auth headers).
//  3. The in-process loopback transport carries the real REST request/response.
//  4. The admin denylist keeps an access-control write tool (create_group) off
//     the in-app runtime even though it is registered on the bridged server.
//
// The search_issues handler registered here is a faithful copy of the real one
// in cmd/multica/cmd_mcp_tools.go (same name, schema, and GET /api/issues/search
// call), so the proof exercises the exact bridge path production would use once
// registerTools is lifted into an importable package.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/clitools"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerSpikeSearchIssues mirrors the production search_issues MCP tool: it
// calls GET /api/issues/search?q=...&limit=... through the (in-process) client.
func registerSpikeSearchIssues(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name:        "search_issues",
		Description: "Search issues by text query across titles and descriptions.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return mcp.ErrorResult("query is required"), nil
		}
		var results any
		if err := client.GetJSON(ctx, "/api/issues/search?q="+query+"&limit=20", &results); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		raw, err := json.Marshal(results)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return mcp.TextResult(string(raw)), nil
	})
}

// registerSpikeCreateGroup stands in for an access-control MUTATION tool that
// the bridge must refuse to expose on the in-app runtime.
func registerSpikeCreateGroup(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name:        "create_group",
		Description: "Create an access-control group (admin).",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult("{}"), nil
	})
}

func TestBridge_SearchIssuesEndToEnd(t *testing.T) {
	const (
		wantToken = "task-token-xyz"
		wantAgent = "agent-uuid-123"
		wantWS    = "ws-uuid-456"
		wantTask  = "task-uuid-789"
	)

	var sawAuth, sawAgent, sawWS, sawPath, sawQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawAgent = r.Header.Get("X-Agent-ID")
		sawWS = r.Header.Get("X-Workspace-ID")
		sawPath = r.URL.Path
		sawQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"identifier":"FIR-1449","title":"Map og vælg"}]`)
	})

	client := InProcessAPIClient(handler, wantWS, wantAgent, wantToken, wantTask)

	srv := mcp.NewServer("bridge-spike", "0.1.0")
	registerSpikeSearchIssues(srv, client)
	registerSpikeCreateGroup(srv, client)

	reg := NewRegistry(nil)
	registerBuiltinTools(reg, nil, nil, ToolContext{}) // the native gateway set (no search_issues)

	if reg.hasTool("search_issues") {
		t.Fatal("precondition failed: native builtins already include search_issues; spike would be meaningless")
	}

	n := RegisterBridgedMCPTools(reg, srv, DefaultInAppAdminDenylist(), true)
	if n != 1 {
		t.Fatalf("expected exactly 1 bridged tool registered (search_issues), got %d", n)
	}

	// (4) admin write tool must NOT have been bridged.
	if reg.hasTool("create_group") {
		t.Fatal("admin tool create_group was bridged onto the in-app runtime — denylist failed")
	}
	// (1) the gap tool must now be callable.
	if !reg.hasTool("search_issues") {
		t.Fatal("search_issues was not bridged into the gateway registry")
	}

	// (1)+(2)+(3) call it end-to-end through the gateway Registry.
	out, err := reg.Call(context.Background(), "search_issues", map[string]any{"query": "tools"})
	if err != nil {
		t.Fatalf("search_issues call failed: %v", err)
	}
	if !strings.Contains(out, "FIR-1449") || !strings.Contains(out, "Map og vælg") {
		t.Fatalf("bridged result did not carry the REST payload: %q", out)
	}

	// Identity + routing assertions: the agent's identity reached the server
	// unchanged, and the request hit the real search endpoint with the query.
	if sawAuth != "Bearer "+wantToken {
		t.Errorf("Authorization header = %q, want Bearer %s", sawAuth, wantToken)
	}
	if sawAgent != wantAgent {
		t.Errorf("X-Agent-ID = %q, want %q", sawAgent, wantAgent)
	}
	if sawWS != wantWS {
		t.Errorf("X-Workspace-ID = %q, want %q", sawWS, wantWS)
	}
	if sawPath != "/api/issues/search" {
		t.Errorf("request path = %q, want /api/issues/search", sawPath)
	}
	if sawQuery != "tools" {
		t.Errorf("search query q = %q, want tools", sawQuery)
	}
}

func TestBridge_AttachmentReadToolsEndToEnd(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/issue-with-file/attachments":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"att-1","filename":"test-attachment.csv","content_type":"text/csv","size_bytes":14}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/attachments/att-1/content":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, "sku,qty\nABC,2\n")
		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, http.StatusNotImplemented)
		}
	})

	client := InProcessAPIClient(handler, "ws-uuid-456", "agent-uuid-123", "task-token-xyz", "task-uuid-789")
	srv := mcp.NewServer("multica", "test")
	var session clitools.SessionState
	clitools.RegisterTools(srv, client, &session, "ws-uuid-456", "proj-uuid-123", "")

	reg := NewRegistry(nil)
	registerBuiltinTools(reg, nil, nil, ToolContext{})
	RegisterBridgedMCPTools(reg, srv, DefaultInAppAdminDenylist(), true)

	if !reg.hasTool("list_attachments") {
		t.Fatal("list_attachments was not bridged into the gateway registry")
	}
	if !reg.hasTool("read_attachment") {
		t.Fatal("read_attachment was not bridged into the gateway registry")
	}

	listed, err := reg.Call(context.Background(), "list_attachments", map[string]any{"issue_id": "issue-with-file"})
	if err != nil {
		t.Fatalf("list_attachments call failed: %v", err)
	}
	if !strings.Contains(listed, "test-attachment.csv") || !strings.Contains(listed, "att-1") {
		t.Fatalf("list_attachments result missing attachment metadata: %q", listed)
	}

	text, err := reg.Call(context.Background(), "read_attachment", map[string]any{"attachment_id": "att-1"})
	if err != nil {
		t.Fatalf("read_attachment call failed: %v", err)
	}
	if text != "sku,qty\nABC,2\n" {
		t.Fatalf("read_attachment text = %q, want CSV text", text)
	}
}

// TestBridge_AdminDenylistShape guards the product boundary: read tools stay
// allowed, write/admin tools stay denied.
func TestBridge_AdminDenylistShape(t *testing.T) {
	deny := DefaultInAppAdminDenylist()

	mustDeny := []string{
		"create_group", "delete_group", "add_group_member",
		"create_grant", "update_grant", "credential_policy_set",
		"skill_propose_change", "skill_review_change_request", "skill_fork",
		"attach_session", "bind_repo",
	}
	for _, name := range mustDeny {
		if !deny[name] {
			t.Errorf("expected %q to be denied on the in-app runtime", name)
		}
	}

	mustAllow := []string{
		"search_issues", "skill_list", "skill_get", "list_groups",
		"create_artifact", "schedule_wakeup", "list_wakeups",
		"add_attachment", "list_attachments", "read_attachment",
	}
	for _, name := range mustAllow {
		if deny[name] {
			t.Errorf("expected %q to be allowed (read / normal work), but it is denied", name)
		}
	}
}
