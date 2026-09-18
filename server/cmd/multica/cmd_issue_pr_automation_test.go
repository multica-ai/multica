package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestIssuePRAutomationResolvesIssueAndSendsExplicitFalse(t *testing.T) {
	t.Chdir(t.TempDir())
	var operations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operations = append(operations, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/api/issues/MUL-7429" {
			json.NewEncoder(w).Encode(map[string]string{"id": "issue-uuid", "identifier": "MUL-7429"})
			return
		}
		if r.URL.Path != "/api/issues/issue-uuid/pr-automation" {
			t.Error("wrong issue scope", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == "PUT" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body, map[string]any{"disabled": false}) {
				t.Error("lost explicit false", body)
			}
			io.WriteString(w, `{"ok":true}`)
			return
		}
		io.WriteString(w, `{"migrated":true,"policy":{"source":"title_branch","auto_complete":true},"issue":{"disabled":false,"links":[],"decision":{"reason":"waiting"}}}`)
	}))
	defer server.Close()
	t.Setenv("MULTICA_SERVER_URL", server.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws")
	t.Setenv("MULTICA_TOKEN", "test-token")
	cmd := &cobra.Command{}
	cmd.Flags().Bool("disabled", false, "")
	cmd.Flags().String("output", "json", "")
	for _, flag := range []string{"link", "exclude", "restore"} {
		cmd.Flags().String(flag, "", "")
	}
	cmd.Flags().Set("disabled", "false")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	old := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = old }()
	err = runIssuePRAutomation(cmd, []string{"MUL-7429"})
	writer.Close()
	os.Stdout = old
	body, _ := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal(body, &result) != nil || result["migrated"] != true {
		t.Fatal("missing resulting policy", string(body))
	}
	expected := []string{"GET /api/issues/MUL-7429", "PUT /api/issues/issue-uuid/pr-automation", "GET /api/issues/issue-uuid/pr-automation"}
	if !reflect.DeepEqual(operations, expected) {
		t.Fatal(operations)
	}
}
