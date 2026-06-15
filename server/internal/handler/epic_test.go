package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEpicCRUD(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	// Create
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/epics?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Test epic " + time.Now().Format(time.RFC3339Nano),
		"description": "A test epic",
		"color":       "#ef4444",
	})
	testHandler.CreateEpic(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateEpic: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created EpicResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode CreateEpic: %v", err)
	}
	if created.Status != "open" {
		t.Errorf("CreateEpic: default status = %q, want open", created.Status)
	}
	if created.Color != "#ef4444" {
		t.Errorf("CreateEpic: color = %q, want #ef4444", created.Color)
	}

	// Get
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/epics/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetEpic(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetEpic: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var fetched EpicResponse
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode GetEpic: %v", err)
	}
	if fetched.ID != created.ID {
		t.Errorf("GetEpic: id = %q, want %q", fetched.ID, created.ID)
	}
	if fetched.Title != created.Title {
		t.Errorf("GetEpic: title = %q, want %q", fetched.Title, created.Title)
	}

	// List
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/epics?workspace_id="+testWorkspaceID, nil)
	testHandler.ListEpics(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListEpics: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Epics []EpicResponse `json:"epics"`
		Total int            `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode ListEpics: %v", err)
	}
	found := false
	for _, e := range listResp.Epics {
		if e.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListEpics: created epic %s not found in list", created.ID)
	}

	// Update title
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/epics/"+created.ID, map[string]any{
		"title": "Updated epic title",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateEpic(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateEpic: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated EpicResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateEpic: %v", err)
	}
	if updated.Title != "Updated epic title" {
		t.Errorf("UpdateEpic: title = %q, want 'Updated epic title'", updated.Title)
	}

	// Update status
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/epics/"+created.ID, map[string]any{
		"status": "closed",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateEpic(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateEpic status: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateEpic status: %v", err)
	}
	if updated.Status != "closed" {
		t.Errorf("UpdateEpic status = %q, want closed", updated.Status)
	}

	// Delete
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/epics/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.DeleteEpic(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteEpic: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/epics/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetEpic(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetEpic after delete: expected 404, got %d", w.Code)
	}
}

func TestCreateEpicValidation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	// Missing title
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/epics?workspace_id="+testWorkspaceID, map[string]any{
		"description": "no title",
	})
	testHandler.CreateEpic(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("CreateEpic missing title: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateEpicInvalidStatus(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/epics?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Status validation epic " + time.Now().Format(time.RFC3339Nano),
	})
	testHandler.CreateEpic(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateEpic: %d %s", w.Code, w.Body.String())
	}
	var created EpicResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		r := newRequest("DELETE", "/api/epics/"+created.ID, nil)
		r = withURLParam(r, "id", created.ID)
		testHandler.DeleteEpic(httptest.NewRecorder(), r)
	}()

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/epics/"+created.ID, map[string]any{
		"status": "invalid_status",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateEpic(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateEpic invalid status: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEpicIssueStats(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}
	ctx := context.Background()

	// Create epic
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/epics?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Stats epic " + time.Now().Format(time.RFC3339Nano),
	})
	testHandler.CreateEpic(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateEpic: %d %s", w.Code, w.Body.String())
	}
	var epic EpicResponse
	json.NewDecoder(w.Body).Decode(&epic)
	defer func() {
		r := newRequest("DELETE", "/api/epics/"+epic.ID, nil)
		r = withURLParam(r, "id", epic.ID)
		testHandler.DeleteEpic(httptest.NewRecorder(), r)
	}()

	// Verify initial stats are 0
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/epics/"+epic.ID, nil)
	req = withURLParam(req, "id", epic.ID)
	testHandler.GetEpic(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetEpic: %d %s", w.Code, w.Body.String())
	}
	var fetched EpicResponse
	json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.IssueCount != 0 || fetched.DoneCount != 0 {
		t.Fatalf("initial stats: issue_count=%d done_count=%d, want 0/0", fetched.IssueCount, fetched.DoneCount)
	}

	// Create issues linked to this epic
	var issueIDs []string
	for i, status := range []string{"todo", "done", "in_progress"} {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace SET issue_counter = issue_counter + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var issueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, creator_type, creator_id, title, status, priority, epic_id, number, position)
			VALUES ($1, 'member', $2, $3, $4, 'none', $5, $6, 0)
			RETURNING id
		`, testWorkspaceID, testUserID, fmt.Sprintf("Epic stats issue %d", i), status, epic.ID, number).Scan(&issueID); err != nil {
			t.Fatalf("create issue %d: %v", i, err)
		}
		issueIDs = append(issueIDs, issueID)
	}
	t.Cleanup(func() {
		for _, id := range issueIDs {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
		}
	})

	// Verify stats on GetEpic
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/epics/"+epic.ID, nil)
	req = withURLParam(req, "id", epic.ID)
	testHandler.GetEpic(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetEpic: %d %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.IssueCount != 3 {
		t.Errorf("GetEpic issue_count = %d, want 3", fetched.IssueCount)
	}
	if fetched.DoneCount != 1 {
		t.Errorf("GetEpic done_count = %d, want 1", fetched.DoneCount)
	}

	// Verify stats on ListEpics
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/epics?workspace_id="+testWorkspaceID, nil)
	testHandler.ListEpics(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListEpics: %d %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Epics []EpicResponse `json:"epics"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)
	for _, e := range listResp.Epics {
		if e.ID == epic.ID {
			if e.IssueCount != 3 {
				t.Errorf("ListEpics[%s] issue_count = %d, want 3", e.ID, e.IssueCount)
			}
			if e.DoneCount != 1 {
				t.Errorf("ListEpics[%s] done_count = %d, want 1", e.ID, e.DoneCount)
			}
			return
		}
	}
	t.Errorf("epic %s not found in ListEpics", epic.ID)
}

func TestGetEpicNotFound(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/epics/00000000-0000-0000-0000-000000000000", nil)
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
	testHandler.GetEpic(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("GetEpic not found: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListEpicsWithStatusFilter(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	// Create an epic
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/epics?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Filter test epic " + time.Now().Format(time.RFC3339Nano),
	})
	testHandler.CreateEpic(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateEpic: %d %s", w.Code, w.Body.String())
	}
	var created EpicResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		r := newRequest("DELETE", "/api/epics/"+created.ID, nil)
		r = withURLParam(r, "id", created.ID)
		testHandler.DeleteEpic(httptest.NewRecorder(), r)
	}()

	// Filter by open status — should include our epic
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/epics?workspace_id="+testWorkspaceID+"&status=open", nil)
	testHandler.ListEpics(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListEpics open: %d %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Epics []EpicResponse `json:"epics"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)
	found := false
	for _, e := range listResp.Epics {
		if e.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListEpics status=open: epic %s not found", created.ID)
	}
}
