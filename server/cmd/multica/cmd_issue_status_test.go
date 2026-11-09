package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// issue-status CLI tests run against a fake catalog API: they assert the
// request shape (status addressed by key or name, icon sent even when empty)
// and the printed result, not the server's own validation.

type issueStatusTestServer struct {
	statuses []issueStatusDTO
	posts    []map[string]any
	patches  []map[string]any
	deletes  []string
}

func newIssueStatusTestServer(t *testing.T) *issueStatusTestServer {
	t.Helper()
	s := &issueStatusTestServer{statuses: []issueStatusDTO{
		{ID: "status-todo", Key: "todo", Name: "Todo", Category: "unstarted", Color: "#94a3b8", IsSystem: true},
		{ID: "status-spec-review", Key: "spec_review", Name: "Spec review", Category: "started", Color: "#3b82f6", Icon: "half"},
		{ID: "status-old", Key: "old_spec", Name: "Old spec", Category: "started", Color: "#3b82f6", ArchivedAt: ptrString("2026-11-01T00:00:00Z")},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issue-statuses":
			includeArchived := r.URL.Query().Get("include_archived") == "true"
			out := make([]issueStatusDTO, 0, len(s.statuses))
			for _, status := range s.statuses {
				if status.ArchivedAt != nil && !includeArchived {
					continue
				}
				out = append(out, status)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"statuses": out, "total": len(out)})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issue-statuses":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.posts = append(s.posts, body)
			created := issueStatusDTO{ID: "status-new", Category: "started", Color: "#3b82f6"}
			if v, ok := body["name"].(string); ok {
				created.Name = v
			}
			if v, ok := body["category"].(string); ok {
				created.Category = v
			}
			if v, ok := body["icon"].(string); ok {
				created.Icon = v
			}
			created.Key = "new_status"
			_ = json.NewEncoder(w).Encode(created)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/issue-statuses/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/issue-statuses/")
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.patches = append(s.patches, body)
			updated := issueStatusDTO{}
			for _, status := range s.statuses {
				if status.ID == id {
					updated = status
				}
			}
			if v, ok := body["icon"].(string); ok {
				updated.Icon = v
			}
			if v, ok := body["name"].(string); ok {
				updated.Name = v
			}
			_ = json.NewEncoder(w).Encode(updated)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/issue-statuses/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/issue-statuses/")
			s.deletes = append(s.deletes, id)
			archived := issueStatusDTO{ID: id, ArchivedAt: ptrString("2026-11-09T00:00:00Z")}
			for _, status := range s.statuses {
				if status.ID == id {
					archived = status
					archived.ArchivedAt = ptrString("2026-11-09T00:00:00Z")
				}
			}
			_ = json.NewEncoder(w).Encode(archived)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	return s
}

func ptrString(v string) *string { return &v }

// issueStatusTestCmd mirrors the flags the real command registers, so a run
// function under test sees the same Changed() semantics.
func issueStatusTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "issue-status"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("category", "", "")
	cmd.Flags().String("color", "", "")
	cmd.Flags().String("key", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("icon", "", "")
	cmd.Flags().Float64("position", 0, "")
	cmd.Flags().Bool("include-archived", false, "")
	return cmd
}

func TestIssueStatusUpdateByKeySendsIcon(t *testing.T) {
	api := newIssueStatusTestServer(t)
	cmd := issueStatusTestCmd()
	_ = cmd.Flags().Set("output", "json")
	_ = cmd.Flags().Set("icon", "three_quarters")

	out, err := captureStdout(t, func() error { return runIssueStatusUpdate(cmd, []string{"spec_review"}) })
	if err != nil {
		t.Fatalf("runIssueStatusUpdate: %v", err)
	}
	if len(api.patches) != 1 {
		t.Fatalf("patches = %d, want 1", len(api.patches))
	}
	if got, _ := api.patches[0]["icon"].(string); got != "three_quarters" {
		t.Fatalf("icon = %q, want three_quarters", got)
	}
	var updated issueStatusDTO
	if err := json.Unmarshal([]byte(out), &updated); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if updated.Icon != "three_quarters" || updated.Key != "spec_review" {
		t.Fatalf("updated = %+v", updated)
	}
}

// An explicit empty --icon must reach the API: it is how a status is reset to
// the category default, distinct from not touching the icon at all.
func TestIssueStatusUpdateResetsIconToDefault(t *testing.T) {
	api := newIssueStatusTestServer(t)
	cmd := issueStatusTestCmd()
	_ = cmd.Flags().Set("icon", "")

	if _, err := captureStdout(t, func() error { return runIssueStatusUpdate(cmd, []string{"spec_review"}) }); err != nil {
		t.Fatalf("runIssueStatusUpdate: %v", err)
	}
	if len(api.patches) != 1 {
		t.Fatalf("patches = %d, want 1", len(api.patches))
	}
	icon, present := api.patches[0]["icon"]
	if !present {
		t.Fatal("icon omitted from the PATCH; an empty value must be sent to reset")
	}
	if icon != "" {
		t.Fatalf("icon = %v, want empty string", icon)
	}
}

func TestIssueStatusGetResolvesByKeyAndName(t *testing.T) {
	newIssueStatusTestServer(t)
	for _, ref := range []string{"spec_review", "Spec review", "status-spec-review"} {
		cmd := issueStatusTestCmd()
		_ = cmd.Flags().Set("output", "json")
		out, err := captureStdout(t, func() error { return runIssueStatusGet(cmd, []string{ref}) })
		if err != nil {
			t.Fatalf("get %q: %v", ref, err)
		}
		var got issueStatusDTO
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("get %q unmarshal: %v\n%s", ref, err, out)
		}
		if got.Key != "spec_review" {
			t.Fatalf("get %q resolved to %q", ref, got.Key)
		}
	}
}

func TestIssueStatusGetUnknownRefListsKeys(t *testing.T) {
	newIssueStatusTestServer(t)
	cmd := issueStatusTestCmd()
	_, err := captureStdout(t, func() error { return runIssueStatusGet(cmd, []string{"nope"}) })
	if err == nil {
		t.Fatal("expected an error for an unknown status")
	}
	if !strings.Contains(err.Error(), "spec_review") {
		t.Fatalf("error %q should name available keys", err)
	}
}

func TestIssueStatusCreateRequiresColorAndCategory(t *testing.T) {
	newIssueStatusTestServer(t)
	cmd := issueStatusTestCmd()
	_ = cmd.Flags().Set("name", "Spec review ML")
	if _, err := captureStdout(t, func() error { return runIssueStatusCreate(cmd, nil) }); err == nil || !strings.Contains(err.Error(), "--category") {
		t.Fatalf("missing --category error = %v", err)
	}
	_ = cmd.Flags().Set("category", "started")
	if _, err := captureStdout(t, func() error { return runIssueStatusCreate(cmd, nil) }); err == nil || !strings.Contains(err.Error(), "--color") {
		t.Fatalf("missing --color error = %v", err)
	}
}

func TestIssueStatusCreateSendsIcon(t *testing.T) {
	api := newIssueStatusTestServer(t)
	cmd := issueStatusTestCmd()
	_ = cmd.Flags().Set("name", "Spec review ML")
	_ = cmd.Flags().Set("category", "started")
	_ = cmd.Flags().Set("color", "#3b82f6")
	_ = cmd.Flags().Set("icon", "three_quarters")

	if _, err := captureStdout(t, func() error { return runIssueStatusCreate(cmd, nil) }); err != nil {
		t.Fatalf("runIssueStatusCreate: %v", err)
	}
	if len(api.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(api.posts))
	}
	if got, _ := api.posts[0]["icon"].(string); got != "three_quarters" {
		t.Fatalf("icon = %q", got)
	}
	if got, _ := api.posts[0]["key"]; got != nil {
		t.Fatalf("key sent although not requested: %v", got)
	}
}

func TestIssueStatusArchiveUsesResolvedID(t *testing.T) {
	api := newIssueStatusTestServer(t)
	cmd := issueStatusTestCmd()
	_ = cmd.Flags().Set("output", "json")

	if _, err := captureStdout(t, func() error { return runIssueStatusArchive(cmd, []string{"Spec review"}) }); err != nil {
		t.Fatalf("runIssueStatusArchive: %v", err)
	}
	if len(api.deletes) != 1 || api.deletes[0] != "status-spec-review" {
		t.Fatalf("deletes = %v, want status-spec-review", api.deletes)
	}
}

func TestIssueStatusListHidesArchivedByDefault(t *testing.T) {
	newIssueStatusTestServer(t)
	cmd := issueStatusTestCmd()
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdout(t, func() error { return runIssueStatusList(cmd, nil) })
	if err != nil {
		t.Fatalf("runIssueStatusList: %v", err)
	}
	var listed []issueStatusDTO
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	for _, s := range listed {
		if s.ArchivedAt != nil {
			t.Fatalf("list included archived %q", s.Key)
		}
	}

	cmd = issueStatusTestCmd()
	_ = cmd.Flags().Set("output", "json")
	_ = cmd.Flags().Set("include-archived", "true")
	out, err = captureStdout(t, func() error { return runIssueStatusList(cmd, nil) })
	if err != nil {
		t.Fatalf("runIssueStatusList --include-archived: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	found := false
	for _, s := range listed {
		if s.Key == "old_spec" {
			found = true
		}
	}
	if !found {
		t.Fatal("--include-archived omitted the archived status")
	}
}

func TestIssueStatusUpdateWithoutFieldsFails(t *testing.T) {
	newIssueStatusTestServer(t)
	cmd := issueStatusTestCmd()
	if _, err := captureStdout(t, func() error { return runIssueStatusUpdate(cmd, []string{"spec_review"}) }); err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("no-op update error = %v", err)
	}
}
