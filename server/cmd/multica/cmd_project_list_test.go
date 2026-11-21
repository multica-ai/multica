package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newProjectListTestServer(t *testing.T, response map[string]any) *[]url.Values {
	t.Helper()
	queries := &[]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			json.NewEncoder(w).Encode(map[string]any{"id": "11111111-1111-1111-1111-111111111111"})
		case "/api/projects":
			*queries = append(*queries, r.URL.Query())
			json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return queries
}

func runProjectListWithFlags(t *testing.T, flags map[string][]string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "list"}
	registerProjectListFlags(cmd)
	for name, values := range flags {
		for _, value := range values {
			if err := cmd.Flags().Set(name, value); err != nil {
				t.Fatalf("set --%s %q: %v", name, value, err)
			}
		}
	}
	return captureStdout(t, func() error { return runProjectList(cmd, nil) })
}

func TestProjectListSendsFiltersSortAndPage(t *testing.T) {
	queries := newProjectListTestServer(t, map[string]any{"projects": []any{}, "total": 0})
	_, err := runProjectListWithFlags(t, map[string][]string{
		"output":    {"json"},
		"status":    {"planned,in_progress", "paused"},
		"priority":  {"urgent"},
		"lead":      {"me", "none"},
		"search":    {"sync"},
		"sort":      {"due_date"},
		"direction": {"desc"},
		"limit":     {"20"},
		"offset":    {"40"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*queries) != 1 {
		t.Fatalf("expected one list request, got %d", len(*queries))
	}
	q := (*queries)[0]
	want := map[string]string{
		"status":    "planned,in_progress,paused",
		"priority":  "urgent",
		"lead":      "member:11111111-1111-1111-1111-111111111111,none",
		"q":         "sync",
		"sort":      "due_date",
		"direction": "desc",
		"limit":     "20",
		"offset":    "40",
	}
	for key, value := range want {
		if got := q.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestProjectListWithoutFlagsKeepsTheArrayOutput(t *testing.T) {
	queries := newProjectListTestServer(t, map[string]any{
		"projects": []any{map[string]any{"id": "p1", "title": "One"}},
		"total":    1,
		"has_more": false,
	})
	out, err := runProjectListWithFlags(t, map[string][]string{"output": {"json"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"limit", "offset", "sort", "status"} {
		if (*queries)[0].Has(key) {
			t.Errorf("no flag should send %s", key)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("json output must stay an array, got %s", out)
	}
}

func TestProjectListRejectsAnOutOfRangeLimit(t *testing.T) {
	newProjectListTestServer(t, map[string]any{"projects": []any{}})
	if _, err := runProjectListWithFlags(t, map[string][]string{"limit": {"500"}}); err == nil {
		t.Fatal("--limit 500 should be rejected")
	}
}
