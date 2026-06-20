package note

// FIR-1621 — documents (any artifact, kind != 'note') can carry comments through
// the same comments panel as notes. These tests exercise the access gate
// (CanUserCommentOnArtifact) and the comment insert against a real DB, proving:
//   * a plain document with no cerebro_note row is commentable by a workspace
//     member (folder access is the only per-artifact gate),
//   * a note still enforces its own visibility rule (a private note is not
//     commentable by a non-owner),
//   * the comment FK now points at artifact(id), so an insert on a document id
//     succeeds where it previously failed.
// They skip cleanly when no DB is reachable (same pattern as the other *_db_test).

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// makeDocument creates a plain document artifact (kind=report) with NO
// cerebro_note row — the shape that previously could not be commented on.
func makeDocument(t *testing.T, ctx context.Context, title string) pgtype.UUID {
	t.Helper()
	id, _ := uuid.NewV7()
	art, err := w3H.Upstream.CreateArtifact(ctx, db.CreateArtifactParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: w3WsID,
		Kind:        "report",
		Format:      "md",
		Title:       title,
		Body:        "document body",
		Metadata:    []byte("{}"),
		AuthorType:  "member",
		AuthorID:    w3UserA,
	})
	if err != nil {
		t.Fatalf("create document artifact: %v", err)
	}
	return art.ID
}

func canComment(t *testing.T, ctx context.Context, artifactID, userID pgtype.UUID) bool {
	t.Helper()
	allowed, err := w3H.Cerebro.CanUserCommentOnArtifact(ctx, cerebrodb.CanUserCommentOnArtifactParams{
		ID:    artifactID,
		PUser: userID,
	})
	if err != nil {
		t.Fatalf("CanUserCommentOnArtifact: %v", err)
	}
	return allowed
}

func TestDocumentCommentsAccessAndInsert(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()

	// A document (no cerebro_note row) is commentable by a workspace member.
	docID := makeDocument(t, ctx, "Plain document")
	if !canComment(t, ctx, docID, w3UserA) {
		t.Fatal("expected document to be commentable by member A")
	}
	if !canComment(t, ctx, docID, w3UserB) {
		t.Fatal("expected document to be commentable by member B (folder-visible)")
	}

	// The comment insert now succeeds on a document id (FK -> artifact(id)).
	if _, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID:     docID,
		Kind:       "comment",
		Body:       "a comment on a plain document",
		AuthorType: "member",
		AuthorID:   w3UserA,
	}); err != nil {
		t.Fatalf("create comment on document: %v", err)
	}
	list, err := w3H.Cerebro.ListNoteComments(ctx, docID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 comment on document, got %d", len(list))
	}

	// A private note still enforces its visibility rule: owner yes, others no.
	noteID := makeNote(t, ctx, "Private note", "secret")
	if !canComment(t, ctx, noteID, w3UserA) {
		t.Fatal("expected private note to be commentable by its owner")
	}
	if canComment(t, ctx, noteID, w3UserB) {
		t.Fatal("expected private note to be NOT commentable by a non-owner")
	}
}
