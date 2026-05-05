package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssueTransitionsCommandFetchesAvailableTransitions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"issue_id": "issue-1",
			"current_status": map[string]any{
				"key": "todo",
			},
			"transitions": []map[string]any{
				{"key": "in_progress", "name": "In progress", "category": "started", "action_label": "Move to in_progress"},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := issueTransitionsCmd
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("set output flag: %v", err)
	}
	if err := runIssueTransitions(cmd, []string{"issue-1"}); err != nil {
		t.Fatalf("runIssueTransitions returned error: %v", err)
	}
	if gotPath != "/api/issues/issue-1/available-transitions" {
		t.Fatalf("path = %q, want /api/issues/issue-1/available-transitions", gotPath)
	}
}
