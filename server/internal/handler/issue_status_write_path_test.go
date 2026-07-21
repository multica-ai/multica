package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuestatus"
)

// MUL-4809 §3.1 / §6.1 — the issue WRITE path resolves through the status catalog.
//
// Before this, the update handler validated `status` against the 7 legacy tokens and
// wrote the raw string, never touching status_id. That made a custom status
// definable but unusable: no issue could ever be moved into one. These tests pin the
// resolved write path — status_id and the compat `status` projection are written from
// the SAME catalog row, so they can never disagree.

// ensureTestWorkspaceStatuses seeds the 7 built-ins for the shared test workspace.
// The fixture inserts the workspace with raw SQL, so it never went through the
// creation path that calls issuestatus.Ensure. Idempotent.
func ensureTestWorkspaceStatuses(t *testing.T) {
	t.Helper()
	if err := issuestatus.Ensure(context.Background(), testHandler.Queries, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("seed workspace issue statuses: %v", err)
	}
}

// updateIssueStatusFields PATCHes an issue with whichever status inputs are given.
func updateIssueStatusFields(t *testing.T, issueID string, body map[string]any) (IssueResponse, int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PATCH", "/api/issues/"+issueID, body), "id", issueID)
	testHandler.UpdateIssue(w, req)
	var resp IssueResponse
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code, w.Body.String()
}

// readIssueStatusColumns reads the raw persisted pair, bypassing the response shape.
func readIssueStatusColumns(t *testing.T, issueID string) (status string, statusID string) {
	t.Helper()
	var sid *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, status_id::text FROM issue WHERE id = $1`, issueID).Scan(&status, &sid); err != nil {
		t.Fatalf("read issue status columns: %v", err)
	}
	if sid != nil {
		statusID = *sid
	}
	return status, statusID
}

// A custom status is reachable by its id, and the legacy column projects to the
// status's Category so every not-yet-migrated reader keeps working.
func TestUpdateIssueAcceptsCustomStatusByID(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Needs QA", "category": "in_progress", "icon": "in_review", "color": "warning",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, custom.ID, "") })

	issueID := createTestIssue(t, "custom status by id", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	resp, code, body := updateIssueStatusFields(t, issueID, map[string]any{"status_id": custom.ID})
	if code != http.StatusOK {
		t.Fatalf("update by status_id: %d %s", code, body)
	}
	if resp.StatusID == nil || *resp.StatusID != custom.ID {
		t.Fatalf("response status_id = %v, want %s", resp.StatusID, custom.ID)
	}
	status, statusID := readIssueStatusColumns(t, issueID)
	if statusID != custom.ID {
		t.Fatalf("persisted status_id = %q, want %s", statusID, custom.ID)
	}
	// Custom statuses have no system_key, so the compat token is the Category.
	if status != "in_progress" {
		t.Fatalf("compat status projection = %q, want in_progress", status)
	}
}

// The alias form must reach a custom status by its exact display name (§3.1 rule 3).
func TestUpdateIssueAcceptsCustomStatusByExactName(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Awaiting Design", "category": "backlog", "icon": "backlog", "color": "info",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, custom.ID, "") })

	issueID := createTestIssue(t, "custom status by name", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	// Case-insensitive on purpose — Resolve lowercases and trims.
	if _, code, body := updateIssueStatusFields(t, issueID, map[string]any{"status": "awaiting design"}); code != http.StatusOK {
		t.Fatalf("update by exact name: %d %s", code, body)
	}
	status, statusID := readIssueStatusColumns(t, issueID)
	if statusID != custom.ID {
		t.Fatalf("persisted status_id = %q, want %s", statusID, custom.ID)
	}
	if status != "backlog" {
		t.Fatalf("compat status projection = %q, want backlog", status)
	}
}

// A built-in keeps its exact legacy token, so in_review / blocked survive the
// round-trip rather than being flattened to their Category.
func TestUpdateIssueBuiltInKeepsLegacyToken(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	issueID := createTestIssue(t, "builtin legacy token", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	if _, code, body := updateIssueStatusFields(t, issueID, map[string]any{"status": "in_review"}); code != http.StatusOK {
		t.Fatalf("update to in_review: %d %s", code, body)
	}
	status, statusID := readIssueStatusColumns(t, issueID)
	if status != "in_review" {
		t.Fatalf("compat status = %q, want in_review (not its in_progress Category)", status)
	}
	if statusID == "" {
		t.Fatal("built-in update must still populate status_id")
	}
}

// Sending both fields is fine when they agree, and a hard 400 when they do not —
// picking a silent winner would guess at the caller's intent.
func TestUpdateIssueRejectsConflictingStatusAndStatusID(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Pending Review", "category": "in_progress", "icon": "in_review", "color": "success",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, custom.ID, "") })

	issueID := createTestIssue(t, "conflicting status inputs", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	// Agreeing pair → accepted.
	if _, code, body := updateIssueStatusFields(t, issueID, map[string]any{
		"status": custom.Name, "status_id": custom.ID,
	}); code != http.StatusOK {
		t.Fatalf("agreeing status + status_id should be accepted: %d %s", code, body)
	}

	// Disagreeing pair → 400, and the issue must not move.
	before, _ := readIssueStatusColumns(t, issueID)
	_, code, body = updateIssueStatusFields(t, issueID, map[string]any{
		"status": "done", "status_id": custom.ID,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("conflicting status + status_id: expected 400, got %d %s", code, body)
	}
	if after, _ := readIssueStatusColumns(t, issueID); after != before {
		t.Fatalf("rejected update still moved the issue: %q -> %q", before, after)
	}
}

// Archived statuses stay readable for old issues but must not accept new ones.
func TestUpdateIssueRejectsArchivedAndUnknownStatus(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Retired Stage", "category": "todo", "icon": "todo", "color": "muted-foreground",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	if code, body := deleteStatus(t, custom.ID, ""); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("archive custom status: %d %s", code, body)
	}

	issueID := createTestIssue(t, "archived status target", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	if _, code, body := updateIssueStatusFields(t, issueID, map[string]any{"status_id": custom.ID}); code != http.StatusBadRequest {
		t.Fatalf("archived status_id: expected 400, got %d %s", code, body)
	}
	if _, code, body := updateIssueStatusFields(t, issueID, map[string]any{"status": "no such status"}); code != http.StatusBadRequest {
		t.Fatalf("unknown status name: expected 400, got %d %s", code, body)
	}
	if _, code, body := updateIssueStatusFields(t, issueID, map[string]any{"status_id": "not-a-uuid"}); code != http.StatusBadRequest {
		t.Fatalf("malformed status_id: expected 400, got %d %s", code, body)
	}
}

// createIssueWithStatusFields POSTs an issue with whichever status inputs are given.
func createIssueWithStatusFields(t *testing.T, body map[string]any) (IssueResponse, int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body))
	var resp IssueResponse
	if w.Code == http.StatusCreated {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code, w.Body.String()
}

// A new issue can start directly in a custom status (MUL-4809 §6.1) — the create
// path resolves the same way the update path does.
func TestCreateIssueAcceptsCustomStatus(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Triaging", "category": "todo", "icon": "todo", "color": "info",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, custom.ID, "") })

	created, code, body := createIssueWithStatusFields(t, map[string]any{
		"title": "starts in a custom status", "status_id": custom.ID, "priority": "none",
	})
	if code != http.StatusCreated {
		t.Fatalf("create by status_id: %d %s", code, body)
	}
	t.Cleanup(func() { deleteTestIssue(t, created.ID) })

	status, statusID := readIssueStatusColumns(t, created.ID)
	if statusID != custom.ID {
		t.Fatalf("persisted status_id = %q, want %s", statusID, custom.ID)
	}
	if status != "todo" {
		t.Fatalf("compat status projection = %q, want todo (the custom status Category)", status)
	}
}

// The create path applies the same guards as update: unknown / archived / a
// conflicting pair are all rejected rather than silently falling back.
func TestCreateIssueRejectsBadStatusInput(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Create Guard", "category": "done", "icon": "done", "color": "success",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, custom.ID, "") })

	if _, code, body := createIssueWithStatusFields(t, map[string]any{
		"title": "conflict", "status": "todo", "status_id": custom.ID, "priority": "none",
	}); code != http.StatusBadRequest {
		t.Fatalf("conflicting status + status_id: expected 400, got %d %s", code, body)
	}
	if _, code, body := createIssueWithStatusFields(t, map[string]any{
		"title": "unknown", "status": "no such status", "priority": "none",
	}); code != http.StatusBadRequest {
		t.Fatalf("unknown status: expected 400, got %d %s", code, body)
	}
}
