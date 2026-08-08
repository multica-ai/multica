package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedTestWorkspaceStatuses seeds the 7 built-in statuses into the shared test
// workspace (the handler fixture inserts the workspace with raw SQL, so it has
// no catalog) and registers cleanup that wipes the whole catalog afterwards, so
// each status test starts from a known, hermetic state.
func seedTestWorkspaceStatuses(t *testing.T) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	if err := issuestatus.Ensure(ctx, db.New(testPool), parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("seed statuses: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE workspace_id = $1`, testWorkspaceID)
	})
}

func getStatusCatalog(t *testing.T, includeArchived bool) IssueStatusCatalogResponse {
	t.Helper()
	path := "/api/issue-statuses"
	if includeArchived {
		path += "?include_archived=true"
	}
	w := httptest.NewRecorder()
	testHandler.ListIssueStatuses(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueStatuses: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueStatusCatalogResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	return resp
}

func createStatus(t *testing.T, body map[string]any) (IssueStatusResponse, int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateIssueStatus(w, newRequest("POST", "/api/issue-statuses", body))
	var resp IssueStatusResponse
	if w.Code == http.StatusCreated {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code, w.Body.String()
}

func patchStatus(t *testing.T, id string, body map[string]any) (IssueStatusResponse, int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PATCH", "/api/issue-statuses/"+id, body), "id", id)
	testHandler.UpdateIssueStatus(w, req)
	var resp IssueStatusResponse
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code, w.Body.String()
}

func deleteStatus(t *testing.T, id, migrateTo string) (int, string) {
	t.Helper()
	path := "/api/issue-statuses/" + id
	if migrateTo != "" {
		path += "?migrate_to_status_id=" + migrateTo
	}
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("DELETE", path, nil), "id", id)
	testHandler.DeleteIssueStatus(w, req)
	return w.Code, w.Body.String()
}

func TestIssueStatusCatalogHasBuiltins(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	cat := getStatusCatalog(t, false)

	if cat.Total != 7 {
		t.Fatalf("expected 7 built-in statuses, got %d", cat.Total)
	}
	// Every Category alias resolves to its default; both legacy aliases resolve.
	for _, c := range issuestatus.Categories {
		if cat.Aliases[c] == "" {
			t.Errorf("alias %q has no target", c)
		}
		if cat.CategoryDefaults[c] == "" {
			t.Errorf("category %q has no default", c)
		}
	}
	for _, legacy := range []string{"in_review", "blocked"} {
		if cat.Aliases[legacy] == "" {
			t.Errorf("legacy alias %q has no target", legacy)
		}
	}
}

func TestCreateCustomStatus(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	created, code, msg := createStatus(t, map[string]any{
		"name": "Needs Clarification", "category": "todo", "icon": "todo", "color": "warning",
	})
	if code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", code, msg)
	}
	if created.IsSystem {
		t.Error("custom status should not be is_system")
	}
	if created.SystemKey != nil {
		t.Error("custom status should have null system_key")
	}
	if created.Category != "todo" {
		t.Errorf("category = %q, want todo", created.Category)
	}
	// Appears in the catalog and sorts after the built-in Todo (position > 0).
	cat := getStatusCatalog(t, false)
	if cat.Total != 8 {
		t.Fatalf("expected 8 statuses, got %d", cat.Total)
	}
	if created.Position <= 0 {
		t.Errorf("custom status position = %v, want > 0", created.Position)
	}
}

func TestCreateStatusRejectsReservedName(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	// The reserved tokens are the underscore alias forms; " Todo " and "IN_PROGRESS"
	// normalize (trim + lowercase) onto them. A display-name collision like
	// "In Progress" is a different rejection (409 on the unique-name index).
	for _, name := range []string{"todo", " Todo ", "IN_PROGRESS", "in_review", "BLOCKED"} {
		_, code, _ := createStatus(t, map[string]any{"name": name, "category": "todo", "icon": "todo", "color": "warning"})
		if code != http.StatusBadRequest {
			t.Errorf("reserved name %q: expected 400, got %d", name, code)
		}
	}
}

func TestCreateStatusValidation(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"bad category", map[string]any{"name": "X", "category": "review", "icon": "todo", "color": "warning"}},
		{"bad color", map[string]any{"name": "X", "category": "todo", "icon": "todo", "color": "#ff0000"}},
		{"bad icon", map[string]any{"name": "X", "category": "todo", "icon": "rocket", "color": "warning"}},
		{"empty name", map[string]any{"name": "  ", "category": "todo", "icon": "todo", "color": "warning"}},
	}
	for _, tc := range cases {
		_, code, msg := createStatus(t, tc.body)
		if code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", tc.name, code, msg)
		}
	}
}

func TestCreateStatusDuplicateName(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	if _, code, msg := createStatus(t, map[string]any{"name": "Design Review", "category": "in_progress", "icon": "in_review", "color": "success"}); code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", code, msg)
	}
	// Case-insensitive collision against the active name index.
	if _, code, _ := createStatus(t, map[string]any{"name": "design review", "category": "in_progress", "icon": "in_review", "color": "success"}); code != http.StatusConflict {
		t.Errorf("duplicate create: expected 409, got %d", code)
	}
}

func TestCreateStatusAsDefaultSwapsCategoryDefault(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	before := getStatusCatalog(t, false)
	oldDefault := before.CategoryDefaults["todo"]

	created, code, msg := createStatus(t, map[string]any{
		"name": "Triage", "category": "todo", "icon": "todo", "color": "warning", "is_default": true,
	})
	if code != http.StatusCreated {
		t.Fatalf("create default: expected 201, got %d: %s", code, msg)
	}
	after := getStatusCatalog(t, false)
	if after.CategoryDefaults["todo"] != created.ID {
		t.Errorf("todo default = %q, want new status %q", after.CategoryDefaults["todo"], created.ID)
	}
	if after.CategoryDefaults["todo"] == oldDefault {
		t.Error("old default should have been demoted")
	}
	// Exactly one default in the todo category.
	defaults := 0
	for _, s := range after.Statuses {
		if s.Category == "todo" && s.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Errorf("todo has %d defaults, want exactly 1", defaults)
	}
}

func TestUpdateStatusRejectsImmutableFields(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	created, _, _ := createStatus(t, map[string]any{"name": "Staging", "category": "in_progress", "icon": "in_progress", "color": "warning"})
	for _, body := range []map[string]any{
		{"category": "done"},
		{"system_key": "in_review"},
		{"workspace_id": testWorkspaceID},
	} {
		_, code, msg := patchStatus(t, created.ID, body)
		if code != http.StatusBadRequest {
			t.Errorf("immutable %v: expected 400, got %d: %s", body, code, msg)
		}
	}
}

func TestUpdateStatusRenameAndRecolor(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	created, _, _ := createStatus(t, map[string]any{"name": "QA", "category": "in_progress", "icon": "in_progress", "color": "warning"})
	updated, code, msg := patchStatus(t, created.ID, map[string]any{"name": "QA Review", "color": "success"})
	if code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", code, msg)
	}
	if updated.Name != "QA Review" || updated.Color != "success" {
		t.Errorf("got name=%q color=%q, want QA Review/success", updated.Name, updated.Color)
	}
}

func TestUpdateStatusPromoteAndCannotUnsetDefault(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	before := getStatusCatalog(t, false)
	systemTodoDefault := before.CategoryDefaults["todo"]

	custom, _, _ := createStatus(t, map[string]any{"name": "Grooming", "category": "todo", "icon": "todo", "color": "warning"})
	// Promote the custom status to default.
	if _, code, msg := patchStatus(t, custom.ID, map[string]any{"is_default": true}); code != http.StatusOK {
		t.Fatalf("promote: expected 200, got %d: %s", code, msg)
	}
	after := getStatusCatalog(t, false)
	if after.CategoryDefaults["todo"] != custom.ID {
		t.Errorf("todo default = %q, want %q", after.CategoryDefaults["todo"], custom.ID)
	}
	// Unsetting the sole default is refused — you promote another instead.
	if _, code, _ := patchStatus(t, custom.ID, map[string]any{"is_default": false}); code != http.StatusBadRequest {
		t.Errorf("unset default: expected 400, got %d", code)
	}
	// Re-promoting the built-in Todo restores it as default.
	if _, code, _ := patchStatus(t, systemTodoDefault, map[string]any{"is_default": true}); code != http.StatusOK {
		t.Errorf("re-promote system: expected 200, got %d", code)
	}
}

func TestArchiveCustomStatus(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	created, _, _ := createStatus(t, map[string]any{"name": "Temp", "category": "backlog", "icon": "backlog", "color": "muted-foreground"})
	if code, msg := deleteStatus(t, created.ID, ""); code != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d: %s", code, msg)
	}
	// Gone from the active catalog, present with include_archived.
	if active := getStatusCatalog(t, false); active.Total != 7 {
		t.Errorf("active catalog = %d, want 7 after archive", active.Total)
	}
	found := false
	for _, s := range getStatusCatalog(t, true).Statuses {
		if s.ID == created.ID {
			found = true
			if !s.Archived {
				t.Error("archived status should report archived=true")
			}
		}
	}
	if !found {
		t.Error("archived status missing from include_archived catalog")
	}
}

func TestArchiveSystemStatusRejected(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	cat := getStatusCatalog(t, false)
	if code, _ := deleteStatus(t, cat.CategoryDefaults["done"], ""); code != http.StatusBadRequest {
		t.Errorf("archive system status: expected 400, got %d", code)
	}
}

func TestArchiveDefaultStatusRejected(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	created, _, _ := createStatus(t, map[string]any{"name": "Parked", "category": "backlog", "icon": "backlog", "color": "muted-foreground", "is_default": true})
	if code, _ := deleteStatus(t, created.ID, ""); code != http.StatusBadRequest {
		t.Errorf("archive default status: expected 400, got %d", code)
	}
}

func TestArchiveInUseStatusRequiresMigration(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	from, _, _ := createStatus(t, map[string]any{"name": "Legacy Stage", "category": "in_progress", "icon": "in_progress", "color": "warning"})
	to, _, _ := createStatus(t, map[string]any{"name": "New Stage", "category": "in_progress", "icon": "in_progress", "color": "warning"})
	otherCat, _, _ := createStatus(t, map[string]any{"name": "Wrong Cat", "category": "done", "icon": "done", "color": "info"})

	// Point a real issue at the from-status via the authoritative status_id.
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, status_id, priority, creator_type, creator_id, number)
		VALUES ($1, 'uses custom status', 'in_progress', $2, 'none', 'member', $3,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, from.ID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	// No migrate target while in use -> 409.
	if code, _ := deleteStatus(t, from.ID, ""); code != http.StatusConflict {
		t.Errorf("archive in-use without migration: expected 409, got %d", code)
	}
	// Cross-category migrate target -> 400.
	if code, _ := deleteStatus(t, from.ID, otherCat.ID); code != http.StatusBadRequest {
		t.Errorf("cross-category migration: expected 400, got %d", code)
	}
	// Same-category migrate target -> 200 and the issue moves.
	if code, msg := deleteStatus(t, from.ID, to.ID); code != http.StatusOK {
		t.Fatalf("same-category migration: expected 200, got %d: %s", code, msg)
	}
	var movedTo string
	if err := testPool.QueryRow(context.Background(), `SELECT status_id FROM issue WHERE id = $1`, issueID).Scan(&movedTo); err != nil {
		t.Fatalf("read issue status_id: %v", err)
	}
	if movedTo != to.ID {
		t.Errorf("issue status_id = %q, want migrated %q", movedTo, to.ID)
	}
}

func TestManageStatusRejectsAgents(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issue-statuses", map[string]any{"name": "Agentic", "category": "todo", "icon": "todo", "color": "warning"})
	// Trusted server-set actor header: resolveActor returns an agent identity.
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", testUserID)
	testHandler.CreateIssueStatus(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("agent create: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCustomStatusCap(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	for i := 0; i < maxActiveCustomIssueStatusesPerWorkspace; i++ {
		if _, code, msg := createStatus(t, map[string]any{
			"name": fmt.Sprintf("Custom %d", i), "category": "in_progress", "icon": "in_progress", "color": "warning",
		}); code != http.StatusCreated {
			t.Fatalf("create %d: expected 201, got %d: %s", i, code, msg)
		}
	}
	if _, code, _ := createStatus(t, map[string]any{"name": "One Too Many", "category": "in_progress", "icon": "in_progress", "color": "warning"}); code != http.StatusBadRequest {
		t.Errorf("over-cap create: expected 400, got %d", code)
	}
}
