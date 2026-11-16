package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

const (
	testInboxProjectA = "aaaaaaaa-1111-1111-1111-111111111111"
	testInboxProjectB = "bbbbbbbb-2222-2222-2222-222222222222"
)

func newInboxListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("type", "", "")
	return cmd
}

func testInboxItems() []map[string]any {
	return []map[string]any{
		{
			"id": "10000000-0000-0000-0000-000000000001", "type": "mentioned", "read": false,
			"issue_id": "20000000-0000-0000-0000-000000000001", "issue_project_id": testInboxProjectA,
			"title": "Mentioned in A", "created_at": "2026-11-01T10:00:00Z",
			"details": map[string]any{"comment_id": "30000000-0000-0000-0000-000000000001"},
		},
		{
			"id": "10000000-0000-0000-0000-000000000002", "type": "new_comment", "read": true,
			"issue_id": "20000000-0000-0000-0000-000000000002", "issue_project_id": testInboxProjectA,
			"title": "Comment in A", "created_at": "2026-11-02T10:00:00Z",
		},
		{
			"id": "10000000-0000-0000-0000-000000000003", "type": "mentioned", "read": false,
			"issue_id": "20000000-0000-0000-0000-000000000003", "issue_project_id": testInboxProjectB,
			"title": "Mentioned in B", "created_at": "2026-11-03T10:00:00Z",
		},
		{
			"id": "10000000-0000-0000-0000-000000000004", "type": "mentioned", "read": false,
			"issue_id": nil, "issue_project_id": nil,
			"title": "No issue", "created_at": "2026-11-04T10:00:00Z",
		},
	}
}

// newInboxTestServer serves the inbox and the project list, recording every
// request as "METHOD path".
func newInboxTestServer(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	requests := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*requests = append(*requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/api/inbox":
			if r.Header.Get("X-Workspace-ID") != "ws-1" {
				t.Errorf("X-Workspace-ID = %q, want ws-1", r.Header.Get("X-Workspace-ID"))
			}
			_ = json.NewEncoder(w).Encode(testInboxItems())
		case "/api/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{"projects": []any{
				map[string]any{"id": testInboxProjectA, "title": "A", "status": "in_progress"},
				map[string]any{"id": testInboxProjectB, "title": "B", "status": "in_progress"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	setCLITestServerEnv(t, srv.URL)
	return requests
}

func runInboxListWithFlags(t *testing.T, flags map[string]string) (string, error) {
	t.Helper()
	cmd := newInboxListTestCmd()
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s %q: %v", name, value, err)
		}
	}
	return captureStdout(t, func() error { return runInboxList(cmd, nil) })
}

func decodeInboxIDs(t *testing.T, out string) []string {
	t.Helper()
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item["id"].(string))
	}
	return ids
}

func TestInboxListJSONPassesThroughServerFields(t *testing.T) {
	newInboxTestServer(t)
	out, err := runInboxListWithFlags(t, map[string]string{"output": "json"})
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if len(got) != 4 {
		t.Fatalf("got %d items, want all 4 unfiltered", len(got))
	}
	if got[0]["issue_id"] != "20000000-0000-0000-0000-000000000001" {
		t.Errorf("issue_id = %v, want passed through", got[0]["issue_id"])
	}
	details, _ := got[0]["details"].(map[string]any)
	if details["comment_id"] != "30000000-0000-0000-0000-000000000001" {
		t.Errorf("details.comment_id = %v, want passed through", details["comment_id"])
	}
}

func TestInboxListFiltersByProjectAndType(t *testing.T) {
	newInboxTestServer(t)
	out, err := runInboxListWithFlags(t, map[string]string{
		"output": "json", "project": testInboxProjectA, "type": "mentioned",
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := decodeInboxIDs(t, out)
	if len(ids) != 1 || ids[0] != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("ids = %v, want only the mentioned item in project A", ids)
	}

	out, err = runInboxListWithFlags(t, map[string]string{"output": "json", "type": "mentioned"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := decodeInboxIDs(t, out); len(ids) != 3 {
		t.Fatalf("type-only filter ids = %v, want 3", ids)
	}

	out, err = runInboxListWithFlags(t, map[string]string{"output": "json", "project": testInboxProjectB})
	if err != nil {
		t.Fatal(err)
	}
	if ids := decodeInboxIDs(t, out); len(ids) != 1 || ids[0] != "10000000-0000-0000-0000-000000000003" {
		t.Fatalf("project-only filter ids = %v, want the item in project B", ids)
	}
}

func TestInboxListResolvesProjectIDPrefix(t *testing.T) {
	newInboxTestServer(t)
	out, err := runInboxListWithFlags(t, map[string]string{"output": "json", "project": "bbbbbbbb"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := decodeInboxIDs(t, out); len(ids) != 1 || ids[0] != "10000000-0000-0000-0000-000000000003" {
		t.Fatalf("ids = %v, want the item in project B", ids)
	}
}

func TestInboxListNoMatchesPrintsEmptyJSONArray(t *testing.T) {
	newInboxTestServer(t)
	out, err := runInboxListWithFlags(t, map[string]string{"output": "json", "type": "task_failed"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("stdout = %q, want []", out)
	}
}

func TestInboxListTableOutput(t *testing.T) {
	newInboxTestServer(t)
	out, err := runInboxListWithFlags(t, map[string]string{"type": "new_comment"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "TYPE", "ISSUE ID", "TITLE", "10000000", "new_comment", "yes", "Comment in A", "2026-11-02"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Mentioned in A") {
		t.Errorf("table includes a filtered-out item:\n%s", out)
	}
}

func TestInboxListOnlyIssuesGETRequests(t *testing.T) {
	requests := newInboxTestServer(t)
	for _, flags := range []map[string]string{
		{"output": "json"},
		{"output": "json", "project": "aaaaaaaa", "type": "mentioned"},
		{},
	} {
		if _, err := runInboxListWithFlags(t, flags); err != nil {
			t.Fatal(err)
		}
	}
	if len(*requests) == 0 {
		t.Fatal("expected requests to the server")
	}
	for _, req := range *requests {
		if !strings.HasPrefix(req, "GET ") {
			t.Errorf("non-GET request %q; inbox list must be read-only", req)
		}
	}
}
