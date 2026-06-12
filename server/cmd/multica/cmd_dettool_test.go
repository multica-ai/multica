package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func newDettoolImportFileTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "import-file"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().Bool("enabled", true, "")
	cmd.Flags().Bool("update-existing", true, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestRunDettoolImportFileCreatesToolFromSourceFile(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/deterministic-tools" {
			t.Fatalf("path = %q, want /api/deterministic-tools", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "tool-123",
			"name":    body["name"],
			"source":  body["source"],
			"enabled": body["enabled"],
		})
	}))
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	source := "package step\n\nfunc Run(input map[string]any) map[string]any { return map[string]any{\"status\":\"ok\"} }\n"
	path := filepath.Join(t.TempDir(), "file-name.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	cmd := newDettoolImportFileTestCmd()
	if _, err := captureStdout(t, func() error { return runDettoolImportFile(cmd, []string{path}) }); err != nil {
		t.Fatalf("runDettoolImportFile: %v", err)
	}
	if body["name"] != "file_name" {
		t.Fatalf("name = %v, want file_name", body["name"])
	}
	if body["source"] != source {
		t.Fatalf("source = %q, want verbatim %q", body["source"], source)
	}
	if body["enabled"] != true {
		t.Fatalf("enabled = %v, want true", body["enabled"])
	}
}

func TestRunDettoolImportFileUpdatesExistingToolOnConflict(t *testing.T) {
	var updateBody map[string]any
	requests := make([]string, 0, 3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/deterministic-tools":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "a deterministic tool with this name already exists"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/deterministic-tools":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id":   "tool-123",
				"name": "existing_tool",
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/deterministic-tools/tool-123":
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "tool-123",
				"name":   "existing_tool",
				"source": updateBody["source"],
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	source := "package step\n\nfunc Run(input map[string]any) map[string]any { return input }\n"
	path := filepath.Join(t.TempDir(), "existing_tool.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	cmd := newDettoolImportFileTestCmd()
	if _, err := captureStdout(t, func() error { return runDettoolImportFile(cmd, []string{path}) }); err != nil {
		t.Fatalf("runDettoolImportFile: %v", err)
	}
	if len(requests) != 3 {
		t.Fatalf("requests = %#v, want post/get/put", requests)
	}
	if updateBody["source"] != source {
		t.Fatalf("update source = %q, want verbatim %q", updateBody["source"], source)
	}
	if _, ok := updateBody["enabled"]; ok {
		t.Fatalf("update body should not include enabled unless flag changed: %#v", updateBody)
	}
}
