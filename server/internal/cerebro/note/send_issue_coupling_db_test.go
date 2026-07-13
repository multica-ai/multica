package note

// FIR-1852/FIR-1782 — a note unified to an issue via issue_id (the white-card
// link, no explicit cerebro_note_reference row) must still resolve as coupled to
// that issue, so "send comments" works. Before the resolveCoupling fallback,
// such a note 409'd with "couple it first" even though it clearly belonged to an
// issue. Explicit references keep precedence and are covered by the existing
// coupling tests.
//
// Skips cleanly when no test DB is reachable.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// makeNoteOnIssue creates a note artifact with issue_id set (the unified link)
// and no explicit reference row.
func makeNoteOnIssue(t *testing.T, ctx context.Context, issueID pgtype.UUID) pgtype.UUID {
	t.Helper()
	id, _ := uuid.NewV7()
	art, err := w3H.Upstream.CreateArtifact(ctx, db.CreateArtifactParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: w3WsID,
		IssueID:     issueID,
		Kind:        "note",
		Format:      "md",
		Title:       "unified note",
		Body:        "body",
		Metadata:    []byte("{}"),
		AuthorType:  "member",
		AuthorID:    w3UserA,
	})
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	if _, err := w3H.Cerebro.UpsertNote(ctx, cerebrodb.UpsertNoteParams{
		ArtifactID: art.ID,
		OwnerID:    w3UserA,
		Visibility: "private",
		Pinned:     false,
	}); err != nil {
		t.Fatalf("upsert note: %v", err)
	}
	return art.ID
}

func resolveReq(t *testing.T) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/notes/x/comments/send", nil)
	return r.WithContext(middleware.SetMemberContext(r.Context(), uuidStr(w3WsID), db.Member{}))
}

func TestResolveCouplingFallsBackToIssueID(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := makeIssue(t, ctx, "Send-comments unified target")
	noteID := makeNoteOnIssue(t, ctx, issueID)

	w := httptest.NewRecorder()
	object, refID, ok := w3H.resolveCoupling(w, resolveReq(t), noteID, uuidStr(issueID), "", "")
	if !ok {
		t.Fatalf("expected coupling resolved, got status %d: %s", w.Code, w.Body.String())
	}
	if object != couplingIssue || refID != uuidStr(issueID) {
		t.Fatalf("got (%q,%q), want (issue,%s)", object, refID, uuidStr(issueID))
	}
}

func TestResolveCouplingNoIssueNoRefsStill409(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	// A workspace-scoped note: no issue_id, no references → must still refuse.
	noteID := makeNote(t, ctx, "Orphan", "no coupling")

	w := httptest.NewRecorder()
	_, _, ok := w3H.resolveCoupling(w, resolveReq(t), noteID, "", "", "")
	if ok {
		t.Fatalf("expected refusal for an uncoupled note, got ok")
	}
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestResolveCouplingPrimaryIssueWinsOverLegacyReference(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	// The canonical issue_id wins over a stale legacy reference row.
	issueA := makeIssue(t, ctx, "issue A (issue_id)")
	issueB := makeIssue(t, ctx, "issue B (explicit ref)")
	noteID := makeNoteOnIssue(t, ctx, issueA)
	addIssueRef(t, ctx, noteID, issueB)

	w := httptest.NewRecorder()
	object, refID, ok := w3H.resolveCoupling(w, resolveReq(t), noteID, uuidStr(issueA), "", "")
	if !ok {
		t.Fatalf("expected resolved, got %d: %s", w.Code, w.Body.String())
	}
	if object != couplingIssue || refID != uuidStr(issueA) {
		t.Fatalf("primary issue should win: got (%q,%q), want (issue,%s)", object, refID, uuidStr(issueA))
	}
}
