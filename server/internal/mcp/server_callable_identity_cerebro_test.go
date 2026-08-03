package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestServerStampsExactCallableIdentityOnEveryDispatch(t *testing.T) {
	server := NewServer("test", "v1")
	seen := make([]string, 0, 2)
	server.RegisterTool(Tool{Name: "create_workflow"}, func(ctx context.Context, _ map[string]any) (CallToolResult, error) {
		seen = append(seen, CallableIdentity(ctx))
		return TextResult("ok"), nil
	})

	if _, err := server.Call(context.Background(), "create_workflow", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	params, _ := json.Marshal(CallToolParams{Name: "create_workflow"})
	server.handleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})

	if len(seen) != 2 || seen[0] != "create_workflow" || seen[1] != "create_workflow" {
		t.Fatalf("callable identities = %v, want exact identity on direct and JSON-RPC dispatch", seen)
	}
}
