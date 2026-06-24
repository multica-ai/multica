package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newKnowledgeSearchTestCmd(output *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{Use: "search"}
	cmd.Flags().String("issue", "", "")
	cmd.Flags().Int32("limit", 5, "")
	cmd.Flags().String("output", "table", "")
	cmd.SetOut(output)
	return cmd
}

func TestRunKnowledgeSearchSendsIssueAndDefaultLimit(t *testing.T) {
	var request map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/knowledge/search" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
			t.Fatalf("workspace_id query = %q, want ws-1", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(knowledgeSearchTestResponse())
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("HOME", t.TempDir())

	var stdout bytes.Buffer
	cmd := newKnowledgeSearchTestCmd(&stdout)
	if err := cmd.Flags().Set("issue", "OPE-2655"); err != nil {
		t.Fatalf("set issue: %v", err)
	}

	if err := runKnowledgeSearch(cmd, []string{"mobile push failure"}); err != nil {
		t.Fatalf("runKnowledgeSearch: %v", err)
	}

	if request["query"] != "mobile push failure" {
		t.Fatalf("query = %#v", request["query"])
	}
	if request["issue_id"] != "OPE-2655" {
		t.Fatalf("issue_id = %#v", request["issue_id"])
	}
	if request["limit"] != float64(5) {
		t.Fatalf("limit = %#v, want 5", request["limit"])
	}
	for _, want := range []string{"ID", "SUMMARY", "KNO-1", "high", "OPE-2649", "Use APNs directly"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("table output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunKnowledgeSearchJSONOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/knowledge/search" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(knowledgeSearchTestResponse())
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("HOME", t.TempDir())

	var stdout bytes.Buffer
	cmd := newKnowledgeSearchTestCmd(&stdout)
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("set output: %v", err)
	}
	if err := cmd.Flags().Set("limit", "2"); err != nil {
		t.Fatalf("set limit: %v", err)
	}

	if err := runKnowledgeSearch(cmd, []string{"apns"}); err != nil {
		t.Fatalf("runKnowledgeSearch: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, stdout.String())
	}
	if out["total"] != float64(1) {
		t.Fatalf("total = %#v, want 1", out["total"])
	}
}

func TestRunKnowledgeSearchRejectsInvalidLimit(t *testing.T) {
	var stdout bytes.Buffer
	cmd := newKnowledgeSearchTestCmd(&stdout)
	if err := cmd.Flags().Set("limit", "0"); err != nil {
		t.Fatalf("set limit: %v", err)
	}
	if err := runKnowledgeSearch(cmd, []string{"query"}); err == nil || !strings.Contains(err.Error(), "--limit must be greater than 0") {
		t.Fatalf("runKnowledgeSearch error = %v", err)
	}
}

func knowledgeSearchTestResponse() map[string]any {
	return map[string]any{
		"results": []map[string]any{{
			"item": map[string]any{
				"id":                   "KNO-1",
				"title":                "Use APNs directly for iOS notification parity",
				"problem_pattern":      "Getui delivery can hide APNs-specific failures.",
				"recommended_practice": "Use APNs directly when debugging iOS-only push delivery.",
				"applicability":        "mobile push",
				"confidence_status":    "high",
				"lifecycle_status":     "published",
				"domain_labels":        []string{"mobile", "push"},
			},
			"source_summary": map[string]any{
				"count":                1,
				"types":                []string{"issue"},
				"primary_source_type":  "issue",
				"primary_source_id":    "11111111-1111-1111-1111-111111111111",
				"primary_source_title": "OPE-2649",
			},
			"final_score":  3.25,
			"match_reason": "text",
		}},
		"total": 1,
	}
}
