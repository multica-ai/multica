package note

// FIR-2810 follow-up — a save that changes nothing must never 409. The
// editor's blur + debounce autosaves (and the author-code stamper) can
// re-send an identical body while an earlier save's response is still in
// flight; the stale base_updated_at then made UpdateNote answer 409 and the
// frontend opened a bogus "two people edited this note" merge dialog for a
// user typing alone. These tests pin the no-op guard and that a REAL
// concurrent change still conflicts. Same DB harness as wave3_db_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// updateRequest drives the UpdateNote handler as userA with the given JSON body.
func updateRequest(t *testing.T, noteID pgtype.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "/api/notes/"+uuidStr(noteID), strings.NewReader(body))
	r.Header.Set("X-User-ID", uuidStr(w3UserA))
	ctx := middleware.SetMemberContext(r.Context(), uuidStr(w3WsID), db.Member{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidStr(noteID))
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	w := httptest.NewRecorder()
	w3H.UpdateNote(w, r.WithContext(ctx))
	return w
}

func TestUpdateNoteNoopSaveNeverConflicts(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Noop", "line one\nline two")

	artifact, err := w3H.Upstream.GetArtifact(ctx, db.GetArtifactParams{ID: noteID, WorkspaceID: w3WsID})
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}

	// Identical body + a stale base: no information can be lost, so the save
	// must succeed and hand back the current state (re-syncing the client).
	payload, _ := json.Marshal(map[string]string{
		"body":            "line one\nline two",
		"base_updated_at": "2000-01-01T00:00:00Z",
	})
	w := updateRequest(t, noteID, string(payload))
	if w.Code != http.StatusOK {
		t.Fatalf("no-op save with stale base: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp NoteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Body != "line one\nline two" {
		t.Fatalf("no-op save changed the body: %q", resp.Body)
	}
	if resp.UpdatedAt != tsStr(artifact.UpdatedAt) {
		t.Fatalf("no-op save bumped updated_at: got %s, want %s", resp.UpdatedAt, tsStr(artifact.UpdatedAt))
	}

	// A REAL change on a stale base must still conflict.
	payload, _ = json.Marshal(map[string]string{
		"body":            "line one\nline two\nline three",
		"base_updated_at": "2000-01-01T00:00:00Z",
	})
	w = updateRequest(t, noteID, string(payload))
	if w.Code != http.StatusConflict {
		t.Fatalf("changed body with stale base: got %d, want 409 (body: %s)", w.Code, w.Body.String())
	}

	// The same change with the CURRENT base saves normally.
	payload, _ = json.Marshal(map[string]string{
		"body":            "line one\nline two\nline three",
		"base_updated_at": tsStr(artifact.UpdatedAt),
	})
	w = updateRequest(t, noteID, string(payload))
	if w.Code != http.StatusOK {
		t.Fatalf("changed body with current base: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
}
