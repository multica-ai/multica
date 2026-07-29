package clitools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/issuepropertytools"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func TestRegisterIssuePropertyToolsRegistersSharedSurface(t *testing.T) {
	srv := mcp.NewServer("test", "0")
	registerIssuePropertyTools(srv, cli.NewAPIClient("", "", ""))
	for _, name := range []string{
		issuepropertytools.ListName,
		issuepropertytools.SetName,
		issuepropertytools.UnsetName,
	} {
		if !hasTool(srv, name) {
			t.Errorf("expected tool %q", name)
		}
	}
}

func TestIssuePropertyMCPToolsUseCanonicalIssueAndPropertyIDs(t *testing.T) {
	var methods []string
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/FIR-123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "issue-uuid",
				"properties": map[string]any{"property-uuid": "old"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/properties":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"properties": []map[string]any{{
					"id": "property-uuid", "name": "Severity", "type": "text",
				}},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/issue-uuid/properties/property-uuid":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"properties": map[string]any{"property-uuid": "high"},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/issues/issue-uuid/properties/property-uuid":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	srv := mcp.NewServer("test", "0")
	registerIssuePropertyTools(srv, cli.NewAPIClient(ts.URL, "workspace-1", "token"))

	setResult, err := srv.Call(context.Background(), issuepropertytools.SetName, map[string]any{
		"issue_id": "FIR-123",
		"property": "severity",
		"value":    "high",
	})
	if err != nil || setResult.IsError {
		t.Fatalf("set call failed: result=%#v err=%v", setResult, err)
	}
	unsetResult, err := srv.Call(context.Background(), issuepropertytools.UnsetName, map[string]any{
		"issue_id": "FIR-123",
		"property": "Severity",
	})
	if err != nil || unsetResult.IsError {
		t.Fatalf("unset call failed: result=%#v err=%v", unsetResult, err)
	}

	wantLast := "/api/issues/issue-uuid/properties/property-uuid"
	if len(paths) < 6 || paths[2] != wantLast || methods[2] != http.MethodPut ||
		paths[5] != wantLast || methods[5] != http.MethodDelete {
		t.Fatalf("requests = %v %v", methods, paths)
	}
}
