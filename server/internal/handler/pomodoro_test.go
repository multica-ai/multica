package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

var pomodoroSchemaOnce sync.Once

func ensurePomodoroSchema(t *testing.T) {
	t.Helper()

	pomodoroSchemaOnce.Do(func() {
		_, err := testPool.Exec(context.Background(), `
			ALTER TABLE time_entry ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'manual';

			CREATE TABLE IF NOT EXISTS pomodoro_sessions (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
				workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
				phase TEXT NOT NULL DEFAULT 'work',
				phase_duration_seconds INT NOT NULL DEFAULT 1500,
				status TEXT NOT NULL DEFAULT 'idle',
				elapsed_seconds INT NOT NULL DEFAULT 0,
				started_at TIMESTAMPTZ,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
			);

			CREATE UNIQUE INDEX IF NOT EXISTS pomodoro_sessions_user_workspace_idx
				ON pomodoro_sessions (user_id, workspace_id);

			ALTER TABLE pomodoro_sessions
				ADD COLUMN IF NOT EXISTS pomodoro_count INT NOT NULL DEFAULT 0;
		`)
		if err != nil {
			t.Fatalf("ensure pomodoro schema: %v", err)
		}
	})
}

// TestPomodoroStartAndResume verifies start -> pause -> start resume transitions.
func TestPomodoroStartAndResume(t *testing.T) {
	ensurePomodoroSchema(t)

	ctx := context.Background()
	_, _ = testPool.Exec(ctx, `DELETE FROM pomodoro_sessions WHERE user_id = $1 AND workspace_id = $2`, testUserID, testWorkspaceID)
	ph := NewPomodoroHandler(testHandler.Queries)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM pomodoro_sessions WHERE user_id = $1 AND workspace_id = $2`, testUserID, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/pomodoro/start", nil)
	ph.StartPomodoro(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartPomodoro: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var started PomodoroSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if started.Status != "running" {
		t.Fatalf("StartPomodoro: expected running status, got %q", started.Status)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/pomodoro/pause", nil)
	ph.PausePomodoro(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PausePomodoro: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/pomodoro/start", nil)
	ph.StartPomodoro(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartPomodoro resume: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resumed PomodoroSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resumed); err != nil {
		t.Fatalf("decode resume response: %v", err)
	}
	if resumed.Status != "running" {
		t.Fatalf("StartPomodoro resume: expected running status, got %q", resumed.Status)
	}
	if resumed.StartedAt == nil {
		t.Fatal("StartPomodoro resume: expected non-nil started_at")
	}
}

// TestPomodoroCompleteIncrementsCount verifies completion increments pomodoro_count.
func TestPomodoroCompleteIncrementsCount(t *testing.T) {
	ensurePomodoroSchema(t)

	ctx := context.Background()
	_, _ = testPool.Exec(ctx, `DELETE FROM pomodoro_sessions WHERE user_id = $1 AND workspace_id = $2`, testUserID, testWorkspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM time_entry WHERE user_id = $1 AND workspace_id = $2`, testUserID, testWorkspaceID)
	ph := NewPomodoroHandler(testHandler.Queries)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM pomodoro_sessions WHERE user_id = $1 AND workspace_id = $2`, testUserID, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM time_entry WHERE user_id = $1 AND workspace_id = $2`, testUserID, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/pomodoro/start", nil)
	ph.StartPomodoro(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartPomodoro: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/pomodoro/complete", map[string]any{
		"long_break_after": 4,
	})
	ph.CompletePomodoro(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompletePomodoro: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT pomodoro_count
		FROM pomodoro_sessions
		WHERE user_id = $1 AND workspace_id = $2
	`, testUserID, testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("query pomodoro_count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected pomodoro_count=1, got %d", count)
	}

	var entryType string
	if err := testPool.QueryRow(ctx, `
		SELECT type
		FROM time_entry
		WHERE user_id = $1 AND workspace_id = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, testUserID, testWorkspaceID).Scan(&entryType); err != nil {
		t.Fatalf("query latest time entry: %v", err)
	}
	if entryType != "pomodoro" {
		t.Fatalf("expected latest time entry type pomodoro, got %q", entryType)
	}
}
