package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunSessionListJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/sessions/page" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("status") != "active" {
			t.Errorf("status=%q, want active", q.Get("status"))
		}
		if q.Get("limit") != "10" {
			t.Errorf("limit=%q, want 10", q.Get("limit"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sessions": []map[string]any{{
				"id":               "sess-1",
				"title":            "Daily sync",
				"status":           "active",
				"agent_id":         "agent-1",
				"last_activity_at": "2026-11-01T00:00:00Z",
				"workspace_id":     "ws-1",
				"creator_id":       "user-1",
				"has_unread":       false,
				"created_at":       "2026-10-01T00:00:00Z",
				"updated_at":       "2026-11-01T00:00:00Z",
			}},
			"total":    1,
			"has_more": false,
			"limit":    10,
			"offset":   0,
		})
	}))
	defer srv.Close()

	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().Int("limit", 10, "")
	cmd.Flags().String("status", "active", "")
	cmd.Flags().String("output", "json", "")
	out, err := captureStdout(t, func() error { return runSessionList(cmd, nil) })
	if err != nil {
		t.Fatalf("session list: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	sessions, _ := parsed["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}
}
