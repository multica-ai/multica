// FIR-2697 — integration tests for agent-document version history. Reuses the
// wave3 fixture harness (TestMain / w3Pool / w3H / w3WsID / w3UserA / w3UserB in
// wave3_db_test.go). Skips when the test DB is unreachable, like its siblings.
package note

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// makeVersionedDocument creates a plain document artifact (no cerebro_note row) authored
// by the given actor, and returns its id.
func makeVersionedDocument(t *testing.T, ctx context.Context, kind, title, body, authorType string, authorID pgtype.UUID) pgtype.UUID {
	t.Helper()
	id, _ := uuid.NewV7()
	art, err := w3H.Upstream.CreateArtifact(ctx, db.CreateArtifactParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: w3WsID,
		Kind:        kind,
		Format:      "md",
		Title:       title,
		Body:        body,
		Metadata:    []byte("{}"),
		AuthorType:  authorType,
		AuthorID:    authorID,
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	return art.ID
}

// artifactSavedEvent builds the artifact:updated payload the upstream handler
// publishes (map[string]any{"artifact": artifactToResponse(...)}), for the given
// actor.
func artifactSavedEvent(id pgtype.UUID, title, body, actorType string, actorID pgtype.UUID) events.Event {
	return events.Event{
		Type:      eventArtifactUpdated,
		ActorType: actorType,
		ActorID:   uuidStr(actorID),
		Payload: map[string]any{
			"artifact": map[string]any{
				"id":    uuidStr(id),
				"title": title,
				"body":  body,
			},
		},
	}
}

// TestArtifactVersionListener proves that a plain agent-created document (with no
// cerebro_note row) accumulates version history from artifact save events —
// including agent authorship, no-op skipping, same-author coalescing, and a new
// entry for a different author.
func TestArtifactVersionListener(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	docID := makeVersionedDocument(t, ctx, "report", "Doc", "v0", "agent", w3UserA)

	bus := events.New()
	RegisterArtifactVersionListener(bus, w3H.Cerebro)

	// First agent save → one version, attributed to the agent.
	bus.Publish(artifactSavedEvent(docID, "Doc", "v1", "agent", w3UserA))
	vers, _ := w3H.Cerebro.ListNoteVersions(ctx, docID)
	if len(vers) != 1 {
		t.Fatalf("after first agent save = %d versions, want 1", len(vers))
	}
	if vers[0].AuthorType != "agent" {
		t.Fatalf("author_type = %q, want agent", vers[0].AuthorType)
	}
	if vers[0].Body != "v1" {
		t.Fatalf("version body = %q, want v1", vers[0].Body)
	}

	// Re-emitting the SAME content (e.g. a folder/scope-only update) must not add
	// a noise version.
	bus.Publish(artifactSavedEvent(docID, "Doc", "v1", "agent", w3UserA))
	vers, _ = w3H.Cerebro.ListNoteVersions(ctx, docID)
	if len(vers) != 1 {
		t.Fatalf("after no-op save = %d versions, want 1 (skipped)", len(vers))
	}

	// A changed save by the SAME agent within the window coalesces (rolls forward).
	bus.Publish(artifactSavedEvent(docID, "Doc", "v2", "agent", w3UserA))
	vers, _ = w3H.Cerebro.ListNoteVersions(ctx, docID)
	if len(vers) != 1 {
		t.Fatalf("after same-author edit = %d versions, want 1 (coalesced)", len(vers))
	}
	if vers[0].Body != "v2" {
		t.Fatalf("coalesced body = %q, want v2", vers[0].Body)
	}

	// A save by a DIFFERENT author starts a new version entry.
	bus.Publish(artifactSavedEvent(docID, "Doc", "v3", "member", w3UserB))
	vers, _ = w3H.Cerebro.ListNoteVersions(ctx, docID)
	if len(vers) != 2 {
		t.Fatalf("after different-author save = %d versions, want 2", len(vers))
	}
	if vers[0].AuthorType != "member" {
		t.Fatalf("newest author_type = %q, want member", vers[0].AuthorType)
	}
}

// TestCanUserEditArtifactDocument checks the document write gate used by the
// version save/restore endpoints: the member author of a root document may edit
// it; an unrelated member may not.
func TestCanUserEditArtifactDocument(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	docID := makeVersionedDocument(t, ctx, "plan", "P", "body", "member", w3UserA)

	okAuthor, err := w3H.Cerebro.CanUserEditArtifact(ctx, cerebrodb.CanUserEditArtifactParams{ID: docID, PUser: w3UserA})
	if err != nil {
		t.Fatalf("CanUserEditArtifact author: %v", err)
	}
	if !okAuthor {
		t.Fatalf("member author = %v, want true", okAuthor)
	}

	okOther, err := w3H.Cerebro.CanUserEditArtifact(ctx, cerebrodb.CanUserEditArtifactParams{ID: docID, PUser: w3UserB})
	if err != nil {
		t.Fatalf("CanUserEditArtifact other: %v", err)
	}
	if okOther {
		t.Fatalf("unrelated member on root doc = %v, want false", okOther)
	}
}
