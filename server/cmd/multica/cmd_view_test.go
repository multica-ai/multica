package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

const (
	testViewBugsUUID    = "6a000000-0000-0000-0000-000000000001"
	testViewBugsTwoUUID = "6b000000-0000-0000-0000-000000000002"
	testViewMineUUID    = "6c000000-0000-0000-0000-000000000003"
	testViewProjectUUID = "77777777-7777-7777-7777-777777777777"
)

// viewTestServer serves three views — "Bugs" (workspace), "Mine" (my) and a
// second "bugs" (project) — and records each issues request's query.
func viewTestServer(t *testing.T) *[]url.Values {
	t.Helper()
	views := []any{
		map[string]any{"id": testViewBugsUUID, "name": "Bugs", "scope_type": "workspace", "scope_id": nil, "visibility": "workspace", "owner_id": "u1"},
		map[string]any{"id": testViewMineUUID, "name": "Mine", "scope_type": "my", "scope_id": nil, "scope_variant": "assigned", "visibility": "private", "owner_id": "u1"},
		map[string]any{"id": testViewBugsTwoUUID, "name": "bugs", "scope_type": "project", "scope_id": testViewProjectUUID, "visibility": "workspace", "owner_id": "u1"},
	}
	var mu sync.Mutex
	var issueQueries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issue-views" && r.URL.Query().Get("scope_type") == "all":
			_ = json.NewEncoder(w).Encode(views)
		case r.Method == http.MethodGet && r.URL.Path == "/api/issue-views/"+testViewMineUUID:
			_ = json.NewEncoder(w).Encode(views[1])
		case r.Method == http.MethodGet && r.URL.Path == "/api/issue-views/"+testViewMineUUID+"/issues":
			mu.Lock()
			issueQueries = append(issueQueries, r.URL.Query())
			mu.Unlock()
			response := map[string]any{
				"issues": []any{
					map[string]any{"id": "i1", "identifier": "MUL-1", "title": "First", "status": "todo", "description": "long"},
				},
				"has_more":    true,
				"next_cursor": "c2",
			}
			if r.URL.Query().Get("cursor") == "" {
				response["total"] = 2
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	setCLITestServerEnv(t, srv.URL)
	return &issueQueries
}

func TestResolveIssueViewByIDNameAndPrefix(t *testing.T) {
	viewTestServer(t)
	client, err := newAPIClient(&cobra.Command{})
	if err != nil {
		t.Fatal(err)
	}
	for input, want := range map[string]string{
		testViewMineUUID: testViewMineUUID,
		"mine":           testViewMineUUID,
		"6c00":           testViewMineUUID,
	} {
		view, err := resolveIssueView(t.Context(), client, input)
		if err != nil {
			t.Fatalf("resolveIssueView(%q): %v", input, err)
		}
		if strVal(view, "id") != want {
			t.Errorf("resolveIssueView(%q) = %s, want %s", input, strVal(view, "id"), want)
		}
	}
	if _, err := resolveIssueView(t.Context(), client, "BUGS"); err == nil || !strings.Contains(err.Error(), "more than one view") {
		t.Fatalf("duplicate name: err = %v", err)
	}
	if _, err := resolveIssueView(t.Context(), client, "nope"); err == nil {
		t.Fatal("unknown name: want an error")
	}
}

func newViewIssuesTestCmd(t *testing.T, flags map[string]string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "issues"}
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().String("cursor", "", "")
	cmd.Flags().String("today", "", "")
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	cmd.Flags().String("fields", "", "")
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

func TestViewIssuesRequestsThePageAndWrapsJSON(t *testing.T) {
	queries := viewTestServer(t)
	cmd := newViewIssuesTestCmd(t, map[string]string{
		"output": "json", "limit": "10", "today": "2026-03-11", "fields": "identifier,title",
	})
	out, err := captureStdout(t, func() error { return runViewIssues(cmd, []string{"Mine"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(*queries) != 1 {
		t.Fatalf("issues requests = %d, want 1", len(*queries))
	}
	query := (*queries)[0]
	if query.Get("limit") != "10" || query.Get("today") != "2026-03-11" || query.Has("cursor") {
		t.Fatalf("issues query = %v", query)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if view, _ := got["view"].(map[string]any); strVal(view, "id") != testViewMineUUID || strVal(view, "name") != "Mine" {
		t.Fatalf("view = %v", got["view"])
	}
	if got["total"] != float64(2) || got["has_more"] != true || got["next_cursor"] != "c2" || got["limit"] != float64(10) {
		t.Fatalf("page meta = %v", got)
	}
	issues, _ := got["issues"].([]any)
	issue, _ := issues[0].(map[string]any)
	if len(issue) != 2 || issue["identifier"] != "MUL-1" || issue["title"] != "First" {
		t.Fatalf("issue after --fields = %v", issue)
	}
}

func TestViewIssuesPassesTheCursorAndOmitsTotalAfterTheFirstPage(t *testing.T) {
	queries := viewTestServer(t)
	cmd := newViewIssuesTestCmd(t, map[string]string{"output": "json", "cursor": "c2"})
	out, err := captureStdout(t, func() error { return runViewIssues(cmd, []string{testViewMineUUID}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := (*queries)[0]; got.Get("cursor") != "c2" || got.Get("today") == "" {
		t.Fatalf("issues query = %v", got)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["total"]; ok {
		t.Fatalf("total on a continuation page: %v", got)
	}
}

func TestViewIssuesRejectsBadFlagsBeforeAnyRequest(t *testing.T) {
	queries := viewTestServer(t)
	for name, flags := range map[string]map[string]string{
		"limit too large": {"limit": "101"},
		"limit zero":      {"limit": "0"},
		"bad today":       {"today": "03/11/2026"},
		"bad field":       {"fields": "assignee"},
	} {
		if err := runViewIssues(newViewIssuesTestCmd(t, flags), []string{"Mine"}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if len(*queries) != 0 {
		t.Fatalf("issues requests = %d, want 0", len(*queries))
	}
}

func TestViewListFiltersByScope(t *testing.T) {
	viewTestServer(t)
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("scope", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("full-id", false, "")
	_ = cmd.Flags().Set("scope", "my")
	out, err := captureStdout(t, func() error { return runViewList(cmd, nil) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var views []map[string]any
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || strVal(views[0], "id") != testViewMineUUID {
		t.Fatalf("views = %v", views)
	}
}
