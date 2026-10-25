package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func reworkDispatchTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "dispatch-rework"}
	c.Flags().String("operation-key", "", "")
	c.Flags().String("agent-id", "", "")
	c.Flags().String("source-run", "", "")
	c.Flags().String("instruction", "", "")
	c.Flags().Bool("instruction-stdin", false, "")
	c.Flags().String("instruction-file", "", "")
	c.Flags().Bool("allow-external-file", false, "")
	c.Flags().String("output", "json", "")
	return c
}

func TestRunIssueDispatchReworkHTTPContract(t *testing.T) {
	var gotPath string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": testIssueUUID, "identifier": "MUL-1"})
			return
		}
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_ = json.NewEncoder(w).Encode(map[string]any{"replayed": false, "task": map[string]any{"id": "run-1", "status": "queued"}})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test")
	cmd := reworkDispatchTestCmd()
	_ = cmd.Flags().Set("operation-key", "pkg-v1")
	_ = cmd.Flags().Set("agent-id", "22222222-2222-2222-2222-222222222222")
	_ = cmd.Flags().Set("source-run", "33333333-3333-3333-3333-333333333333")
	_ = cmd.Flags().Set("instruction", "frozen package")
	if _, err := captureStdout(t, func() error { return runIssueDispatchRework(cmd, []string{testIssueUUID}) }); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/issues/"+testIssueUUID+"/rework-dispatch" {
		t.Fatalf("path %q", gotPath)
	}
	if got["operation_key"] != "pkg-v1" || got["instruction"] != "frozen package" || got["source_task_id"] != "33333333-3333-3333-3333-333333333333" || !strings.HasPrefix(got["payload_sha256"].(string), "") {
		t.Fatalf("body %#v", got)
	}
	if len(got["payload_sha256"].(string)) != 64 {
		t.Fatalf("digest %#v", got)
	}
}
