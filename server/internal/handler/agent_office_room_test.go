package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAgentOfficeRoom covers the office room an agent sits in: a new agent
// starts in general, a valid room moves it, an unknown or empty room is 400
// and leaves it where it was, and an update that omits the field keeps it.
func TestAgentOfficeRoom(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)
	agentID := createAgentOnRuntime(t, "office-room-test", claudeRuntimeID, "")

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
	})

	patch := func(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		return w
	}

	t.Run("a new agent starts in general", func(t *testing.T) {
		var room string
		if err := testPool.QueryRow(ctx, `SELECT office_room FROM agent WHERE id = $1`, agentID).Scan(&room); err != nil {
			t.Fatalf("read office_room: %v", err)
		}
		if room != "general" {
			t.Fatalf("new agent office_room = %q, want general", room)
		}
	})

	t.Run("a known room moves the agent", func(t *testing.T) {
		w := patch(t, map[string]any{"office_room": "planning"})
		if w.Code != http.StatusOK {
			t.Fatalf("move to planning: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["office_room"] != "planning" {
			t.Fatalf("response office_room = %v, want planning", resp["office_room"])
		}
	})

	t.Run("an omitted room keeps the agent where it is", func(t *testing.T) {
		w := patch(t, map[string]any{"name": "office-room-test-renamed"})
		if w.Code != http.StatusOK {
			t.Fatalf("rename: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var room string
		if err := testPool.QueryRow(ctx, `SELECT office_room FROM agent WHERE id = $1`, agentID).Scan(&room); err != nil {
			t.Fatalf("read office_room: %v", err)
		}
		if room != "planning" {
			t.Fatalf("rename moved the agent to %q, want planning", room)
		}
	})

	for _, bad := range []string{"", "sales", "Planning"} {
		t.Run("rejects room "+bad, func(t *testing.T) {
			w := patch(t, map[string]any{"office_room": bad})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("office_room=%q: expected 400, got %d: %s", bad, w.Code, w.Body.String())
			}
			var room string
			if err := testPool.QueryRow(ctx, `SELECT office_room FROM agent WHERE id = $1`, agentID).Scan(&room); err != nil {
				t.Fatalf("read office_room: %v", err)
			}
			if room != "planning" {
				t.Fatalf("rejected update changed the room to %q", room)
			}
		})
	}
}
