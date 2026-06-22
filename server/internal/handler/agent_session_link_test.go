package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestTaskToResponseSessionID guards that the csc/Claude session id is
// serialized onto the UI-facing task response. The execution-log "View in
// CoStrict" deep-link depends on this field being present once the daemon has
// reported a session; it must be omitted (empty) until then. taskToResponse is
// pure, so this needs no database.
func TestTaskToResponseSessionID(t *testing.T) {
	t.Run("present when set", func(t *testing.T) {
		task := db.MulticaAgentTaskQueue{
			SessionID: pgtype.Text{String: "sess-abc-123", Valid: true},
			WorkDir:   pgtype.Text{String: "/home/user/project", Valid: true},
		}
		resp := taskToResponse(task)
		if resp.SessionID != "sess-abc-123" {
			t.Fatalf("expected SessionID %q, got %q", "sess-abc-123", resp.SessionID)
		}
		if resp.WorkDir != "/home/user/project" {
			t.Fatalf("expected WorkDir %q, got %q", "/home/user/project", resp.WorkDir)
		}
	})

	t.Run("empty when unset", func(t *testing.T) {
		task := db.MulticaAgentTaskQueue{
			SessionID: pgtype.Text{Valid: false},
		}
		resp := taskToResponse(task)
		if resp.SessionID != "" {
			t.Fatalf("expected empty SessionID, got %q", resp.SessionID)
		}
	})
}
