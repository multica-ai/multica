package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The endpoint stays for clients from before the local FIFO assignee ordering
// (MUL-9724): same route and query parameters, 200 with a JSON array, but no
// database work. The handler has no Queries or DB pool, so any query would
// panic.
func TestGetAssigneeFrequencyReturnsEmptyListWithoutDB(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/assignee-frequency?workspace_id=00000000-0000-0000-0000-000000000001", nil)
	req.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000002")
	req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000001")
	w := httptest.NewRecorder()

	h.GetAssigneeFrequency(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "[]\n" {
		t.Fatalf("expected an empty JSON array, got %q", got)
	}
	var entries []AssigneeFrequencyEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("old clients decode the body as a list: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %+v", entries)
	}
}

func TestGetAssigneeFrequencyRequiresUser(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/assignee-frequency", nil)
	w := httptest.NewRecorder()

	h.GetAssigneeFrequency(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
