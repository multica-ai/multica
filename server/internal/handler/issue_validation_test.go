package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateIssueInvalidStatusReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "invalid status issue",
		"status": "active",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "backlog") {
		t.Errorf("expected error to list valid statuses, got: %s", body)
	}
}

func TestCreateIssueInvalidPriorityReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "invalid priority issue",
		"priority": "P1",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "urgent") {
		t.Errorf("expected error to list valid priorities, got: %s", body)
	}
}

func TestUpdateIssueInvalidStatusReturns400(t *testing.T) {
	issueID := createTestIssue(t, "update invalid status issue", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "active"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateIssueInvalidPriorityReturns400(t *testing.T) {
	issueID := createTestIssue(t, "update invalid priority issue", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"priority": "P1"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateIssueRejectsStaleLabelIDsWithoutMutation(t *testing.T) {
	issueID := createTestIssue(t, "update stale label issue", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  "stale-update-label",
		"color": "#3b82f6",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var label LabelResponse
	if err := json.NewDecoder(w.Body).Decode(&label); err != nil {
		t.Fatalf("decode label: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/labels/"+label.ID, nil)
	req = withURLParam(req, "id", label.ID)
	testHandler.DeleteLabel(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteLabel: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"title":     "mutated title",
		"status":    "in_progress",
		"label_ids": []string{label.ID},
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for stale label, got %d: %s", w.Code, w.Body.String())
	}

	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("GetIssue after failed update: %v", err)
	}
	if issue.Title != "update stale label issue" {
		t.Fatalf("expected title to remain unchanged, got %q", issue.Title)
	}
	if issue.Status != "todo" {
		t.Fatalf("expected status to remain todo, got %q", issue.Status)
	}
}

func TestBatchUpdateIssuesInvalidStatusReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{"not-needed"},
		"updates": map[string]any{
			"status": "active",
		},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatchUpdateIssuesInvalidPriorityReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{"not-needed"},
		"updates": map[string]any{
			"priority": "P1",
		},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d: %s", w.Code, w.Body.String())
	}
}
