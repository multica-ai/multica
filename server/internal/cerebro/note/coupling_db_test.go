package note

// FIR-1621 — note↔destination coupling + send-to-agent dispatch. These tests
// exercise the reverse-lookup query and the SendComments handler against a real
// DB, with a fake task dispatcher standing in for the agent runtime. They skip
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

// fakeDispatcher records dispatch calls so a test can assert what was triggered
// without a real agent runtime.
type fakeDispatcher struct {
	mentionAgents []pgtype.UUID
	chatTasks     int
}

func (f *fakeDispatcher) EnqueueTaskForMention(_ context.Context, _ db.Issue, agentID pgtype.UUID, _ pgtype.UUID) (db.AgentTaskQueue, error) {
	f.mentionAgents = append(f.mentionAgents, agentID)
	return db.AgentTaskQueue{}, nil
}

func (f *fakeDispatcher) EnqueueChatTask(_ context.Context, _ db.ChatSession) (db.AgentTaskQueue, error) {
	f.chatTasks++
	return db.AgentTaskQueue{}, nil
}

// addIssueRef couples a note to an issue via the same reference machinery the
// product uses (object='issue').
func addIssueRef(t *testing.T, ctx context.Context, noteID, issueID pgtype.UUID) {
	t.Helper()
	if _, err := w3H.Cerebro.UpsertNoteReference(ctx, cerebrodb.UpsertNoteReferenceParams{
		NoteID:        noteID,
		Object:        couplingIssue,
		RefID:         uuidStr(issueID),
		Metadata:      []byte("{}"),
		CreatedByType: "member",
		CreatedByID:   w3UserA,
	}); err != nil {
		t.Fatalf("add issue ref: %v", err)
	}
}

// makeIssue inserts a minimal issue owned by userA and returns its id.
func makeIssue(t *testing.T, ctx context.Context, title string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	// number is unique per workspace (NOT NULL DEFAULT 0); a raw insert must pick
	// the next free number itself since it bypasses the CreateIssue counter.
	if err := w3Pool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
		 VALUES ($1, $2, 'member', $3, (SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id = $1))
		 RETURNING id`,
		w3WsID, title, w3UserA).Scan(&id); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return id
}

// sendRequest drives the SendComments handler with the given note + body,
// injecting the workspace + user + chi {id} the handler reads.
func sendRequest(t *testing.T, h *Handler, noteID pgtype.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/notes/"+uuidStr(noteID)+"/comments/send", strings.NewReader(body))
	r.Header.Set("X-User-ID", uuidStr(w3UserA))
	ctx := middleware.SetMemberContext(r.Context(), uuidStr(w3WsID), db.Member{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidStr(noteID))
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	w := httptest.NewRecorder()
	h.SendComments(w, r.WithContext(ctx))
	return w
}

func TestListNotesReferencingObject(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := makeIssue(t, ctx, "Coupling target")

	// A workspace-visible note coupled to the issue: visible to anyone.
	openNote := makeNote(t, ctx, "Open", "shared note")
	if err := w3H.Cerebro.SetNoteVisibility(ctx, cerebrodb.SetNoteVisibilityParams{ArtifactID: openNote, Visibility: "workspace"}); err != nil {
		t.Fatalf("set visibility: %v", err)
	}
	addIssueRef(t, ctx, openNote, issueID)

	// A private note (owned by A) coupled to the same issue: only A sees it.
	privNote := makeNote(t, ctx, "Private", "secret note")
	addIssueRef(t, ctx, privNote, issueID)

	// A note coupled to a DIFFERENT issue must not appear.
	otherIssue := makeIssue(t, ctx, "Other")
	otherNote := makeNote(t, ctx, "Other", "elsewhere")
	if err := w3H.Cerebro.SetNoteVisibility(ctx, cerebrodb.SetNoteVisibilityParams{ArtifactID: otherNote, Visibility: "workspace"}); err != nil {
		t.Fatalf("set visibility: %v", err)
	}
	addIssueRef(t, ctx, otherNote, otherIssue)

	// Viewer A (owner of the private note) sees both notes on the issue.
	rowsA, err := w3H.Cerebro.ListNotesReferencingObject(ctx, cerebrodb.ListNotesReferencingObjectParams{
		WorkspaceID: w3WsID, Object: couplingIssue, RefID: uuidStr(issueID), ViewerID: w3UserA,
	})
	if err != nil {
		t.Fatalf("list for A: %v", err)
	}
	if len(rowsA) != 2 {
		t.Fatalf("viewer A: got %d coupled notes, want 2", len(rowsA))
	}

	// Viewer B sees only the workspace-visible note (not A's private one).
	rowsB, err := w3H.Cerebro.ListNotesReferencingObject(ctx, cerebrodb.ListNotesReferencingObjectParams{
		WorkspaceID: w3WsID, Object: couplingIssue, RefID: uuidStr(issueID), ViewerID: w3UserB,
	})
	if err != nil {
		t.Fatalf("list for B: %v", err)
	}
	if len(rowsB) != 1 {
		t.Fatalf("viewer B: got %d coupled notes, want 1 (private hidden)", len(rowsB))
	}
	if uuidStr(rowsB[0].ID) != uuidStr(openNote) {
		t.Fatalf("viewer B saw the wrong note: %s", uuidStr(rowsB[0].ID))
	}
}

func TestSendCommentsToIssue(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := makeIssue(t, ctx, "Send target")
	noteID := makeNote(t, ctx, "Notes with comments", "body")
	addIssueRef(t, ctx, noteID, issueID)

	// FIR-1621: only comments that @-tag an agent are sent. Two tag the same
	// agent (dedup → 1 trigger); the third is untagged local discussion and
	// must NOT be sent.
	agentMention := "please [@bot](mention://agent/" + uuidStr(w3UserB) + ") look at this"
	agentMention2 := "also [@bot](mention://agent/" + uuidStr(w3UserB) + ") again"
	for _, b := range []string{agentMention, agentMention2, "untagged discussion"} {
		if _, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
			NoteID: noteID, Kind: "comment", Body: b, AuthorType: "member", AuthorID: w3UserA,
		}); err != nil {
			t.Fatalf("create comment: %v", err)
		}
	}

	fake := &fakeDispatcher{}
	h := &Handler{Upstream: w3H.Upstream, Cerebro: w3H.Cerebro, Tasks: fake}

	// Send all unsent (empty body).
	w := sendRequest(t, h, noteID, "{}")
	if w.Code != http.StatusOK {
		t.Fatalf("send all: status %d, body %s", w.Code, w.Body.String())
	}
	var resp sendCommentsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if len(resp.Sent) != 2 {
		t.Fatalf("sent %d comments, want 2 (only the tagged ones)", len(resp.Sent))
	}
	if resp.UnsentRemaining != 0 {
		t.Fatalf("unsent remaining = %d, want 0", resp.UnsentRemaining)
	}
	if len(fake.mentionAgents) != 1 {
		t.Fatalf("triggered %d agents, want 1 (deduped mention)", len(fake.mentionAgents))
	}
	if resp.AgentsTriggered != 1 {
		t.Fatalf("AgentsTriggered = %d, want 1", resp.AgentsTriggered)
	}

	// One issue comment should now exist on the target issue.
	var commentCount int
	if err := w3Pool.QueryRow(ctx, `SELECT COUNT(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&commentCount); err != nil {
		t.Fatalf("count issue comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("issue comment count = %d, want 1", commentCount)
	}

	// Re-sending now has nothing to send.
	w2 := sendRequest(t, h, noteID, "{}")
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("re-send: status %d, want 400 (no unsent)", w2.Code)
	}
}

// FIR-1753 — a note linked to two issues used to make "Send all" 409 because
// resolveCoupling refused the ambiguity while the client only ever read the
// first coupling. Now: with no hint, multiple couplings still 409 (so an old
// client gets a clear message), but passing the chosen destination_ref_id sends
// to exactly that issue.
func TestSendCommentsMultipleCouplings(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueA := makeIssue(t, ctx, "Destination A")
	issueB := makeIssue(t, ctx, "Destination B")
	noteID := makeNote(t, ctx, "Linked to two issues", "body")
	addIssueRef(t, ctx, noteID, issueA)
	addIssueRef(t, ctx, noteID, issueB)

	agentMention := "please [@bot](mention://agent/" + uuidStr(w3UserB) + ") look"
	if _, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID: noteID, Kind: "comment", Body: agentMention, AuthorType: "member", AuthorID: w3UserA,
	}); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	h := &Handler{Upstream: w3H.Upstream, Cerebro: w3H.Cerebro, Tasks: &fakeDispatcher{}}

	// No hint + two couplings → 409, not a generic failure.
	wAmb := sendRequest(t, h, noteID, "{}")
	if wAmb.Code != http.StatusConflict {
		t.Fatalf("ambiguous send: status %d, want 409; body %s", wAmb.Code, wAmb.Body.String())
	}

	// A hint that is not one of the note's couplings → 409.
	otherIssue := makeIssue(t, ctx, "Not linked")
	wBad := sendRequest(t, h, noteID,
		`{"destination_object":"issue","destination_ref_id":"`+uuidStr(otherIssue)+`"}`)
	if wBad.Code != http.StatusConflict {
		t.Fatalf("bad-hint send: status %d, want 409; body %s", wBad.Code, wBad.Body.String())
	}

	// Hint pointing at issueB → sends there.
	wOK := sendRequest(t, h, noteID,
		`{"destination_object":"issue","destination_ref_id":"`+uuidStr(issueB)+`"}`)
	if wOK.Code != http.StatusOK {
		t.Fatalf("hinted send: status %d, want 200; body %s", wOK.Code, wOK.Body.String())
	}
	var resp sendCommentsResponse
	if err := json.Unmarshal(wOK.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.DestinationRefID != uuidStr(issueB) {
		t.Fatalf("sent to %s, want issueB %s", resp.DestinationRefID, uuidStr(issueB))
	}
	// The comment landed on issueB, not issueA.
	var onB, onA int
	if err := w3Pool.QueryRow(ctx, `SELECT COUNT(*) FROM comment WHERE issue_id = $1`, issueB).Scan(&onB); err != nil {
		t.Fatalf("count B: %v", err)
	}
	if err := w3Pool.QueryRow(ctx, `SELECT COUNT(*) FROM comment WHERE issue_id = $1`, issueA).Scan(&onA); err != nil {
		t.Fatalf("count A: %v", err)
	}
	if onB != 1 || onA != 0 {
		t.Fatalf("comment routing wrong: onB=%d onA=%d, want onB=1 onA=0", onB, onA)
	}
}

func TestSendCommentsNotCoupled(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Uncoupled", "body")
	if _, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID: noteID, Kind: "comment", Body: "draft", AuthorType: "member", AuthorID: w3UserA,
	}); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	fake := &fakeDispatcher{}
	h := &Handler{Upstream: w3H.Upstream, Cerebro: w3H.Cerebro, Tasks: fake}
	w := sendRequest(t, h, noteID, "{}")
	if w.Code != http.StatusConflict {
		t.Fatalf("uncoupled note: status %d, want 409", w.Code)
	}
}
