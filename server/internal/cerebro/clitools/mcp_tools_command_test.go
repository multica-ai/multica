package clitools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func TestRegisterCommandToolsRegistersSharedSurface(t *testing.T) {
	srv := mcp.NewServer("test", "0")
	registerCommandTools(srv, cli.NewAPIClient("", "", ""))
	for _, name := range []string{"list_commands", "get_command", "create_command", "update_command", "delete_command"} {
		if !hasTool(srv, name) {
			t.Errorf("expected tool %q", name)
		}
	}
}

func TestUpdateCommandUsesCommandRouteAndBody(t *testing.T) {
	var body map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/cerebro/commands/command-1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "command-1"})
	}))
	defer ts.Close()

	srv := mcp.NewServer("test", "0")
	registerCommandTools(srv, cli.NewAPIClient(ts.URL, "ws-1", "token"))
	result, err := srv.Call(context.Background(), "update_command", map[string]any{
		"command_id": "command-1",
		"command":    map[string]any{"key": "checks", "title": "Run checks", "argv": []any{"make", "check"}},
	})
	if err != nil || result.IsError {
		t.Fatalf("call failed: result=%#v err=%v", result, err)
	}
	if body["key"] != "checks" {
		t.Fatalf("body = %#v", body)
	}
}
