package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createIssueWithDue posts a create with the given raw due_date value and
// returns the recorder so callers can assert on status or the decoded issue.
func createIssueWithDue(t *testing.T, title, dueRaw string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   "todo",
		"priority": "low",
		"due_date": dueRaw,
	})
	testHandler.CreateIssue(w, req)
	return w
}

// PR2: the API round-trips a full instant. A time-of-day survives create→response.
func TestCreateIssue_PreservesDueDateTime(t *testing.T) {
	w := createIssueWithDue(t, "datetime-due", "2026-02-01T14:30:00Z")
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })
	if issue.DueDate == nil || *issue.DueDate != "2026-02-01T14:30:00Z" {
		t.Fatalf("due_date round-trip: got %v want 2026-02-01T14:30:00Z", issue.DueDate)
	}
}

// PR2: legacy date-only input is still accepted and lands at that day's UTC midnight.
func TestCreateIssue_AcceptsLegacyDateOnly(t *testing.T) {
	w := createIssueWithDue(t, "legacy-date-only", "2026-02-01")
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })
	if issue.DueDate == nil || *issue.DueDate != "2026-02-01T00:00:00Z" {
		t.Fatalf("legacy date-only: got %v want 2026-02-01T00:00:00Z", issue.DueDate)
	}
}

// PR2: an unparseable due_date is rejected with 400, not silently dropped.
func TestCreateIssue_RejectsInvalidDueDate(t *testing.T) {
	w := createIssueWithDue(t, "bad-due", "nonsense")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid due_date, got %d: %s", w.Code, w.Body.String())
	}
}
