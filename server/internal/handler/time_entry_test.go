package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTimeEntryLifecycle tests the full start → list → stop → delete flow.
func TestTimeEntryLifecycle(t *testing.T) {
	ctx := context.Background()

	// Create an issue to optionally link entries to.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues", map[string]any{
		"title":  "Time entry test issue",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		// time_entry rows are deleted by workspace cascade; clean issue explicitly.
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// ── Start a live timer ────────────────────────────────────────────────────
	startTime := time.Now().UTC().Truncate(time.Second)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/time-entries", map[string]any{
		"start_time": startTime.Format(time.RFC3339),
		"issue_id":   issueID,
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTimeEntry (start): expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var entry TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&entry)

	if entry.ID == "" {
		t.Fatal("expected non-empty entry ID")
	}
	if entry.DurationSeconds >= 0 {
		t.Errorf("expected negative duration_seconds while running, got %d", entry.DurationSeconds)
	}
	if entry.StopTime != nil {
		t.Errorf("expected nil stop_time for running timer, got %v", entry.StopTime)
	}
	if entry.IssueID == nil || *entry.IssueID != issueID {
		t.Errorf("expected issue_id %q, got %v", issueID, entry.IssueID)
	}

	// ── Get current timer ─────────────────────────────────────────────────────
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/time-entries/current", nil)
	testHandler.GetCurrentTimeEntry(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetCurrentTimeEntry: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var current TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&current)
	if current.ID != entry.ID {
		t.Errorf("GetCurrentTimeEntry: expected entry %q, got %q", entry.ID, current.ID)
	}

	// ── List entries ──────────────────────────────────────────────────────────
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/time-entries", nil)
	testHandler.ListTimeEntries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListTimeEntries: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) == 0 {
		t.Fatal("ListTimeEntries: expected at least one entry")
	}
	found := false
	for _, e := range list {
		if e.ID == entry.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListTimeEntries: created entry %q not found in list", entry.ID)
	}

	// ── Stop the timer ────────────────────────────────────────────────────────
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/time-entries/"+entry.ID+"/stop", nil)
	req = withURLParam(req, "entry_id", entry.ID)
	testHandler.StopTimeEntry(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StopTimeEntry: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stopped TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&stopped)
	if stopped.StopTime == nil {
		t.Error("StopTimeEntry: expected non-nil stop_time after stopping")
	}
	if stopped.DurationSeconds < 0 {
		t.Errorf("StopTimeEntry: expected non-negative duration_seconds, got %d", stopped.DurationSeconds)
	}

	// No current timer after stop.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/time-entries/current", nil)
	testHandler.GetCurrentTimeEntry(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetCurrentTimeEntry after stop: expected 200, got %d", w.Code)
	}
	var afterStop TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&afterStop)
	if afterStop.ID != "" {
		t.Errorf("expected no current timer after stop, got %q", afterStop.ID)
	}

	// ── Delete the entry ──────────────────────────────────────────────────────
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/time-entries/"+entry.ID, nil)
	req = withURLParam(req, "entry_id", entry.ID)
	testHandler.DeleteTimeEntry(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteTimeEntry: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// TestTimeEntryAutoStop verifies that starting a second timer auto-stops the first.
func TestTimeEntryAutoStop(t *testing.T) {
	// Start first timer.
	w := httptest.NewRecorder()
	startA := time.Now().UTC().Truncate(time.Second)
	req := newRequest("POST", "/api/time-entries", map[string]any{
		"start_time":  startA.Format(time.RFC3339),
		"description": "Timer A",
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("start timer A: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var entryA TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&entryA)

	// Start second timer — first should be auto-stopped.
	w = httptest.NewRecorder()
	startB := time.Now().UTC().Truncate(time.Second)
	req = newRequest("POST", "/api/time-entries", map[string]any{
		"start_time":  startB.Format(time.RFC3339),
		"description": "Timer B",
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("start timer B: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var entryB TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&entryB)

	// Current timer should be B, not A.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/time-entries/current", nil)
	testHandler.GetCurrentTimeEntry(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get current: expected 200, got %d", w.Code)
	}
	var current TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&current)
	if current.ID != entryB.ID {
		t.Errorf("expected current timer to be B (%q), got %q", entryB.ID, current.ID)
	}

	// Clean up.
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM time_entry WHERE id = $1`, entryA.ID)
		testPool.Exec(ctx, `DELETE FROM time_entry WHERE id = $1`, entryB.ID)
		testPool.Exec(ctx, `DELETE FROM running_timer WHERE user_id = $1`, testUserID)
	})
}

// TestTimeEntryIssueEndpoint tests GET/POST on the issue-scoped time-entries path.
func TestTimeEntryIssueEndpoint(t *testing.T) {
	ctx := context.Background()

	// Create an issue.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues", map[string]any{
		"title":  "Issue for time entry test",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM running_timer WHERE user_id = $1`, testUserID)
		testPool.Exec(ctx, `DELETE FROM time_entry WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Create time entry linked to issue via issue path.
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	stop := time.Now().UTC().Truncate(time.Second)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/time-entries", map[string]any{
		"start_time": start.Format(time.RFC3339),
		"stop_time":  stop.Format(time.RFC3339),
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTimeEntry via issue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var entry TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&entry)
	if entry.IssueID == nil || *entry.IssueID != issueID {
		t.Errorf("expected issue_id %q on entry, got %v", issueID, entry.IssueID)
	}
	if entry.StopTime == nil {
		t.Error("expected stop_time set for manual entry")
	}

	// List issue time entries.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/time-entries", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueTimeEntries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueTimeEntries: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var issueEntries []TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&issueEntries)
	if len(issueEntries) == 0 {
		t.Fatal("ListIssueTimeEntries: expected at least one entry")
	}
}
