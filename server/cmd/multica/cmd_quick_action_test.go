package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const (
	testQuickActionID  = "11111111-1111-1111-1111-111111111111"
	testQuickActionID2 = "22222222-2222-2222-2222-222222222222"
	testQuickIssueID   = "33333333-3333-3333-3333-333333333333"
)

func newQuickActionListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().Bool("include-archived", false, "")
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

func newQuickActionGetTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

func newQuickActionRunTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().String("issue", "", "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func setQuickActionTestServerEnv(t *testing.T, serverURL string) {
	t.Helper()
	setCLITestServerEnv(t, serverURL)
	// The repository test runner executes inside a daemon-managed workdir in
	// some environments. Task-scoped tokens satisfy that guard while the local
	// httptest server remains responsible for the API response.
	t.Setenv("MULTICA_TOKEN", "mat_quick_action_test")
}

func TestQuickActionListIncludesArchivedAndRendersTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/quick-actions" {
			t.Fatalf("request = %s %s, want GET /api/quick-actions", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("include_archived"); got != "true" {
			t.Fatalf("include_archived = %q, want true", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"quick_actions": []map[string]any{
				{
					"id":             testQuickActionID,
					"name":           "Investigate timeout",
					"status":         "active",
					"visibility":     "public",
					"assignee_type":  "agent",
					"assignee_id":    "44444444-4444-4444-4444-444444444444",
					"target_name":    "Backend on-call",
					"use_count":      7,
					"last_used_at":   nil,
					"target_missing": false,
				},
				{
					"id":             testQuickActionID2,
					"name":           "Post update",
					"status":         "archived",
					"visibility":     "private",
					"assignee_type":  "",
					"assignee_id":    "",
					"use_count":      0,
					"last_used_at":   nil,
					"target_missing": false,
				},
			},
		})
	}))
	defer srv.Close()
	setQuickActionTestServerEnv(t, srv.URL)

	cmd := newQuickActionListTestCmd()
	if err := cmd.Flags().Set("include-archived", "true"); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runQuickActionList(cmd, nil) })
	if err != nil {
		t.Fatalf("runQuickActionList: %v", err)
	}
	for _, want := range []string{"ID", "NAME", "Investigate timeout", "agent:Backend on-call", "Post update", "(none)", "never"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestQuickActionGetResolvesExactNameAndIncludesArchived(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/quick-actions" {
			t.Fatalf("request = %s %s, want GET /api/quick-actions", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("include_archived"); got != "true" {
			t.Fatalf("include_archived = %q, want true", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"quick_actions": []map[string]any{
				{"id": testQuickActionID, "name": "Other", "status": "active"},
				{"id": testQuickActionID2, "name": "Archived triage", "status": "archived"},
			},
		})
	}))
	defer srv.Close()
	setQuickActionTestServerEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runQuickActionGet(newQuickActionGetTestCmd(), []string{"archived triage"})
	})
	if err != nil {
		t.Fatalf("runQuickActionGet: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if strVal(got, "id") != testQuickActionID2 || strVal(got, "name") != "Archived triage" {
		t.Fatalf("got quick action %#v", got)
	}
}

func TestQuickActionGetRejectsAmbiguousName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"quick_actions": []map[string]any{
				{"id": testQuickActionID, "name": "Triage"},
				{"id": testQuickActionID2, "name": "Triage"},
			},
		})
	}))
	defer srv.Close()
	setQuickActionTestServerEnv(t, srv.URL)

	_, err := captureStdout(t, func() error {
		return runQuickActionGet(newQuickActionGetTestCmd(), []string{"Triage"})
	})
	if err == nil || !strings.Contains(err.Error(), "more than one quick action is named") {
		t.Fatalf("err = %v, want ambiguous-name error", err)
	}
	for _, id := range []string{testQuickActionID, testQuickActionID2} {
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("error does not list %s: %v", id, err)
		}
	}
}

func TestQuickActionRunPostsToIssueAndPreservesTriggerOutcomes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/INFRA-214":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         testQuickIssueID,
				"identifier": "INFRA-214",
				"title":      "Quick actions",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+testQuickIssueID+"/quick-actions/"+testQuickActionID+"/run":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              "55555555-5555-5555-5555-555555555555",
				"issue_id":        testQuickIssueID,
				"quick_action_id": testQuickActionID,
				"content":         "Please investigate the timeout.",
				"trigger_outcomes": []map[string]any{{
					"target_type": "agent",
					"target_id":   "44444444-4444-4444-4444-444444444444",
					"status":      "queued",
					"reason_code": "",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setQuickActionTestServerEnv(t, srv.URL)

	cmd := newQuickActionRunTestCmd()
	if err := cmd.Flags().Set("issue", "INFRA-214"); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runQuickActionRun(cmd, []string{testQuickActionID}) })
	if err != nil {
		t.Fatalf("runQuickActionRun: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got["quick_action_id"] != testQuickActionID {
		t.Fatalf("quick_action_id = %#v, want %s", got["quick_action_id"], testQuickActionID)
	}
	if _, ok := got["trigger_outcomes"]; !ok {
		t.Fatalf("trigger_outcomes missing from JSON output: %#v", got)
	}
}

func TestQuickActionRunDryRunUsesRenderAndPrintsContent(t *testing.T) {
	requests := make([]string, 0, 3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/quick-actions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"quick_actions": []map[string]any{{"id": testQuickActionID, "name": "Triage"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/INFRA-214":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         testQuickIssueID,
				"identifier": "INFRA-214",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+testQuickIssueID+"/quick-actions/"+testQuickActionID+"/render":
			_ = json.NewEncoder(w).Encode(map[string]string{"content": "Rendered prompt"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setQuickActionTestServerEnv(t, srv.URL)

	cmd := newQuickActionRunTestCmd()
	for name, value := range map[string]string{"issue": "INFRA-214", "output": "table"} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runQuickActionRun(cmd, []string{"Triage"}) })
	if err != nil {
		t.Fatalf("runQuickActionRun dry-run: %v", err)
	}
	if out != "Rendered prompt\n" {
		t.Fatalf("output = %q, want rendered prompt", out)
	}
	want := []string{
		"GET /api/quick-actions",
		"GET /api/issues/INFRA-214",
		"POST /api/issues/" + testQuickIssueID + "/quick-actions/" + testQuickActionID + "/render",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(requests, "\n"), strings.Join(want, "\n"))
	}
}

func TestQuickActionRunRequiresIssue(t *testing.T) {
	_, err := captureStdout(t, func() error {
		return runQuickActionRun(newQuickActionRunTestCmd(), []string{testQuickActionID})
	})
	if err == nil || err.Error() != "--issue is required" {
		t.Fatalf("err = %v, want --issue is required", err)
	}
}
