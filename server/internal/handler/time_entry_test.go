package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var timeEntryLabelSchemaOnce sync.Once

func ensureTimeEntryLabelSchema(t *testing.T) {
	t.Helper()

	timeEntryLabelSchemaOnce.Do(func() {
		_, err := testPool.Exec(context.Background(), `
			CREATE TABLE IF NOT EXISTS time_entry_label (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
				name TEXT NOT NULL,
				color TEXT NOT NULL DEFAULT '#6b7280',
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				CONSTRAINT time_entry_label_workspace_name_unique UNIQUE (workspace_id, name)
			);

			CREATE TABLE IF NOT EXISTS time_entry_to_label (
				time_entry_id UUID NOT NULL REFERENCES time_entry(id) ON DELETE CASCADE,
				label_id UUID NOT NULL REFERENCES time_entry_label(id) ON DELETE CASCADE,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				PRIMARY KEY (time_entry_id, label_id)
			);

			CREATE INDEX IF NOT EXISTS idx_time_entry_label_workspace ON time_entry_label (workspace_id);
			CREATE INDEX IF NOT EXISTS idx_time_entry_to_label_label ON time_entry_to_label (label_id);
		`)
		if err != nil {
			t.Fatalf("ensure time entry label schema: %v", err)
		}
	})
}

func countUserTimeEntriesSince(t *testing.T, start time.Time) int {
	t.Helper()

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM time_entry
		WHERE workspace_id = $1
		  AND user_id = $2
		  AND start_time >= $3
	`, testWorkspaceID, testUserID, start).Scan(&count); err != nil {
		t.Fatalf("count time entries: %v", err)
	}
	return count
}

// TestTimeEntryLifecycle tests the full start → list → stop → delete flow.
func TestTimeEntryLifecycle(t *testing.T) {
	ensureTimeEntryLabelSchema(t)

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
	ensureTimeEntryLabelSchema(t)

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
	ensureTimeEntryLabelSchema(t)

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

// TestSwitchTimeEntryStopsCurrentAndStartsNew verifies explicit switch semantics.
func TestSwitchTimeEntryStopsCurrentAndStartsNew(t *testing.T) {
	ensureTimeEntryLabelSchema(t)

	ctx := context.Background()

	startA := time.Now().UTC().Add(-15 * time.Minute).Truncate(time.Second)
	startB := time.Now().UTC().Truncate(time.Second)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/time-entries", map[string]any{
		"start_time":  startA.Format(time.RFC3339),
		"description": "Existing timer",
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed timer: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var oldEntry TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&oldEntry)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM time_entry WHERE user_id = $1 AND start_time >= $2`, testUserID, startA)
		testPool.Exec(ctx, `DELETE FROM running_timer WHERE user_id = $1`, testUserID)
	})

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/time-entries/switch", map[string]any{
		"start_time":  startB.Format(time.RFC3339),
		"description": "Switched timer",
	})
	testHandler.SwitchTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("SwitchTimeEntry: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var startedEntry TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&startedEntry)

	// Verify the new timer was started.
	if startedEntry.ID == "" {
		t.Error("started entry should have non-empty ID")
	}
	if startedEntry.StopTime != nil {
		t.Error("started entry should have nil stop_time (running)")
	}
	if startedEntry.DurationSeconds >= 0 {
		t.Errorf("started entry should have negative duration while running, got %d", startedEntry.DurationSeconds)
	}

	// Verify the old timer was actually stopped.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/time-entries", nil)
	testHandler.ListTimeEntries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListTimeEntries after switch: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var entries []TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&entries)

	foundOld := false
	for _, entry := range entries {
		if entry.ID != oldEntry.ID {
			continue
		}
		foundOld = true
		if entry.StopTime == nil {
			t.Error("stopped entry should have stop_time set")
		}
		if entry.DurationSeconds < 0 {
			t.Errorf("stopped entry should have non-negative duration, got %d", entry.DurationSeconds)
		}
		break
	}
	if !foundOld {
		t.Fatalf("expected old entry %q in list after switch", oldEntry.ID)
	}
}

func TestCreateLiveTimeEntryWithInvalidLabelsIsAtomic(t *testing.T) {
	ensureTimeEntryLabelSchema(t)

	start := time.Now().UTC().Truncate(time.Second)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM time_entry WHERE user_id = $1 AND start_time >= $2`, testUserID, start)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM running_timer WHERE user_id = $1`, testUserID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/time-entries", map[string]any{
		"start_time": start.Format(time.RFC3339),
		"label_ids":  []string{"not-a-uuid"},
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTimeEntry with invalid labels: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	if got := countUserTimeEntriesSince(t, start); got != 0 {
		t.Fatalf("expected no persisted entries after label failure, got %d", got)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/time-entries/current", nil)
	testHandler.GetCurrentTimeEntry(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetCurrentTimeEntry: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var current TimeEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&current); err != nil {
		t.Fatalf("decode current timer: %v", err)
	}
	if current.ID != "" {
		t.Fatalf("expected no running timer after label failure, got %q", current.ID)
	}
}

func TestCreateHistoricalTimeEntryWithInvalidLabelsIsAtomic(t *testing.T) {
	ensureTimeEntryLabelSchema(t)

	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	stop := start.Add(30 * time.Minute)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM time_entry WHERE user_id = $1 AND start_time >= $2`, testUserID, start)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/time-entries", map[string]any{
		"start_time": start.Format(time.RFC3339),
		"stop_time":  stop.Format(time.RFC3339),
		"label_ids":  []string{"not-a-uuid"},
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTimeEntry historical invalid labels: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	if got := countUserTimeEntriesSince(t, start); got != 0 {
		t.Fatalf("expected no persisted manual entries after label failure, got %d", got)
	}
}

func TestSwitchTimeEntryWithInvalidLabelsIsAtomic(t *testing.T) {
	ensureTimeEntryLabelSchema(t)

	startA := time.Now().UTC().Add(-20 * time.Minute).Truncate(time.Second)
	startB := time.Now().UTC().Truncate(time.Second)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM time_entry WHERE user_id = $1 AND start_time >= $2`, testUserID, startA)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM running_timer WHERE user_id = $1`, testUserID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/time-entries", map[string]any{
		"start_time":  startA.Format(time.RFC3339),
		"description": "Existing timer",
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed timer: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var original TimeEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&original); err != nil {
		t.Fatalf("decode original timer: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/time-entries/switch", map[string]any{
		"start_time": startB.Format(time.RFC3339),
		"label_ids":  []string{"not-a-uuid"},
	})
	testHandler.SwitchTimeEntry(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("SwitchTimeEntry invalid labels: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	if got := countUserTimeEntriesSince(t, startA); got != 1 {
		t.Fatalf("expected original timer only after failed switch, got %d entries", got)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/time-entries/current", nil)
	testHandler.GetCurrentTimeEntry(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetCurrentTimeEntry after failed switch: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var current TimeEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&current); err != nil {
		t.Fatalf("decode current timer: %v", err)
	}
	if current.ID != original.ID {
		t.Fatalf("expected original timer %q to keep running, got %q", original.ID, current.ID)
	}
}

func TestUpdateTimeEntryWithInvalidLabelsIsAtomic(t *testing.T) {
	ensureTimeEntryLabelSchema(t)

	start := time.Now().UTC().Add(-90 * time.Minute).Truncate(time.Second)
	stop := start.Add(25 * time.Minute)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM time_entry WHERE user_id = $1 AND start_time >= $2`, testUserID, start)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/time-entries", map[string]any{
		"start_time":  start.Format(time.RFC3339),
		"stop_time":   stop.Format(time.RFC3339),
		"description": "Original description",
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed historical entry: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var original TimeEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&original); err != nil {
		t.Fatalf("decode original entry: %v", err)
	}

	updatedDescription := "Updated description"
	updatedStart := start.Add(-10 * time.Minute).Format(time.RFC3339)

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/time-entries/"+original.ID, map[string]any{
		"description": &updatedDescription,
		"start_time":  &updatedStart,
		"label_ids":   []string{"not-a-uuid"},
	})
	req = withURLParam(req, "entry_id", original.ID)
	testHandler.UpdateTimeEntry(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateTimeEntry with invalid labels: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/time-entries", nil)
	testHandler.ListTimeEntries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListTimeEntries after failed update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []TimeEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode entries: %v", err)
	}

	for _, entry := range entries {
		if entry.ID != original.ID {
			continue
		}
		if entry.Description == nil || *entry.Description != "Original description" {
			t.Fatalf("expected original description to remain, got %v", entry.Description)
		}
		if entry.StartTime != original.StartTime {
			t.Fatalf("expected original start_time %q to remain, got %q", original.StartTime, entry.StartTime)
		}
		return
	}

	t.Fatalf("updated entry %q not found", original.ID)
}

// TestCreateHistoricalTimeEntryRequiresOverlapConfirmation verifies overlap detection.
func TestCreateHistoricalTimeEntryRequiresOverlapConfirmation(t *testing.T) {
	ensureTimeEntryLabelSchema(t)

	baseStart := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	baseStop := baseStart.Add(45 * time.Minute)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/time-entries", map[string]any{
		"start_time":  baseStart.Format(time.RFC3339),
		"stop_time":   baseStop.Format(time.RFC3339),
		"description": "Existing historical entry",
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed historical entry: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/time-entries", map[string]any{
		"start_time":      baseStart.Add(15 * time.Minute).Format(time.RFC3339),
		"stop_time":       baseStop.Add(15 * time.Minute).Format(time.RFC3339),
		"description":     "Overlapping entry",
		"confirm_overlap": false,
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateTimeEntry overlap: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateHistoricalTimeEntryRequiresOverlapConfirmation verifies overlap detection when updating.
func TestUpdateHistoricalTimeEntryRequiresOverlapConfirmation(t *testing.T) {
	ensureTimeEntryLabelSchema(t)

	ctx := context.Background()

	baseStart := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	baseStop := baseStart.Add(45 * time.Minute)

	// Create first historical entry.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/time-entries", map[string]any{
		"start_time":  baseStart.Format(time.RFC3339),
		"stop_time":   baseStop.Format(time.RFC3339),
		"description": "Base entry",
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create base entry: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Create second historical entry with different time range.
	secondStart := baseStart.Add(2 * time.Hour)
	secondStop := secondStart.Add(30 * time.Minute)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/time-entries", map[string]any{
		"start_time":  secondStart.Format(time.RFC3339),
		"stop_time":   secondStop.Format(time.RFC3339),
		"description": "Second entry",
	})
	testHandler.CreateTimeEntry(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create second entry: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var secondEntry TimeEntryResponse
	json.NewDecoder(w.Body).Decode(&secondEntry)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM time_entry WHERE user_id = $1 AND start_time >= $2`, testUserID, baseStart)
	})

	// Update second entry to overlap with the first entry without confirmation.
	overlappingStart := baseStart.Add(15 * time.Minute).Format(time.RFC3339)
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/time-entries/"+secondEntry.ID, map[string]any{
		"start_time":      &overlappingStart,
		"confirm_overlap": false,
	})
	req = withURLParam(req, "entry_id", secondEntry.ID)
	testHandler.UpdateTimeEntry(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("UpdateTimeEntry overlap: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var overlapResp TimeEntryOverlapResponse
	json.NewDecoder(w.Body).Decode(&overlapResp)
	if overlapResp.Code != "time_entry_overlap" {
		t.Errorf("expected code 'time_entry_overlap', got %q", overlapResp.Code)
	}
	if len(overlapResp.Conflicts) == 0 {
		t.Error("expected at least one conflict in response")
	}
}
