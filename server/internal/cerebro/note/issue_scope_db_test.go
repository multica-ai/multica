package note

// FIR-1852 — a note created in an issue context is unified with documents: the
// underlying artifact carries the same issue_id a document does, so the note
// shows in the issue's document list (ListArtifactsByIssue) and the editor
// renders "on FIR-XXX". These tests drive the real CreateNote handler against
// the shared test DB and skip cleanly when no DB is reachable.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createNoteHTTP drives the CreateNote handler with a JSON body and the
// workspace + user context the handler reads (mirrors sendRequest).
func createNoteHTTP(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/notes", strings.NewReader(body))
	r.Header.Set("X-User-ID", uuidStr(w3UserA))
	ctx := middleware.SetMemberContext(r.Context(), uuidStr(w3WsID), db.Member{})
	w := httptest.NewRecorder()
	h.CreateNote(w, r.WithContext(ctx))
	return w
}

func TestCreateNoteWithIssueScope(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := makeIssue(t, ctx, "Note scope target")

	w := createNoteHTTP(t, w3H, `{"title":"Scoped note","body":"x","issue_id":"`+uuidStr(issueID)+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create note: status %d, body %s", w.Code, w.Body.String())
	}
	var resp NoteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.IssueID == nil || *resp.IssueID != uuidStr(issueID) {
		t.Fatalf("response issue_id = %v, want %s", resp.IssueID, uuidStr(issueID))
	}

	// The note must surface in the issue's document list exactly like a document.
	arts, err := w3H.Upstream.ListArtifactsByIssue(ctx, db.ListArtifactsByIssueParams{
		IssueID:     issueID,
		WorkspaceID: w3WsID,
	})
	if err != nil {
		t.Fatalf("list artifacts by issue: %v", err)
	}
	var found bool
	for _, a := range arts {
		if uuidStr(a.ID) == resp.ID {
			found = true
			if a.Kind != "note" {
				t.Fatalf("artifact kind = %q, want note", a.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("note %s not found in issue %s document list", resp.ID, uuidStr(issueID))
	}
}

func TestCreateNoteWithoutIssueScope(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	// Omitting issue_id keeps the old workspace-scoped behaviour: no issue link.
	w := createNoteHTTP(t, w3H, `{"title":"Loose note","body":"y"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create note: status %d, body %s", w.Code, w.Body.String())
	}
	var resp NoteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.IssueID != nil {
		t.Fatalf("response issue_id = %v, want nil for a workspace-scoped note", *resp.IssueID)
	}
}

func TestCreateNoteUnknownIssueIs404(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	// A well-formed but non-existent issue id is a 404, never a silent drop.
	w := createNoteHTTP(t, w3H, `{"title":"x","issue_id":"00000000-0000-0000-0000-0000000000ff"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown issue: status %d, want 404, body %s", w.Code, w.Body.String())
	}
}
