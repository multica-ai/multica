package note

// FIR-3102 — create an issue from a single note comment. These tests exercise
// the CreateIssueFromComment handler against a real DB (for the comment row +
// issue_id linking), with a fake IssueCreator standing in for the IssueService
// adapter so the note package stays free of the service layer. They skip
// cleanly when no DB is reachable (same pattern as wave3_db_test.go).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeIssueCreator records its input and returns a canned issue identity, so the
// handler's own logic (title derivation, comment linking, response shape) can be
// asserted without the real service layer.
type fakeIssueCreator struct {
	lastIn            IssueFromCommentInput
	calls             int
	issueID           pgtype.UUID
	number            int32
	validationStatus  int
	validationMessage string
}

func (f *fakeIssueCreator) CreateIssueFromNoteComment(_ context.Context, in IssueFromCommentInput) (IssueFromCommentResult, error) {
	f.calls++
	f.lastIn = in
	return IssueFromCommentResult{IssueID: f.issueID, Number: f.number}, nil
}

func (f *fakeIssueCreator) ValidateIssueFromNoteCommentAssignee(_ context.Context, _ *http.Request, _ pgtype.UUID, _ pgtype.UUID, _ string, _ pgtype.UUID) (int, string) {
	return f.validationStatus, f.validationMessage
}

func createIssueRequest(t *testing.T, h *Handler, noteID, commentID pgtype.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/notes/"+uuidStr(noteID)+"/comments/"+uuidStr(commentID)+"/create-issue", strings.NewReader(body))
	r.Header.Set("X-User-ID", uuidStr(w3UserA))
	ctx := middleware.SetMemberContext(r.Context(), uuidStr(w3WsID), db.Member{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidStr(noteID))
	rctx.URLParams.Add("commentId", uuidStr(commentID))
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	w := httptest.NewRecorder()
	h.CreateIssueFromComment(w, r.WithContext(ctx))
	return w
}

func TestCreateIssueFromComment(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Note with actionable comment", "body")
	comment, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID: noteID, Kind: "comment",
		Body:       "First line becomes title\nsecond line ignored",
		AuthorType: "member", AuthorID: w3UserA,
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// Reuse a real issue id so the FK on cerebro_note_comment.issue_id holds.
	newIssue := makeIssue(t, ctx, "placeholder")
	fake := &fakeIssueCreator{issueID: newIssue, number: 42}
	h := &Handler{Upstream: w3H.Upstream, Cerebro: w3H.Cerebro, Pool: w3Pool, Issues: fake}

	// No title in the request → derived from the comment's first line.
	w := createIssueRequest(t, h, noteID, comment.ID, "{}")
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	var resp createIssueFromCommentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.IssueID != uuidStr(newIssue) {
		t.Fatalf("issue_id = %s, want %s", resp.IssueID, uuidStr(newIssue))
	}
	if resp.Number != 42 {
		t.Fatalf("number = %d, want 42", resp.Number)
	}
	if fake.lastIn.Title != "First line becomes title" {
		t.Fatalf("derived title = %q, want the comment's first line", fake.lastIn.Title)
	}
	if uuidStr(fake.lastIn.CommentID) != uuidStr(comment.ID) {
		t.Fatalf("origin comment id not passed through to the creator")
	}
	// The comment now carries issue_id — in the response and in the DB.
	if resp.Comment.IssueID == nil || *resp.Comment.IssueID != uuidStr(newIssue) {
		t.Fatalf("response comment.issue_id not linked: %+v", resp.Comment.IssueID)
	}
	reread, err := w3H.Cerebro.GetNoteComment(ctx, comment.ID)
	if err != nil {
		t.Fatalf("reread comment: %v", err)
	}
	if uuidStr(reread.IssueID) != uuidStr(newIssue) {
		t.Fatalf("DB comment.issue_id = %s, want %s", uuidStr(reread.IssueID), uuidStr(newIssue))
	}
	refs, err := w3H.Cerebro.ListReferencesByIssue(ctx, newIssue)
	if err != nil {
		t.Fatalf("list issue references: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("issue references = %d, want 1 backlink to the source comment", len(refs))
	}
	ref := refs[0]
	if ref.Object != "note_comment" || ref.RefID != uuidStr(comment.ID) {
		t.Fatalf("reference = object %q ref_id %q, want note_comment/%s", ref.Object, ref.RefID, uuidStr(comment.ID))
	}
	var meta map[string]string
	if err := json.Unmarshal(ref.Metadata, &meta); err != nil {
		t.Fatalf("decode reference metadata: %v", err)
	}
	if meta["note_id"] != uuidStr(noteID) || meta["comment_id"] != uuidStr(comment.ID) {
		t.Fatalf("reference metadata = %+v, want note_id/comment_id backlink", meta)
	}
}

func TestCreateIssueFromCommentExplicitTitle(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Note", "body")
	comment, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID: noteID, Kind: "comment", Body: "raw comment text",
		AuthorType: "member", AuthorID: w3UserA,
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	newIssue := makeIssue(t, ctx, "ph")
	fake := &fakeIssueCreator{issueID: newIssue, number: 7}
	h := &Handler{Upstream: w3H.Upstream, Cerebro: w3H.Cerebro, Issues: fake}

	w := createIssueRequest(t, h, noteID, comment.ID, `{"title":"Chosen title"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	if fake.lastIn.Title != "Chosen title" {
		t.Fatalf("title = %q, want the explicit one", fake.lastIn.Title)
	}
	if fake.lastIn.Description != "raw comment text" {
		t.Fatalf("description = %q, want the comment body", fake.lastIn.Description)
	}
}

func TestCreateIssueFromCommentIsIdempotentAfterCommentIsLinked(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Note", "body")
	comment, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID: noteID, Kind: "comment", Body: "Create once", AuthorType: "member", AuthorID: w3UserA,
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	newIssue := makeIssue(t, ctx, "created issue")
	fake := &fakeIssueCreator{issueID: newIssue, number: 9}
	h := &Handler{Upstream: w3H.Upstream, Cerebro: w3H.Cerebro, Issues: fake}

	first := createIssueRequest(t, h, noteID, comment.ID, "{}")
	if first.Code != http.StatusCreated {
		t.Fatalf("first status %d: %s", first.Code, first.Body.String())
	}
	second := createIssueRequest(t, h, noteID, comment.ID, `{"title":"A different title must not create a duplicate"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second status %d: %s", second.Code, second.Body.String())
	}
	if fake.calls != 1 {
		t.Fatalf("issue creator calls = %d, want 1", fake.calls)
	}
}

func TestCreateIssueFromCommentRejectsInvalidAssignee(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Note", "body")
	comment, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID: noteID, Kind: "comment", Body: "Assign safely", AuthorType: "member", AuthorID: w3UserA,
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	fake := &fakeIssueCreator{validationStatus: http.StatusForbidden, validationMessage: "cannot assign to private agent"}
	h := &Handler{Upstream: w3H.Upstream, Cerebro: w3H.Cerebro, Issues: fake}

	w := createIssueRequest(t, h, noteID, comment.ID, `{"assignee_type":"agent","assignee_id":"`+uuidStr(w3UserB)+`"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", w.Code, w.Body.String())
	}
	if fake.calls != 0 {
		t.Fatalf("issue creator called %d times, want 0", fake.calls)
	}
}

// A nil Issues seam must degrade to 503, not panic.
func TestCreateIssueFromCommentNoCreator(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Note", "body")
	comment, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID: noteID, Kind: "comment", Body: "x", AuthorType: "member", AuthorID: w3UserA,
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	h := &Handler{Upstream: w3H.Upstream, Cerebro: w3H.Cerebro} // Issues nil
	w := createIssueRequest(t, h, noteID, comment.ID, "{}")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", w.Code)
	}
}
