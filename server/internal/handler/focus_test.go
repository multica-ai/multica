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

var focusSchemaOnce sync.Once

func ensureFocusSchema(t *testing.T) {
	t.Helper()

	ensureTimeEntryLabelSchema(t)
	focusSchemaOnce.Do(func() {
		_, err := testPool.Exec(context.Background(), `
			CREATE TABLE IF NOT EXISTS focus_sessions (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
				user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
				mode TEXT NOT NULL CHECK (mode IN ('pomodoro', 'flowtime', 'quick_start')),
				phase TEXT NOT NULL CHECK (phase IN ('idle', 'focusing', 'paused', 'break_suggested', 'breaking', 'completed', 'abandoned')),
				preset TEXT,
				issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
				description TEXT,
				commitment_text TEXT,
				label_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
				first_started_at TIMESTAMPTZ,
				started_at TIMESTAMPTZ,
				paused_at TIMESTAMPTZ,
				elapsed_focus_seconds INT NOT NULL DEFAULT 0,
				suggested_break_seconds INT,
				status_reason TEXT,
				reason_note TEXT,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				UNIQUE (user_id, workspace_id)
			);

			CREATE TABLE IF NOT EXISTS focus_events (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
				user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
				focus_session_id UUID REFERENCES focus_sessions(id) ON DELETE SET NULL,
				event_type TEXT NOT NULL,
				reason TEXT,
				note TEXT,
				duration_seconds INT,
				metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now()
			);
		`)
		if err != nil {
			t.Fatalf("ensure focus schema: %v", err)
		}
	})
}

func cleanupFocusRows(t *testing.T) {
	t.Helper()
	_, _ = testPool.Exec(context.Background(), `DELETE FROM focus_events WHERE user_id = $1 AND workspace_id = $2`, testUserID, testWorkspaceID)
	_, _ = testPool.Exec(context.Background(), `DELETE FROM focus_sessions WHERE user_id = $1 AND workspace_id = $2`, testUserID, testWorkspaceID)
	_, _ = testPool.Exec(context.Background(), `DELETE FROM time_entry WHERE user_id = $1 AND workspace_id = $2 AND type IN ('flowtime', 'quick_start')`, testUserID, testWorkspaceID)
}

func TestFocusFlowtimeCompleteCreatesTimeEntryAndBreakSuggestion(t *testing.T) {
	ensureFocusSchema(t)
	cleanupFocusRows(t)
	t.Cleanup(func() { cleanupFocusRows(t) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/focus/start", map[string]any{
		"mode":              "flowtime",
		"preset":            "flowtime_default",
		"commitment_text":   "Open the failing CI log",
		"resistance_reason": "unclear_next_step",
	})
	testHandler.StartFocus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartFocus: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	startedAt := time.Now().Add(-35 * time.Minute).UTC()
	if _, err := testPool.Exec(context.Background(), `
		UPDATE focus_sessions
		SET first_started_at = $1, started_at = $1
		WHERE user_id = $2 AND workspace_id = $3
	`, startedAt, testUserID, testWorkspaceID); err != nil {
		t.Fatalf("backdate focus session: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/focus/complete", map[string]any{
		"end_reason": "completed",
	})
	testHandler.CompleteFocus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteFocus: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		Session focusSessionResponse `json:"session"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode complete response: %v", err)
	}
	if response.Session.Phase != focusPhaseBreakSuggested {
		t.Fatalf("expected break_suggested phase, got %q", response.Session.Phase)
	}
	if response.Session.SuggestedBreakSeconds == nil || *response.Session.SuggestedBreakSeconds != 10*60 {
		t.Fatalf("expected 10 minute break suggestion, got %v", response.Session.SuggestedBreakSeconds)
	}

	var entryType string
	var duration int64
	if err := testPool.QueryRow(context.Background(), `
		SELECT type, duration_seconds
		FROM time_entry
		WHERE user_id = $1 AND workspace_id = $2 AND type = 'flowtime'
		ORDER BY created_at DESC
		LIMIT 1
	`, testUserID, testWorkspaceID).Scan(&entryType, &duration); err != nil {
		t.Fatalf("query flowtime entry: %v", err)
	}
	if entryType != "flowtime" {
		t.Fatalf("expected flowtime entry, got %q", entryType)
	}
	if duration < 34*60 {
		t.Fatalf("expected duration to use actual elapsed focus time, got %d", duration)
	}

	var eventCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM focus_events
		WHERE user_id = $1
		  AND workspace_id = $2
		  AND event_type IN ('focus_started', 'focus_completed', 'break_suggested')
	`, testUserID, testWorkspaceID).Scan(&eventCount); err != nil {
		t.Fatalf("count focus events: %v", err)
	}
	if eventCount != 3 {
		t.Fatalf("expected 3 focus events, got %d", eventCount)
	}
}

func TestFocusBreakSkipDoesNotCreateTimeEntry(t *testing.T) {
	ensureFocusSchema(t)
	cleanupFocusRows(t)
	t.Cleanup(func() { cleanupFocusRows(t) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/focus/start", map[string]any{"mode": "flowtime"})
	testHandler.StartFocus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartFocus: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/focus/complete", map[string]any{"end_reason": "completed"})
	testHandler.CompleteFocus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteFocus: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var before int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM time_entry WHERE user_id = $1 AND workspace_id = $2
	`, testUserID, testWorkspaceID).Scan(&before); err != nil {
		t.Fatalf("count entries before skip: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/focus/break/skip", map[string]any{"reason": "not_needed"})
	testHandler.SkipFocusBreak(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SkipFocusBreak: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var after int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM time_entry WHERE user_id = $1 AND workspace_id = $2
	`, testUserID, testWorkspaceID).Scan(&after); err != nil {
		t.Fatalf("count entries after skip: %v", err)
	}
	if after != before {
		t.Fatalf("break skip should not create a time entry: before=%d after=%d", before, after)
	}
}
