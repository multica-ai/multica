package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// decodeProject decodes a ProjectResponse from a recorder, failing the test on
// a non-expected status or a decode error.
func decodeProject(t *testing.T, w *httptest.ResponseRecorder, wantStatus int) ProjectResponse {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, w.Code, w.Body.String())
	}
	var p ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("decode ProjectResponse: %v", err)
	}
	return p
}

// Project start_date / due_date follow the same calendar-day contract as the
// issue dates: create echoes them, GET persists them, an update with the key
// present but empty clears the date while an absent key leaves it untouched.
func TestProjectStartDueDateLifecycle(t *testing.T) {
	// Create with both dates set.
	w := httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "dated project",
		"start_date": "2026-03-01",
		"due_date":   "2026-03-31",
	}))
	created := decodeProject(t, w, http.StatusCreated)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, created.ID)
	})
	if created.StartDate == nil || *created.StartDate != "2026-03-01" {
		t.Fatalf("create start_date = %v, want 2026-03-01", created.StartDate)
	}
	if created.DueDate == nil || *created.DueDate != "2026-03-31" {
		t.Fatalf("create due_date = %v, want 2026-03-31", created.DueDate)
	}

	// GET persists both dates.
	w = httptest.NewRecorder()
	getReq := withURLParam(newRequest("GET", "/api/projects/"+created.ID, nil), "id", created.ID)
	testHandler.GetProject(w, getReq)
	got := decodeProject(t, w, http.StatusOK)
	if got.StartDate == nil || *got.StartDate != "2026-03-01" || got.DueDate == nil || *got.DueDate != "2026-03-31" {
		t.Fatalf("get dates = (%v, %v), want (2026-03-01, 2026-03-31)", got.StartDate, got.DueDate)
	}

	// Update due_date only; start_date is absent from the body and must persist.
	w = httptest.NewRecorder()
	putReq := withURLParam(newRequest("PUT", "/api/projects/"+created.ID, map[string]any{
		"due_date": "2026-04-15",
	}), "id", created.ID)
	testHandler.UpdateProject(w, putReq)
	updated := decodeProject(t, w, http.StatusOK)
	if updated.DueDate == nil || *updated.DueDate != "2026-04-15" {
		t.Fatalf("update due_date = %v, want 2026-04-15", updated.DueDate)
	}
	if updated.StartDate == nil || *updated.StartDate != "2026-03-01" {
		t.Fatalf("start_date must be untouched by an absent key, got %v", updated.StartDate)
	}

	// Clearing: an explicit empty string nulls start_date, due_date untouched.
	w = httptest.NewRecorder()
	clearReq := withURLParam(newRequest("PUT", "/api/projects/"+created.ID, map[string]any{
		"start_date": "",
	}), "id", created.ID)
	testHandler.UpdateProject(w, clearReq)
	cleared := decodeProject(t, w, http.StatusOK)
	if cleared.StartDate != nil {
		t.Fatalf("start_date should be cleared, got %v", cleared.StartDate)
	}
	if cleared.DueDate == nil || *cleared.DueDate != "2026-04-15" {
		t.Fatalf("due_date must survive clearing start_date, got %v", cleared.DueDate)
	}
}

// SearchProjects hand-scans an explicit column list (not SELECT *), so a
// column/scan misalignment after adding start_date / due_date would surface
// here as a scan error or shifted values. Exercise the real query + scan.
func TestSearchProjectsCarriesDates(t *testing.T) {
	w := httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "zzsearchdated project",
		"start_date": "2026-05-01",
		"due_date":   "2026-05-31",
	}))
	created := decodeProject(t, w, http.StatusCreated)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, created.ID)
	})

	w = httptest.NewRecorder()
	testHandler.SearchProjects(w, newRequest("GET", "/api/projects/search?q=zzsearchdated", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("search status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Projects []SearchProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	var found *SearchProjectResponse
	for i := range resp.Projects {
		if resp.Projects[i].ID == created.ID {
			found = &resp.Projects[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("created project not in search results: %s", w.Body.String())
	}
	if found.StartDate == nil || *found.StartDate != "2026-05-01" || found.DueDate == nil || *found.DueDate != "2026-05-31" {
		t.Fatalf("search dates = (%v, %v), want (2026-05-01, 2026-05-31)", found.StartDate, found.DueDate)
	}
}

// A malformed calendar day is a 400 on both create and update, never a 500.
func TestProjectInvalidDateReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "bad date project",
		"start_date": "03/01/2026",
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid create start_date, got %d: %s", w.Code, w.Body.String())
	}

	// Seed a valid project, then reject a malformed update due_date.
	w = httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "bad update date project",
	}))
	project := decodeProject(t, w, http.StatusCreated)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
	})

	w = httptest.NewRecorder()
	putReq := withURLParam(newRequest("PUT", "/api/projects/"+project.ID, map[string]any{
		"due_date": "not-a-date",
	}), "id", project.ID)
	testHandler.UpdateProject(w, putReq)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid update due_date, got %d: %s", w.Code, w.Body.String())
	}
}
