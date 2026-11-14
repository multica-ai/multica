package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/spf13/cobra"
)

const (
	archiveTestProjectID  = "11111111-1111-1111-1111-111111111111"
	archiveTestArchivedID = "22222222-2222-2222-2222-222222222222"
)

func newProjectArchiveTestServer(t *testing.T, calls *[]string, listQueries *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			*listQueries = append(*listQueries, r.URL.RawQuery)
			projects := []any{
				map[string]any{"id": archiveTestProjectID, "title": "Active", "status": "in_progress", "archived_at": nil},
			}
			if r.URL.Query().Get("include_archived") == "true" {
				projects = append(projects, map[string]any{
					"id": archiveTestArchivedID, "title": "Old", "status": "completed", "archived_at": "2026-11-01T00:00:00Z",
				})
			}
			json.NewEncoder(w).Encode(map[string]any{"projects": projects, "total": len(projects)})
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/"+archiveTestArchivedID+"/archive":
			json.NewEncoder(w).Encode(map[string]any{
				"id": archiveTestArchivedID, "title": "Old", "archived_at": "2026-11-15T00:00:00Z",
				"paused_autopilot_ids": []string{"33333333-3333-3333-3333-333333333333"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/"+archiveTestArchivedID+"/restore":
			json.NewEncoder(w).Encode(map[string]any{"id": archiveTestArchivedID, "title": "Old", "archived_at": nil})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return srv
}

func TestRunProjectArchiveAndRestoreResolveArchivedPrefix(t *testing.T) {
	var calls, listQueries []string
	srv := newProjectArchiveTestServer(t, &calls, &listQueries)
	defer srv.Close()

	archive := &cobra.Command{Use: "archive"}
	archive.Flags().String("output", "json", "")
	if err := runProjectArchive(archive, []string{"22222222"}); err != nil {
		t.Fatalf("runProjectArchive: %v", err)
	}
	restore := &cobra.Command{Use: "restore"}
	restore.Flags().String("output", "json", "")
	if err := runProjectRestore(restore, []string{"22222222"}); err != nil {
		t.Fatalf("runProjectRestore: %v", err)
	}

	want := []string{
		"GET /api/projects",
		"POST /api/projects/" + archiveTestArchivedID + "/archive",
		"GET /api/projects",
		"POST /api/projects/" + archiveTestArchivedID + "/restore",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls[%d] = %q, want %q", i, calls[i], want[i])
		}
	}
	for _, q := range listQueries {
		if v, _ := url.ParseQuery(q); v.Get("include_archived") != "true" {
			t.Fatalf("prefix resolution must include archived projects, got query %q", q)
		}
	}
}

func TestRunProjectListArchivedOnly(t *testing.T) {
	var calls, listQueries []string
	srv := newProjectArchiveTestServer(t, &calls, &listQueries)
	defer srv.Close()

	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("full-id", false, "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().Bool("archived", false, "")
	_ = cmd.Flags().Set("archived", "true")

	out, err := captureStdout(t, func() error { return runProjectList(cmd, nil) })
	if err != nil {
		t.Fatalf("runProjectList: %v", err)
	}
	var projects []map[string]any
	if err := json.Unmarshal([]byte(out), &projects); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if len(projects) != 1 || projects[0]["id"] != archiveTestArchivedID {
		t.Fatalf("--archived listed %v, want only the archived project", projects)
	}
}
