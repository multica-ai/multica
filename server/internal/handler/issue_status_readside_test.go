package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Read-side coverage for the custom-status catalog (MUL-4809, plan §6.2):
// status_id / status_detail on issue responses and the status_id /
// status_category list filters. Issues are created through the real handler so
// the status_id double-write (status token -> built-in system_key) is exercised.

func createIssueForReadside(t *testing.T, title, status string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": title, "status": status, "priority": "none",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var resp IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, resp.ID) })
	return resp
}

func getIssueForReadside(t *testing.T, id string) (IssueResponse, int) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.GetIssue(w, withURLParam(newRequest("GET", "/api/issues/"+id, nil), "id", id))
	var resp IssueResponse
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code
}

// listIssuesForReadside calls ListIssues with the given raw query string (filters
// only; the workspace comes from the header newRequest sets).
func listIssuesForReadside(t *testing.T, query string) ([]IssueResponse, int, string) {
	t.Helper()
	path := "/api/issues"
	if query != "" {
		path += "?" + query
	}
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		return nil, w.Code, w.Body.String()
	}
	var out struct {
		Issues []IssueResponse `json:"issues"`
	}
	json.NewDecoder(w.Body).Decode(&out)
	return out.Issues, w.Code, ""
}

func containsIssue(issues []IssueResponse, id string) bool {
	for _, is := range issues {
		if is.ID == id {
			return true
		}
	}
	return false
}

func findIssue(issues []IssueResponse, id string) *IssueResponse {
	for i := range issues {
		if issues[i].ID == id {
			return &issues[i]
		}
	}
	return nil
}

func TestIssueResponseIncludesStatusDetail(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	todoStatusID := getStatusCatalog(t, false).CategoryDefaults["todo"]

	issue := createIssueForReadside(t, "has status detail", "todo")

	// GetIssue resolves status_id + status_detail from the catalog.
	got, code := getIssueForReadside(t, issue.ID)
	if code != http.StatusOK {
		t.Fatalf("GetIssue: %d", code)
	}
	if got.StatusID == nil || *got.StatusID != todoStatusID {
		t.Fatalf("GetIssue status_id = %v, want %s", got.StatusID, todoStatusID)
	}
	if got.StatusDetail == nil {
		t.Fatal("GetIssue status_detail is nil, want the todo catalog detail")
	}
	if got.StatusDetail.ID != todoStatusID || got.StatusDetail.Category != "todo" ||
		got.StatusDetail.Name == "" || got.StatusDetail.Icon == "" || got.StatusDetail.Color == "" {
		t.Errorf("GetIssue status_detail incomplete: %+v", *got.StatusDetail)
	}

	// The list endpoint attaches it too (filter by status_id to dodge pagination
	// of the shared test workspace).
	issues, code, msg := listIssuesForReadside(t, "status_id="+todoStatusID)
	if code != http.StatusOK {
		t.Fatalf("ListIssues: %d %s", code, msg)
	}
	listed := findIssue(issues, issue.ID)
	if listed == nil {
		t.Fatal("created issue missing from status_id-filtered list")
	}
	if listed.StatusDetail == nil || listed.StatusDetail.Category != "todo" {
		t.Errorf("list status_detail = %+v", listed.StatusDetail)
	}
}

func TestListIssuesStatusCategoryFilter(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	todoIssue := createIssueForReadside(t, "todo category filter", "todo")
	doneIssue := createIssueForReadside(t, "done category filter", "done")

	issues, code, msg := listIssuesForReadside(t, "status_category=todo")
	if code != http.StatusOK {
		t.Fatalf("list: %d %s", code, msg)
	}
	if !containsIssue(issues, todoIssue.ID) {
		t.Error("status_category=todo did not return the todo issue")
	}
	if containsIssue(issues, doneIssue.ID) {
		t.Error("status_category=todo wrongly returned the done issue")
	}
}

func TestListIssuesStatusIDFilter(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	cat := getStatusCatalog(t, false)
	todoStatusID := cat.CategoryDefaults["todo"]

	todoIssue := createIssueForReadside(t, "todo id filter", "todo")
	doneIssue := createIssueForReadside(t, "done id filter", "done")

	issues, code, msg := listIssuesForReadside(t, "status_id="+todoStatusID)
	if code != http.StatusOK {
		t.Fatalf("list: %d %s", code, msg)
	}
	if !containsIssue(issues, todoIssue.ID) {
		t.Error("status_id filter did not return the matching issue")
	}
	if containsIssue(issues, doneIssue.ID) {
		t.Error("status_id filter wrongly returned a non-matching issue")
	}
}

func TestListIssuesStatusFilterValidation(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	if _, code, _ := listIssuesForReadside(t, "status_id=not-a-uuid"); code != http.StatusBadRequest {
		t.Errorf("invalid status_id: expected 400, got %d", code)
	}
	if _, code, _ := listIssuesForReadside(t, "status_category=bogus"); code != http.StatusBadRequest {
		t.Errorf("invalid status_category: expected 400, got %d", code)
	}
}

func TestIssueWithoutStatusIDHasNoStatusDetail(t *testing.T) {
	// An issue inserted with a NULL status_id (workspace catalog not seeded at
	// write time) resolves to no status_detail — the client falls back to the
	// legacy status token.
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'no status_id', 'todo', 'none', 'member', $2,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id::text
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	got, code := getIssueForReadside(t, issueID)
	if code != http.StatusOK {
		t.Fatalf("GetIssue: %d", code)
	}
	if got.StatusID != nil {
		t.Errorf("status_id = %v, want nil for an issue with NULL status_id", *got.StatusID)
	}
	if got.StatusDetail != nil {
		t.Errorf("status_detail = %+v, want nil", *got.StatusDetail)
	}
	if got.Status != "todo" {
		t.Errorf("legacy status = %q, want todo", got.Status)
	}
}

// TestListOpenIssuesStatusFilters covers P1-1 (MUL-4809 read-side review): the
// open_only fast path must apply status_id / status_category and validate them
// (they were silently ignored before, and bad values wrongly returned 200).
func TestListOpenIssuesStatusFilters(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	todoStatusID := getStatusCatalog(t, false).CategoryDefaults["todo"]

	// Both statuses are "open" (not done/cancelled), so status filtering — not the
	// open predicate — is what must separate them.
	todoIssue := createIssueForReadside(t, "open todo", "todo")
	inProgIssue := createIssueForReadside(t, "open in progress", "in_progress")

	issues, code, msg := listIssuesForReadside(t, "open_only=true&status_category=todo")
	if code != http.StatusOK {
		t.Fatalf("open_only category filter: %d %s", code, msg)
	}
	if !containsIssue(issues, todoIssue.ID) {
		t.Error("open_only status_category=todo dropped the todo issue")
	}
	if containsIssue(issues, inProgIssue.ID) {
		t.Error("open_only status_category=todo wrongly returned the in_progress issue")
	}

	issues, code, _ = listIssuesForReadside(t, "open_only=true&status_id="+todoStatusID)
	if code != http.StatusOK {
		t.Fatalf("open_only status_id filter: %d", code)
	}
	got := findIssue(issues, todoIssue.ID)
	if got == nil {
		t.Fatal("open_only status_id filter dropped the matching issue")
	}
	if got.StatusDetail == nil || got.StatusDetail.Category != "todo" {
		t.Errorf("open_only status_detail = %+v, want category todo", got.StatusDetail)
	}
	if containsIssue(issues, inProgIssue.ID) {
		t.Error("open_only status_id filter returned a non-matching issue")
	}

	// Invalid values are a 400 on the open_only branch too (no longer ignored).
	if _, code, _ := listIssuesForReadside(t, "open_only=true&status_id=not-a-uuid"); code != http.StatusBadRequest {
		t.Errorf("open_only invalid status_id: expected 400, got %d", code)
	}
	if _, code, _ := listIssuesForReadside(t, "open_only=true&status_category=bogus"); code != http.StatusBadRequest {
		t.Errorf("open_only invalid status_category: expected 400, got %d", code)
	}
}

// TestListChildIssuesAttachesStatusDetail covers P1-4 (MUL-4809): the child-list
// read endpoint hydrates status_detail like the other Issue read entries.
func TestListChildIssuesAttachesStatusDetail(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	todoStatusID := getStatusCatalog(t, false).CategoryDefaults["todo"]
	parent := createIssueForReadside(t, "parent for child detail", "todo")

	var childID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, status_id, priority, creator_type, creator_id, parent_issue_id, number)
		VALUES ($1, 'child with status', 'todo', $2, 'none', 'member', $3, $4,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id::text
	`, testWorkspaceID, todoStatusID, testUserID, parent.ID).Scan(&childID); err != nil {
		t.Fatalf("insert child: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, childID) })

	w := httptest.NewRecorder()
	testHandler.ListChildIssues(w, withURLParam(newRequest("GET", "/api/issues/"+parent.ID+"/children", nil), "id", parent.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("ListChildIssues: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Issues []IssueResponse `json:"issues"`
	}
	json.NewDecoder(w.Body).Decode(&out)
	child := findIssue(out.Issues, childID)
	if child == nil {
		t.Fatal("child issue missing from children list")
	}
	if child.StatusID == nil || *child.StatusID != todoStatusID {
		t.Errorf("child status_id = %v, want %s", child.StatusID, todoStatusID)
	}
	if child.StatusDetail == nil || child.StatusDetail.Category != "todo" {
		t.Errorf("child status_detail = %+v, want category todo", child.StatusDetail)
	}
}
