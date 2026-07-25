// FIR-3778 — reproduction of "a user cannot edit a document an agent created
// for them". Reuses the wave3 fixture harness (TestMain / w3Pool / w3H /
// w3WsID / w3UserA / w3UserB in wave3_db_test.go).
//
// The decided behaviour: the human the document was created for owns it and may
// edit it, while the agent keeps its own write access on their behalf. The data
// needed already existed — artifact.requester_user_id (migration 9004) records
// that human — it was simply never consulted by CanUserEditArtifact.
package note

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// makeRequestedDocument creates the artifact shape the live API produces when an
// agent writes a document for a human: agent author, human requester, no folder.
func makeRequestedDocument(t *testing.T, ctx context.Context, kind, title, body string, agentID, requesterID pgtype.UUID) pgtype.UUID {
	t.Helper()
	id, _ := uuid.NewV7()
	art, err := w3H.Upstream.CreateArtifact(ctx, db.CreateArtifactParams{
		ID:              pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:     w3WsID,
		Kind:            kind,
		Format:          "md",
		Title:           title,
		Body:            body,
		Metadata:        []byte("{}"),
		AuthorType:      "agent",
		AuthorID:        agentID,
		RequesterUserID: requesterID,
	})
	if err != nil {
		t.Fatalf("create requested document: %v", err)
	}
	return art.ID
}

// TestFIR3778AgentCreatedDocumentIsEditableByItsRequester isolates the single
// variable that decides whether a human may edit a root document: who created it.
//
// Same workspace, same member, same document shape — only author_type differs.
// Before the fix, CanUserEditArtifact admitted only the folder-grant path or
// `author_type = 'member' AND author_id = caller`, so an agent-authored document
// was editable by nobody: the human it was written for stayed invisible to the
// gate even though requester_user_id named her.
//
// The GET note endpoint feeds this same query into NoteResponse.CanEdit
// (handler.go, FIR-3628), so a false here is exactly what rendered the editor
// read-only with no visible edit option — the symptom Majken reported.
func TestFIR3778AgentCreatedDocumentIsEditableByItsRequester(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()

	canEdit := func(docID, user pgtype.UUID) bool {
		t.Helper()
		ok, err := w3H.Cerebro.CanUserEditArtifact(ctx, cerebrodb.CanUserEditArtifactParams{
			ID:    docID,
			PUser: user,
		})
		if err != nil {
			t.Fatalf("CanUserEditArtifact: %v", err)
		}
		return ok
	}

	// Control: the member creates the document herself → she may edit it.
	// This proves the fixture user is a real workspace member and the gate is
	// not simply denying everything.
	ownDoc := makeVersionedDocument(t, ctx, "note", "Checklist (self-made)", "body", "member", w3UserA)
	if !canEdit(ownDoc, w3UserA) {
		t.Fatalf("control: member author cannot edit her own document — fixture is wrong, not the bug")
	}

	// The reported case: the member asks an agent to create the same document
	// for her. The agent is the author; she is recorded as requester_user_id —
	// exactly the shape the live artifact API produces today (migration 9004:
	// "when an agent creates an artifact at a user's request, author is the
	// agent and requester is the user who prompted them").
	agentID, _ := uuid.NewV7()
	agentDoc := makeRequestedDocument(t, ctx, "note", "Checklist (agent-made)", "body",
		pgtype.UUID{Bytes: agentID, Valid: true}, w3UserA)

	// Decided behaviour (Jesper, 2026-07-25): the human owns it and may edit it.
	if !canEdit(agentDoc, w3UserA) {
		t.Errorf("the human the document was created for cannot edit it (canEdit=false, want true)")
	}

	// The fix admits exactly one human, not "any member". A colleague who never
	// asked for the document must still be refused — this is the guard against
	// the gate quietly widening into workspace-wide write access.
	if canEdit(agentDoc, w3UserB) {
		t.Errorf("an unrelated member can edit the agent document (canEdit=true, want false)")
	}
}
