package note

// FIR-3628 — GET /api/notes/{id} must resolve a plain DOCUMENT, not only a
// Notes-feature note. Documents created through the artifact API (multica
// artifact create / document create) carry no cerebro_note row, and the old
// CanUserSeeNote gate (FROM cerebro_note) answered 404 for every one of them —
// while the same id opened fine in the web app and resolved through the
// artifact API. Same DB harness as wave3_db_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// getNoteRequest drives the GetNote handler as the given user.
func getNoteRequest(t *testing.T, noteID, userID pgtype.UUID) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/notes/"+uuidStr(noteID), nil)
	r.Header.Set("X-User-ID", uuidStr(userID))
	ctx := middleware.SetMemberContext(r.Context(), uuidStr(w3WsID), db.Member{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidStr(noteID))
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	w := httptest.NewRecorder()
	w3H.GetNote(w, r.WithContext(ctx))
	return w
}

// TestGetNoteResolvesAgentDocument is the regression: an agent-authored plain
// document at the workspace root reads back through the note API instead of
// 404ing, and reports a sane default note state.
func TestGetNoteResolvesAgentDocument(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	docID := makeVersionedDocument(t, ctx, "plan", "Agent Plan", "the plan body", "agent", w3UserA)

	w := getNoteRequest(t, docID, w3UserA)
	if w.Code != http.StatusOK {
		t.Fatalf("read agent document: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp NoteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Title != "Agent Plan" || resp.Body != "the plan body" {
		t.Fatalf("wrong document returned: title=%q body=%q", resp.Title, resp.Body)
	}
	if resp.Visibility != "workspace" {
		t.Fatalf("document visibility = %q, want workspace", resp.Visibility)
	}
	if resp.Pinned {
		t.Fatalf("document pinned = true, want false")
	}
}

// TestGetNoteDocumentAuthorCanEdit pins the write flag for documents: the member
// author of a root document gets can_edit=true (document rule), an unrelated
// member reading the same root document does not get write access.
func TestGetNoteDocumentAuthorCanEdit(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	docID := makeVersionedDocument(t, ctx, "report", "Report", "body", "member", w3UserA)

	var authorResp NoteResponse
	w := getNoteRequest(t, docID, w3UserA)
	if w.Code != http.StatusOK {
		t.Fatalf("author read: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &authorResp); err != nil {
		t.Fatalf("decode author response: %v", err)
	}
	if !authorResp.CanEdit {
		t.Fatalf("member author can_edit = false, want true")
	}
	if authorResp.OwnerID != uuidStr(w3UserA) {
		t.Fatalf("document owner = %q, want the member author %q", authorResp.OwnerID, uuidStr(w3UserA))
	}

	var otherResp NoteResponse
	w = getNoteRequest(t, docID, w3UserB)
	if w.Code != http.StatusOK {
		t.Fatalf("other member read: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &otherResp); err != nil {
		t.Fatalf("decode other response: %v", err)
	}
	if otherResp.CanEdit {
		t.Fatalf("unrelated member can_edit = true on a root document, want false")
	}
}

// TestGetNotePrivateNoteStillHidden proves the unified read gate did not loosen
// note visibility: a private note owned by userA stays 404 for userB.
func TestGetNotePrivateNoteStillHidden(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Private", "secret")

	if w := getNoteRequest(t, noteID, w3UserA); w.Code != http.StatusOK {
		t.Fatalf("owner read: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if w := getNoteRequest(t, noteID, w3UserB); w.Code != http.StatusNotFound {
		t.Fatalf("non-owner read of private note: got %d, want 404 (body: %s)", w.Code, w.Body.String())
	}
}

// TestCanUserReadArtifactAsNoteDocument checks the query itself: a document with
// no cerebro_note row is readable by any workspace member (folder access is the
// only gate, and a root document has no folder to restrict it).
func TestCanUserReadArtifactAsNoteDocument(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	docID := makeVersionedDocument(t, ctx, "note", "Doc", "body", "agent", w3UserA)

	for _, u := range []pgtype.UUID{w3UserA, w3UserB} {
		ok, err := w3H.Cerebro.CanUserReadArtifactAsNote(ctx, cerebrodb.CanUserReadArtifactAsNoteParams{ID: docID, PUser: u})
		if err != nil {
			t.Fatalf("CanUserReadArtifactAsNote: %v", err)
		}
		if !ok {
			t.Fatalf("workspace member cannot read a root document, want true")
		}
	}
}
