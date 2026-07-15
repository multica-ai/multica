package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowHookCreateUsesCanonicalAPI(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hook.json")
	if err := os.WriteFile(file, []byte(`{"name":"Require continuation"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resetWorkflowFlags(t, workflowHookCreateCmd, "file", "stdin", "output")
	_ = workflowHookCreateCmd.Flags().Set("file", file)
	method, path, body := withWorkflowHookServer(t, func() {
		if err := runWorkflowHookCreate(workflowHookCreateCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if method != http.MethodPost || path != "/api/cerebro/workflow-hooks" || body["name"] != "Require continuation" {
		t.Fatalf("request = %s %s %#v", method, path, body)
	}
}

func TestWorkflowHookPublishUsesCanonicalAPI(t *testing.T) {
	method, path, _ := withWorkflowHookServer(t, func() {
		if err := runWorkflowHookPublish(workflowHookPublishCmd, []string{"hook-1"}); err != nil {
			t.Fatal(err)
		}
	})
	if method != http.MethodPost || path != "/api/cerebro/workflow-hooks/hook-1/publish" {
		t.Fatalf("request = %s %s", method, path)
	}
}

func withWorkflowHookServer(t *testing.T, run func()) (method, path string, body map[string]any) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "hook-1"})
	}))
	defer srv.Close()
	withCLIEnv(t, srv.URL, "ws-1", run)
	return method, path, body
}
