package clitools

// FIR-2388 — the status tool must ALWAYS be registered so a zero-connection-tool
// outcome is never silent: whether the list call errors, returns nothing, or
// returns tools, an agent can call multica_connection_tools_status and learn why.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func hasTool(srv *mcp.Server, name string) bool {
	for _, t := range srv.Tools() {
		if t.Name == name {
			return true
		}
	}
	return false
}

func statusText(t *testing.T, srv *mcp.Server) string {
	t.Helper()
	res, err := srv.Call(context.Background(), connectionToolsStatusToolName, nil)
	if err != nil {
		t.Fatalf("status tool call errored: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("status tool returned no content")
	}
	return res.Content[0].Text
}

// The list call fails (unreachable API): the status tool is still registered and
// reports "were not loaded".
func TestRegisterConnectionToolsAlwaysRegistersStatusTool_OnError(t *testing.T) {
	srv := mcp.NewServer("test", "0")
	client := cli.NewAPIClient("", "", "") // empty base URL → GetJSON errors

	registerConnectionTools(srv, client)

	if !hasTool(srv, connectionToolsStatusToolName) {
		t.Fatalf("status tool must be registered even when discovery fails")
	}
	if msg := statusText(t, srv); !strings.Contains(msg, "were not loaded") {
		t.Fatalf("want a not-loaded message on error, got %q", msg)
	}
}

// The list call succeeds but returns no tools: the status tool reports 0 with a
// diagnostic hint.
func TestRegisterConnectionToolsAlwaysRegistersStatusTool_Empty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tools":[]}`))
	}))
	defer ts.Close()
	srv := mcp.NewServer("test", "0")

	registerConnectionTools(srv, cli.NewAPIClient(ts.URL, "", ""))

	if !hasTool(srv, connectionToolsStatusToolName) {
		t.Fatalf("status tool must be registered when the list is empty")
	}
	if msg := statusText(t, srv); !strings.Contains(msg, "loaded: 0") {
		t.Fatalf("want a loaded-0 message, got %q", msg)
	}
}

// The list call returns tools: each endpoint tool AND the status tool are
// registered, and the status reports the count.
func TestRegisterConnectionToolsAlwaysRegistersStatusTool_WithTools(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tools":[{"name":"c__get_x","description":"d","input_schema":{"type":"object"}}]}`))
	}))
	defer ts.Close()
	srv := mcp.NewServer("test", "0")

	registerConnectionTools(srv, cli.NewAPIClient(ts.URL, "", ""))

	if !hasTool(srv, "c__get_x") {
		t.Fatalf("endpoint tool c__get_x must be registered")
	}
	if !hasTool(srv, connectionToolsStatusToolName) {
		t.Fatalf("status tool must be registered alongside endpoint tools")
	}
	if msg := statusText(t, srv); !strings.Contains(msg, "loaded: 1") {
		t.Fatalf("want a loaded-1 message, got %q", msg)
	}
}
