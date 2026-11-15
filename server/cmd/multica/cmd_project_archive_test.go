package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
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
	registerProjectListFlags(cmd)
	_ = cmd.Flags().Set("output", "json")
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

func TestResolveProjectIDPrefersLiveProjectOnSharedPrefix(t *testing.T) {
	const (
		live     = "11111111-aaaa-1111-1111-111111111111"
		archived = "11111111-bbbb-1111-1111-111111111111"
		shelved  = "22222222-2222-2222-2222-222222222222"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"projects": []any{
			map[string]any{"id": live, "title": "Live", "status": "in_progress", "archived_at": nil},
			map[string]any{"id": archived, "title": "Old", "status": "completed", "archived_at": "2026-11-01T00:00:00Z"},
			map[string]any{"id": shelved, "title": "Shelved", "status": "paused", "archived_at": "2026-11-01T00:00:00Z"},
		}})
	}))
	defer srv.Close()
	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	for name, resolve := range map[string]func(context.Context, *cli.APIClient, string) (resolvedID, error){
		"read":     resolveProjectID,
		"new work": resolveProjectIDForNewWork,
	} {
		ref, err := resolve(ctx, client, "11111111")
		if err != nil || ref.ID != live {
			t.Fatalf("%s: shared prefix resolved to %+v, %v; want the live project", name, ref, err)
		}
	}

	ref, err := resolveProjectID(ctx, client, "22222222")
	if err != nil || ref.ID != shelved || !ref.Archived {
		t.Fatalf("read of an archived-only prefix = %+v, %v; want the archived project", ref, err)
	}
	if _, err := resolveProjectIDForNewWork(ctx, client, "22222222"); err == nil ||
		!strings.Contains(err.Error(), "archived") || !strings.Contains(err.Error(), "multica project restore "+shelved) {
		t.Fatalf("new work into an archived-only prefix: err = %v, want the restore hint", err)
	}
}

// archivedProjectWriteServer lists a live and an archived project sharing a
// prefix and, like the API, accepts a write naming the record's current
// archived project but refuses a move into it with 409 project_archived.
func archivedProjectWriteServer(t *testing.T, recordPath, method, currentProject string, sent *[]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			json.NewEncoder(w).Encode(map[string]any{"projects": []any{
				map[string]any{"id": "11111111-aaaa-1111-1111-111111111111", "title": "Live", "status": "in_progress", "archived_at": nil},
				map[string]any{"id": archiveTestArchivedID, "title": "Old", "status": "completed", "archived_at": "2026-11-01T00:00:00Z"},
			}})
		case r.URL.Path == recordPath && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"id": "rec", "project_id": currentProject})
		case r.URL.Path == recordPath && r.Method == method:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			*sent = append(*sent, body["project_id"])
			if body["project_id"] == archiveTestArchivedID && currentProject != archiveTestArchivedID {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"code": "project_archived", "error": "project is archived; restore it before adding issues"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "rec", "project_id": body["project_id"]})
		default:
			http.NotFound(w, r)
		}
	}))
	setCLITestServerEnv(t, srv.URL)
	return srv
}

func TestUpdateCommandsKeepEditingInsideArchivedProject(t *testing.T) {
	const (
		issueID     = "44444444-4444-4444-8444-444444444444"
		autopilotID = "55555555-5555-4555-8555-555555555555"
		otherID     = "66666666-6666-4666-8666-666666666666"
	)
	commands := map[string]struct {
		path, method string
		run          func(project string) error
	}{
		"issue update": {"/api/issues/" + issueID, http.MethodPut, func(project string) error {
			cmd := newIssueUpdateTestCmd()
			_ = cmd.Flags().Set("title", "renamed")
			_ = cmd.Flags().Set("project", project)
			return runIssueUpdate(cmd, []string{issueID})
		}},
		"autopilot update": {"/api/autopilots/" + autopilotID, http.MethodPatch, func(project string) error {
			cmd := newAutopilotUpdateTestCmd()
			_ = cmd.Flags().Set("title", "renamed")
			_ = cmd.Flags().Set("project", project)
			return runAutopilotUpdate(cmd, []string{autopilotID})
		}},
	}
	for name, c := range commands {
		t.Run(name, func(t *testing.T) {
			// Already in the archived project: short prefix and full UUID behave the same.
			for _, ref := range []string{"22222222", archiveTestArchivedID} {
				var sent []any
				srv := archivedProjectWriteServer(t, c.path, c.method, archiveTestArchivedID, &sent)
				if err := c.run(ref); err != nil {
					t.Fatalf("edit inside archived project with --project %s: %v", ref, err)
				}
				if len(sent) != 1 || sent[0] != archiveTestArchivedID {
					t.Fatalf("--project %s sent project_id %v, want %s", ref, sent, archiveTestArchivedID)
				}
				srv.Close()
			}

			// Moving in from elsewhere is still refused, again the same for both forms.
			for _, ref := range []string{"22222222", archiveTestArchivedID} {
				var sent []any
				srv := archivedProjectWriteServer(t, c.path, c.method, otherID, &sent)
				err := c.run(ref)
				if err == nil || !strings.Contains(err.Error(), "archived") {
					t.Fatalf("move into archived project with --project %s: err = %v, want project_archived", ref, err)
				}
				srv.Close()
			}
		})
	}
}
