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

func TestSprintCRUD(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	now := time.Now()
	startDate := now.Format("2006-01-02")
	endDate := now.AddDate(0, 0, 14).Format("2006-01-02")

	// Create
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/sprints?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "Test sprint " + now.Format(time.RFC3339Nano),
		"goal":       "Ship v1",
		"start_date": startDate,
		"end_date":   endDate,
	})
	testHandler.CreateSprint(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSprint: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created SprintResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode CreateSprint: %v", err)
	}
	if created.Status != "planned" {
		t.Errorf("CreateSprint: default status = %q, want planned", created.Status)
	}
	if created.StartDate != startDate {
		t.Errorf("CreateSprint: start_date = %q, want %q", created.StartDate, startDate)
	}
	if created.EndDate != endDate {
		t.Errorf("CreateSprint: end_date = %q, want %q", created.EndDate, endDate)
	}

	// Get
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/sprints/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetSprint(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetSprint: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var fetched SprintResponse
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode GetSprint: %v", err)
	}
	if fetched.ID != created.ID {
		t.Errorf("GetSprint: id = %q, want %q", fetched.ID, created.ID)
	}
	if fetched.Name != created.Name {
		t.Errorf("GetSprint: name = %q, want %q", fetched.Name, created.Name)
	}

	// List
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/sprints?workspace_id="+testWorkspaceID, nil)
	testHandler.ListSprints(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListSprints: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Sprints []SprintResponse `json:"sprints"`
		Total   int              `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode ListSprints: %v", err)
	}
	found := false
	for _, s := range listResp.Sprints {
		if s.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListSprints: created sprint %s not found in list", created.ID)
	}

	// Update name
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/sprints/"+created.ID, map[string]any{
		"name": "Updated sprint name",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateSprint(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSprint: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated SprintResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateSprint: %v", err)
	}
	if updated.Name != "Updated sprint name" {
		t.Errorf("UpdateSprint: name = %q, want 'Updated sprint name'", updated.Name)
	}

	// Update status
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/sprints/"+created.ID, map[string]any{
		"status": "active",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateSprint(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSprint status: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateSprint status: %v", err)
	}
	if updated.Status != "active" {
		t.Errorf("UpdateSprint status = %q, want active", updated.Status)
	}

	// Delete
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/sprints/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.DeleteSprint(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteSprint: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/sprints/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetSprint(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetSprint after delete: expected 404, got %d", w.Code)
	}
}

func TestCreateSprintValidation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	now := time.Now()
	cases := []struct {
		name string
		body map[string]any
	}{
		{
			name: "missing name",
			body: map[string]any{
				"start_date": now.Format("2006-01-02"),
				"end_date":   now.AddDate(0, 0, 7).Format("2006-01-02"),
			},
		},
		{
			name: "missing start_date",
			body: map[string]any{
				"name":     "No start",
				"end_date": now.AddDate(0, 0, 7).Format("2006-01-02"),
			},
		},
		{
			name: "missing end_date",
			body: map[string]any{
				"name":       "No end",
				"start_date": now.Format("2006-01-02"),
			},
		},
		{
			name: "invalid start_date format",
			body: map[string]any{
				"name":       "Bad start",
				"start_date": "not-a-date",
				"end_date":   now.AddDate(0, 0, 7).Format("2006-01-02"),
			},
		},
		{
			name: "invalid end_date format",
			body: map[string]any{
				"name":       "Bad end",
				"start_date": now.Format("2006-01-02"),
				"end_date":   "2025/01/01",
			},
		},
		{
			name: "end_date before start_date",
			body: map[string]any{
				"name":       "Reversed dates",
				"start_date": now.AddDate(0, 0, 7).Format("2006-01-02"),
				"end_date":   now.Format("2006-01-02"),
			},
		},
		{
			name: "end_date equals start_date",
			body: map[string]any{
				"name":       "Same dates",
				"start_date": now.Format("2006-01-02"),
				"end_date":   now.Format("2006-01-02"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/sprints?workspace_id="+testWorkspaceID, tc.body)
			testHandler.CreateSprint(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("CreateSprint %s: expected 400, got %d: %s", tc.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestUpdateSprintInvalidStatus(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	now := time.Now()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/sprints?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "Status validation sprint " + now.Format(time.RFC3339Nano),
		"start_date": now.Format("2006-01-02"),
		"end_date":   now.AddDate(0, 0, 7).Format("2006-01-02"),
	})
	testHandler.CreateSprint(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSprint: %d %s", w.Code, w.Body.String())
	}
	var created SprintResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		r := newRequest("DELETE", "/api/sprints/"+created.ID, nil)
		r = withURLParam(r, "id", created.ID)
		testHandler.DeleteSprint(httptest.NewRecorder(), r)
	}()

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/sprints/"+created.ID, map[string]any{
		"status": "invalid_status",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateSprint(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("UpdateSprint invalid status: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSprintDateValidation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	now := time.Now()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/sprints?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "Date validation sprint " + now.Format(time.RFC3339Nano),
		"start_date": now.Format("2006-01-02"),
		"end_date":   now.AddDate(0, 0, 14).Format("2006-01-02"),
	})
	testHandler.CreateSprint(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSprint: %d %s", w.Code, w.Body.String())
	}
	var created SprintResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		r := newRequest("DELETE", "/api/sprints/"+created.ID, nil)
		r = withURLParam(r, "id", created.ID)
		testHandler.DeleteSprint(httptest.NewRecorder(), r)
	}()

	cases := []struct {
		name string
		body map[string]any
	}{
		{
			name: "invalid start_date on update",
			body: map[string]any{"start_date": "not-a-date"},
		},
		{
			name: "invalid end_date on update",
			body: map[string]any{"end_date": "bad-date"},
		},
		{
			name: "end_date before start_date on update",
			body: map[string]any{
				"end_date": now.AddDate(0, 0, -1).Format("2006-01-02"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("PUT", "/api/sprints/"+created.ID, tc.body)
			req = withURLParam(req, "id", created.ID)
			testHandler.UpdateSprint(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("UpdateSprint %s: expected 400, got %d: %s", tc.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestSprintIssueStats(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}
	ctx := context.Background()

	now := time.Now()
	// Create sprint
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/sprints?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "Stats sprint " + now.Format(time.RFC3339Nano),
		"start_date": now.Format("2006-01-02"),
		"end_date":   now.AddDate(0, 0, 14).Format("2006-01-02"),
	})
	testHandler.CreateSprint(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSprint: %d %s", w.Code, w.Body.String())
	}
	var sprint SprintResponse
	json.NewDecoder(w.Body).Decode(&sprint)
	defer func() {
		r := newRequest("DELETE", "/api/sprints/"+sprint.ID, nil)
		r = withURLParam(r, "id", sprint.ID)
		testHandler.DeleteSprint(httptest.NewRecorder(), r)
	}()

	// Verify initial stats are 0
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/sprints/"+sprint.ID, nil)
	req = withURLParam(req, "id", sprint.ID)
	testHandler.GetSprint(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetSprint: %d %s", w.Code, w.Body.String())
	}
	var fetched SprintResponse
	json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.IssueCount != 0 || fetched.DoneCount != 0 {
		t.Fatalf("initial stats: issue_count=%d done_count=%d, want 0/0", fetched.IssueCount, fetched.DoneCount)
	}

	// Create issues linked to this sprint
	var issueIDs []string
	for i, status := range []string{"todo", "done", "in_progress", "done"} {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace SET issue_counter = issue_counter + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var issueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, creator_type, creator_id, title, status, priority, sprint_id, number, position)
			VALUES ($1, 'member', $2, $3, $4, 'none', $5, $6, 0)
			RETURNING id
		`, testWorkspaceID, testUserID, fmt.Sprintf("Sprint stats issue %d", i), status, sprint.ID, number).Scan(&issueID); err != nil {
			t.Fatalf("create issue %d: %v", i, err)
		}
		issueIDs = append(issueIDs, issueID)
	}
	t.Cleanup(func() {
		for _, id := range issueIDs {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
		}
	})

	// Verify stats on GetSprint
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/sprints/"+sprint.ID, nil)
	req = withURLParam(req, "id", sprint.ID)
	testHandler.GetSprint(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetSprint: %d %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.IssueCount != 4 {
		t.Errorf("GetSprint issue_count = %d, want 4", fetched.IssueCount)
	}
	if fetched.DoneCount != 2 {
		t.Errorf("GetSprint done_count = %d, want 2", fetched.DoneCount)
	}

	// Verify stats on ListSprints
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/sprints?workspace_id="+testWorkspaceID, nil)
	testHandler.ListSprints(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListSprints: %d %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Sprints []SprintResponse `json:"sprints"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)
	for _, s := range listResp.Sprints {
		if s.ID == sprint.ID {
			if s.IssueCount != 4 {
				t.Errorf("ListSprints[%s] issue_count = %d, want 4", s.ID, s.IssueCount)
			}
			if s.DoneCount != 2 {
				t.Errorf("ListSprints[%s] done_count = %d, want 2", s.ID, s.DoneCount)
			}
			return
		}
	}
	t.Errorf("sprint %s not found in ListSprints", sprint.ID)
}

func TestGetSprintNotFound(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/sprints/00000000-0000-0000-0000-000000000000", nil)
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
	testHandler.GetSprint(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("GetSprint not found: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSprintsWithStatusFilter(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not configured")
	}

	now := time.Now()
	// Create a sprint
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/sprints?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "Filter test sprint " + now.Format(time.RFC3339Nano),
		"start_date": now.Format("2006-01-02"),
		"end_date":   now.AddDate(0, 0, 7).Format("2006-01-02"),
	})
	testHandler.CreateSprint(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSprint: %d %s", w.Code, w.Body.String())
	}
	var created SprintResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		r := newRequest("DELETE", "/api/sprints/"+created.ID, nil)
		r = withURLParam(r, "id", created.ID)
		testHandler.DeleteSprint(httptest.NewRecorder(), r)
	}()

	// Filter by planned status — should include our sprint
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/sprints?workspace_id="+testWorkspaceID+"&status=planned", nil)
	testHandler.ListSprints(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListSprints planned: %d %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Sprints []SprintResponse `json:"sprints"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)
	found := false
	for _, s := range listResp.Sprints {
		if s.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListSprints status=planned: sprint %s not found", created.ID)
	}
}
