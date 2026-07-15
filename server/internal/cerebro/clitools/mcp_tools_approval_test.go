package clitools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func TestRequestApprovalProxiesWithServerAuthoritativeTaskTokenIdentity(t *testing.T) {
	t.Helper()

	var gotAuthorization string
	var gotToolName string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		var request struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode proxied request: %v", err)
		}
		gotToolName = request.Params.Name
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"id\":\"approval-1\",\"status\":\"pending\"}"}]}}`))
	}))
	defer remote.Close()

	client := cli.NewAPIClient(remote.URL, "workspace-1", "mat_task-token")
	server := mcp.NewServer("test", "1")
	registerApprovalTools(server, client)

	result, err := server.Call(context.Background(), "request_approval", map[string]any{
		"capability": "create_issue",
		"reason":     "Needs an owner decision",
	})
	if err != nil {
		t.Fatalf("request_approval returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("request_approval returned tool error: %+v", result.Content)
	}
	if gotAuthorization != "Bearer mat_task-token" {
		t.Fatalf("Authorization = %q, want task token", gotAuthorization)
	}
	if gotToolName != "request_approval" {
		t.Fatalf("proxied tool = %q, want request_approval", gotToolName)
	}
}
