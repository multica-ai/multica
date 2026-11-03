package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

const (
	inboxTestWorkspace = "11111111-1111-1111-1111-111111111111"
	inboxTestIssue     = "22222222-2222-2222-2222-222222222222"
	inboxTestItem1     = "33333333-3333-3333-3333-333333333333"
	inboxTestItem2     = "44444444-4444-4444-4444-444444444444"
)

func setupInboxTest(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_TOKEN", "mat_test")
	t.Setenv("MULTICA_WORKSPACE_ID", inboxTestWorkspace)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
}

func inboxListFixture() []map[string]any {
	return []map[string]any{
		{
			"id": inboxTestItem1, "workspace_id": inboxTestWorkspace,
			"recipient_type": "member", "recipient_id": "55555555-5555-5555-5555-555555555555",
			"type": "new_comment", "severity": "info", "issue_id": inboxTestIssue,
			"title": "A useful comment", "read": false, "archived": false,
			"created_at": "2026-10-13T01:00:00Z", "issue_status": "done", "issue_priority": "high",
			"actor_type": "agent", "actor_id": "66666666-6666-6666-6666-666666666666", "details": map[string]any{},
		},
		{
			"id": inboxTestItem2, "workspace_id": inboxTestWorkspace,
			"recipient_type": "member", "recipient_id": "55555555-5555-5555-5555-555555555555",
			"type": "status_change", "severity": "info", "issue_id": inboxTestIssue,
			"title": "Issue completed", "read": true, "archived": false,
			"created_at": "2026-10-13T00:00:00Z", "issue_status": "done", "issue_priority": "high",
			"actor_type": "member", "actor_id": "77777777-7777-7777-7777-777777777777", "details": map[string]any{},
		},
	}
}

func serveInboxFixture(t *testing.T, postCount *int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Workspace-ID"); got != inboxTestWorkspace {
			t.Errorf("X-Workspace-ID = %q, want %q", got, inboxTestWorkspace)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/inbox":
			_ = json.NewEncoder(w).Encode(inboxListFixture())
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+inboxTestIssue:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": inboxTestIssue, "identifier": "MUL-521", "status": "done", "priority": "high",
				"project_id":    "88888888-8888-8888-8888-888888888888",
				"assignee_type": "agent", "assignee_id": "99999999-9999-9999-9999-999999999999",
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/archive"):
			if postCount != nil {
				atomic.AddInt32(postCount, 1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": inboxTestItem1, "archived": true})
		default:
			http.NotFound(w, r)
		}
	}
}

func TestRunInboxListFiltersAndEnrichesJSON(t *testing.T) {
	setupInboxTest(t, serveInboxFixture(t, nil))
	cmd := newInboxListCommand("list", "", runInboxList)
	_ = cmd.Flags().Set("output", "json")
	_ = cmd.Flags().Set("unread", "true")

	out, err := captureStdout(t, func() error { return runInboxList(cmd, nil) })
	if err != nil {
		t.Fatalf("runInboxList: %v", err)
	}
	var items []inboxItem
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if len(items) != 1 || items[0].ID != inboxTestItem1 {
		t.Fatalf("items = %#v, want only unread item", items)
	}
	if items[0].IssueIdentifier != "MUL-521" || items[0].ProjectID == "" || items[0].AssigneeID == "" {
		t.Fatalf("item was not enriched: %#v", items[0])
	}
}

func TestRunInboxBulkDismissRequiresFilterAndExplicitLimit(t *testing.T) {
	cmd := newInboxListCommand("bulk-dismiss", "", runInboxBulkDismiss)
	cmd.Flags().Bool("dry-run", false, "")
	if err := runInboxBulkDismiss(cmd, nil); err == nil || !strings.Contains(err.Error(), "explicit positive --limit") {
		t.Fatalf("error = %v, want explicit limit error", err)
	}
	_ = cmd.Flags().Set("limit", "10")
	if err := runInboxBulkDismiss(cmd, nil); err == nil || !strings.Contains(err.Error(), "explicit filter") {
		t.Fatalf("error = %v, want explicit filter error", err)
	}
}

func TestRunInboxBulkDismissDryRunDoesNotMutate(t *testing.T) {
	var postCount int32
	setupInboxTest(t, serveInboxFixture(t, &postCount))
	cmd := newInboxListCommand("bulk-dismiss", "", runInboxBulkDismiss)
	cmd.Flags().Bool("dry-run", false, "")
	_ = cmd.Flags().Set("limit", "10")
	_ = cmd.Flags().Set("status", "done,cancelled")
	_ = cmd.Flags().Set("dry-run", "true")

	out, err := captureStdout(t, func() error { return runInboxBulkDismiss(cmd, nil) })
	if err != nil {
		t.Fatalf("runInboxBulkDismiss: %v", err)
	}
	if got := atomic.LoadInt32(&postCount); got != 0 {
		t.Fatalf("POST count = %d, want 0", got)
	}
	if !strings.Contains(out, `"dry_run": true`) || !strings.Contains(out, `"matched_count": 2`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRunInboxBulkDismissDeduplicatesIssueGroups(t *testing.T) {
	var postCount int32
	setupInboxTest(t, serveInboxFixture(t, &postCount))
	cmd := newInboxListCommand("bulk-dismiss", "", runInboxBulkDismiss)
	cmd.Flags().Bool("dry-run", false, "")
	_ = cmd.Flags().Set("limit", "10")
	_ = cmd.Flags().Set("status", "done")

	_, err := captureStdout(t, func() error { return runInboxBulkDismiss(cmd, nil) })
	if err != nil {
		t.Fatalf("runInboxBulkDismiss: %v", err)
	}
	if got := atomic.LoadInt32(&postCount); got != 1 {
		t.Fatalf("POST count = %d, want 1 for one issue group", got)
	}
}

func TestRunInboxSingleItemActions(t *testing.T) {
	var paths []string
	setupInboxTest(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": inboxTestItem1, "read": true, "archived": strings.HasSuffix(r.URL.Path, "/archive")})
	})
	for _, tc := range []struct {
		name string
		run  func(*cobra.Command, []string) error
		path string
	}{
		{"mark-read", runInboxMarkRead, "/api/inbox/" + inboxTestItem1 + "/read"},
		{"dismiss", runInboxDismiss, "/api/inbox/" + inboxTestItem1 + "/archive"},
	} {
		cmd := &cobra.Command{Use: tc.name}
		cmd.Flags().String("workspace", "", "")
		cmd.Flags().String("output", "json", "")
		if _, err := captureStdout(t, func() error { return tc.run(cmd, []string{inboxTestItem1}) }); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := paths[len(paths)-1]; got != "POST "+tc.path {
			t.Fatalf("path = %q, want %q", got, "POST "+tc.path)
		}
	}
}
