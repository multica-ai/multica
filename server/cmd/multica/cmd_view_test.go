package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func newViewIssuesTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "issues"}
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Int("offset", 0, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

func TestRunViewIssuesAppliesSavedDefinition(t *testing.T) {
	var groupedHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/views/view-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "view-1",
				"name":       "Agent launch work",
				"scope_type": "workspace",
				"visibility": "private",
				"definition": map[string]any{
					"version":            1,
					"viewMode":           "list",
					"grouping":           "status",
					"statusFilters":      []string{"todo"},
					"priorityFilters":    []string{"high"},
					"includeNoAssignee":  true,
					"sortBy":             "priority",
					"sortDirection":      "desc",
					"showSubIssues":      false,
					"workspaceActorKind": "agents",
				},
			})
		case "/api/issues/grouped":
			groupedHits++
			query := r.URL.Query()
			checks := map[string]string{
				"statuses":            "todo",
				"priorities":          "high",
				"include_no_assignee": "true",
				"assignee_types":      "agent,squad",
				"sort":                "priority",
				"direction":           "desc",
				"limit":               "100",
			}
			for key, want := range checks {
				if got := query.Get(key); got != want {
					t.Errorf("query %s = %q, want %q", key, got, want)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"groups": []any{map[string]any{
					"id": "assignee:unassigned",
					"issues": []any{
						map[string]any{"id": "root", "identifier": "DEV-1", "title": "Root"},
						map[string]any{"id": "child", "identifier": "DEV-2", "title": "Child", "parent_issue_id": "root"},
					},
					"total": 2,
				}},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	out, err := captureStdout(t, func() error {
		return runViewIssues(newViewIssuesTestCmd(), []string{"view-1"})
	})
	if err != nil {
		t.Fatalf("runViewIssues: %v", err)
	}
	if groupedHits != 1 {
		t.Fatalf("grouped endpoint hits = %d, want 1", groupedHits)
	}
	var result struct {
		Count  int              `json:"count"`
		Issues []map[string]any `json:"issues"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if result.Count != 1 || len(result.Issues) != 1 {
		t.Fatalf("filtered result = %#v, want one root issue", result)
	}
	if got := result.Issues[0]["id"]; got != "root" {
		t.Fatalf("issue id = %v, want root", got)
	}
	if strings.Contains(out, "child") {
		t.Fatalf("sub-issue leaked into output: %s", out)
	}
}

func TestRunViewIssuesPaginatesEveryGroupBeforeApplyingOffset(t *testing.T) {
	var groupedHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/views/view-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "view-1",
				"name":       "Large saved view",
				"scope_type": "workspace",
				"visibility": "workspace",
				"definition": map[string]any{
					"version":       1,
					"sortBy":        "position",
					"showSubIssues": true,
				},
			})
		case "/api/issues/grouped":
			groupedHits++
			if got := r.URL.Query().Get("limit"); got != "100" {
				t.Errorf("limit = %q, want 100", got)
			}
			switch r.URL.Query().Get("offset") {
			case "0":
				firstGroup := make([]map[string]any, 100)
				for i := range firstGroup {
					firstGroup[i] = map[string]any{"id": fmt.Sprintf("a-%03d", i)}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"groups": []any{
					map[string]any{"id": "assignee:member:a", "issues": firstGroup, "total": 101},
					map[string]any{"id": "assignee:member:b", "issues": []any{map[string]any{"id": "b-000"}}, "total": 1},
				}})
			case "100":
				_ = json.NewEncoder(w).Encode(map[string]any{"groups": []any{
					map[string]any{"id": "assignee:member:a", "issues": []any{map[string]any{"id": "a-100"}}, "total": 101},
				}})
			default:
				t.Errorf("unexpected grouped offset %q", r.URL.Query().Get("offset"))
				http.Error(w, "unexpected offset", http.StatusBadRequest)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newViewIssuesTestCmd()
	if err := cmd.Flags().Set("limit", "2"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("offset", "100"); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runViewIssues(cmd, []string{"view-1"})
	})
	if err != nil {
		t.Fatalf("runViewIssues: %v", err)
	}
	if groupedHits != 2 {
		t.Fatalf("grouped endpoint hits = %d, want 2", groupedHits)
	}
	var result struct {
		Issues  []map[string]any `json:"issues"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if len(result.Issues) != 2 {
		t.Fatalf("paginated issue count = %d, want 2: %s", len(result.Issues), out)
	}
	if got := []any{result.Issues[0]["id"], result.Issues[1]["id"]}; fmt.Sprint(got) != "[a-100 b-000]" {
		t.Fatalf("paginated ids = %v, want [a-100 b-000]", got)
	}
	if result.HasMore {
		t.Fatal("has_more = true, want false")
	}
}

func TestSavedViewDateBoundsResolvesRelativePresets(t *testing.T) {
	now := time.Date(2026, time.July, 15, 23, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	from, to, err := savedViewDateBounds(&savedViewDateFilter{
		From: "2020-01-01", To: "2020-01-07", Preset: "last_7_days",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if from != "2026-07-09" || to != "2026-07-15" {
		t.Fatalf("relative bounds = %s..%s, want 2026-07-09..2026-07-15", from, to)
	}

	from, to, err = savedViewDateBounds(&savedViewDateFilter{
		From: "2026-01-02", To: "2026-01-04",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if from != "2026-01-02" || to != "2026-01-04" {
		t.Fatalf("absolute bounds = %s..%s, want stored dates", from, to)
	}

	if _, _, err := savedViewDateBounds(&savedViewDateFilter{Preset: "future"}, now); err == nil {
		t.Fatal("invalid relative preset unexpectedly succeeded")
	}
}
