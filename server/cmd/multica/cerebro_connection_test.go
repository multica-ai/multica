// CEREBRO-PATCH(cerebro-connections-cli-test): FIR-2835 cerebro-only file — connection CLI tests.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	cerebroConnTestWorkspaceID = "ws-conn-1"
	cerebroConnTestConnID      = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
)

// newCerebroConnectionTestServer stands in for the cerebro connections API. It
// records every request and serves the minimal routes the CLI hits, so tests
// assert on the request shape the CLI produces — not on server behavior.
func newCerebroConnectionTestServer() (*httptest.Server, *requestRecorder) {
	rec := &requestRecorder{}
	existing := map[string]any{
		"id":                   cerebroConnTestConnID,
		"workspace_id":         cerebroConnTestWorkspaceID,
		"name":                 "finance-mcp",
		"display_name":         "Finance MCP",
		"type":                 "mcp_http",
		"url":                  "http://firtal-agents-private.internal:3000/api/mcp",
		"internal":             true,
		"enabled":              true,
		"default_access":       "deny",
		"auth_config":          map[string]any{"bearer_token": "***"},
		"endpoint_permissions": []any{},
		"scopable_args":        []any{},
	}

	base := "/api/workspaces/" + cerebroConnTestWorkspaceID + "/connections"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
		}
		rec.requests = append(rec.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: body})

		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			_ = json.NewEncoder(w).Encode(map[string]any{"connections": []map[string]any{existing}})
		case r.Method == http.MethodGet && r.URL.Path == base+"/"+cerebroConnTestConnID:
			_ = json.NewEncoder(w).Encode(existing)
		case r.Method == http.MethodPost && r.URL.Path == base:
			w.WriteHeader(http.StatusCreated)
			out := map[string]any{"id": cerebroConnTestConnID}
			for k, v := range body {
				out[k] = v
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPut && r.URL.Path == base+"/"+cerebroConnTestConnID:
			out := map[string]any{"id": cerebroConnTestConnID}
			for k, v := range body {
				out[k] = v
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && r.URL.Path == base+"/test":
			_ = json.NewEncoder(w).Encode(map[string]any{"reachable": true})
		case r.Method == http.MethodDelete && r.URL.Path == base+"/"+cerebroConnTestConnID:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv, rec
}

// TestConnectionCreateInternal verifies the create path sends an internal-path
// MCP connection with the bearer token — the exact shape Jesper asked for.
func TestConnectionCreateInternal(t *testing.T) {
	srv, rec := newCerebroConnectionTestServer()
	defer srv.Close()

	t.Cleanup(func() {
		for _, n := range []string{"name", "display-name", "url", "type", "internal", "auth-token", "default-access", "output"} {
			_ = connectionCreateCmd.Flags().Set(n, "")
			if f := connectionCreateCmd.Flags().Lookup(n); f != nil {
				f.Changed = false
			}
		}
	})

	set := func(k, v string) {
		if err := connectionCreateCmd.Flags().Set(k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
	set("name", "finance-mcp")
	set("display-name", "Finance MCP")
	set("type", "mcp_http")
	set("url", "http://firtal-agents-private.internal:3000/api/mcp")
	set("internal", "true")
	set("auth-token", "sekret")
	set("output", "json")

	withCLIEnv(t, srv.URL, cerebroConnTestWorkspaceID, func() {
		if err := runConnectionCreate(connectionCreateCmd, nil); err != nil {
			t.Fatalf("runConnectionCreate: %v", err)
		}
	})

	req, ok := findRequest(rec, http.MethodPost, "/api/workspaces/"+cerebroConnTestWorkspaceID+"/connections")
	if !ok {
		t.Fatal("expected POST to /connections")
	}
	if req.Body["name"] != "finance-mcp" {
		t.Errorf("name = %v", req.Body["name"])
	}
	if req.Body["type"] != "mcp_http" {
		t.Errorf("type = %v", req.Body["type"])
	}
	if req.Body["internal"] != true {
		t.Errorf("internal = %v, want true", req.Body["internal"])
	}
	auth, ok := req.Body["auth_config"].(map[string]any)
	if !ok || auth["bearer_token"] != "sekret" {
		t.Errorf("auth_config = %v, want bearer_token=sekret", req.Body["auth_config"])
	}
}

// TestConnectionCreateRequiresURL — an unset --url must error before any HTTP call.
func TestConnectionCreateRequiresURL(t *testing.T) {
	srv, rec := newCerebroConnectionTestServer()
	defer srv.Close()

	t.Cleanup(func() {
		for _, n := range []string{"name", "url"} {
			_ = connectionCreateCmd.Flags().Set(n, "")
			if f := connectionCreateCmd.Flags().Lookup(n); f != nil {
				f.Changed = false
			}
		}
	})
	if err := connectionCreateCmd.Flags().Set("name", "x"); err != nil {
		t.Fatal(err)
	}

	withCLIEnv(t, srv.URL, cerebroConnTestWorkspaceID, func() {
		err := runConnectionCreate(connectionCreateCmd, nil)
		if err == nil || !strings.Contains(err.Error(), "--url is required") {
			t.Fatalf("expected --url required error, got: %v", err)
		}
	})
	if len(rec.requests) != 0 {
		t.Errorf("expected zero HTTP requests, got %d", len(rec.requests))
	}
}

// TestConnectionUpdateByNameMergesStored verifies update resolves a connection
// by name, preserves stored fields, and only overrides what the caller changed.
func TestConnectionUpdateByNameMergesStored(t *testing.T) {
	srv, rec := newCerebroConnectionTestServer()
	defer srv.Close()

	t.Cleanup(func() {
		for _, n := range []string{"display-name", "url", "internal", "enabled", "auth-token", "default-access", "output"} {
			_ = connectionUpdateCmd.Flags().Set(n, "")
			if f := connectionUpdateCmd.Flags().Lookup(n); f != nil {
				f.Changed = false
			}
		}
	})
	if err := connectionUpdateCmd.Flags().Set("display-name", "Finance MCP v2"); err != nil {
		t.Fatal(err)
	}

	withCLIEnv(t, srv.URL, cerebroConnTestWorkspaceID, func() {
		if err := runConnectionUpdate(connectionUpdateCmd, []string{"finance-mcp"}); err != nil {
			t.Fatalf("runConnectionUpdate: %v", err)
		}
	})

	req, ok := findRequest(rec, http.MethodPut, "/api/workspaces/"+cerebroConnTestWorkspaceID+"/connections/"+cerebroConnTestConnID)
	if !ok {
		t.Fatal("expected PUT to /connections/{id}")
	}
	if req.Body["display_name"] != "Finance MCP v2" {
		t.Errorf("display_name = %v, want overridden", req.Body["display_name"])
	}
	// Untouched fields keep their stored value.
	if req.Body["url"] != "http://firtal-agents-private.internal:3000/api/mcp" {
		t.Errorf("url = %v, want stored value preserved", req.Body["url"])
	}
	if req.Body["internal"] != true {
		t.Errorf("internal = %v, want stored true preserved", req.Body["internal"])
	}
}
