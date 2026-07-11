// CEREBRO-PATCH(cerebro-workflow-cli-management): FIR-2937 workflow CLI contract tests.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

func TestWorkflowCreateReadsJSONFileAndPosts(t *testing.T) {
	var method, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "wf-1"})
	}))
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "workflow.json")
	if err := os.WriteFile(file, []byte(`{"name":"Claude x Codex","workflow_type":"issue_loop","loop_spec":{"goal":"Ship"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resetWorkflowFlags(t, workflowCreateCmd, "file", "stdin", "output")
	_ = workflowCreateCmd.Flags().Set("file", file)

	withCLIEnv(t, srv.URL, "ws-1", func() {
		if err := runWorkflowCreate(workflowCreateCmd, nil); err != nil {
			t.Fatalf("runWorkflowCreate: %v", err)
		}
	})
	if method != http.MethodPost || path != "/api/cerebro/workflows" {
		t.Fatalf("request = %s %s", method, path)
	}
	if body["workflow_type"] != "issue_loop" || body["name"] != "Claude x Codex" {
		t.Fatalf("body = %#v", body)
	}
}

func TestWorkflowActivatePostsIssueID(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if r.Method != http.MethodPost || r.URL.Path != "/api/cerebro/workflows/wf-1/activate" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"activated": true})
	}))
	defer srv.Close()
	resetWorkflowFlags(t, workflowActivateCmd, "output")

	withCLIEnv(t, srv.URL, "ws-1", func() {
		if err := runWorkflowActivate(workflowActivateCmd, []string{"wf-1", "FIR-123"}); err != nil {
			t.Fatalf("runWorkflowActivate: %v", err)
		}
	})
	if body["issue_id"] != "FIR-123" {
		t.Fatalf("body = %#v", body)
	}
}

func resetWorkflowFlags(t *testing.T, cmd interface{ Flags() *pflag.FlagSet }, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range names {
			_ = cmd.Flags().Set(name, "")
			if f := cmd.Flags().Lookup(name); f != nil {
				f.Changed = false
			}
		}
	})
}
