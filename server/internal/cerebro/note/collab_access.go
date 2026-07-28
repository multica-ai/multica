package note

import (
	"context"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// CollabAccess adapts the Notes access rules to the live co-editing engine
// (FIR-1317). It deliberately reuses the exact same queries the REST endpoints
// use — CanUserSeeNote for opening a note and CanUserEditNote for changing it —
// so joining a live session can never grant more than saving already does.
//
// It lives in the note package rather than in collab because the rules (owner,
// workspace visibility, share, folder grant) belong to Notes; collab only asks
// the yes/no questions.
type CollabAccess struct {
	Cerebro *cerebrodb.Queries
}

// CanSee reports whether the user may open the note at all.
func (a CollabAccess) CanSee(ctx context.Context, noteID, userID string) (bool, error) {
	noteUUID, err := util.ParseUUID(noteID)
	if err != nil {
		return false, err
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return false, err
	}
	return a.Cerebro.CanUserSeeNote(ctx, cerebrodb.CanUserSeeNoteParams{
		ArtifactID: noteUUID,
		OwnerID:    userUUID,
	})
}

// CanEdit reports whether the user may change the note. A viewer still joins
// the live session and sees other people's carets and text, but the engine
// refuses their steps — read-only stays read-only.
func (a CollabAccess) CanEdit(ctx context.Context, noteID, userID string) (bool, error) {
	noteUUID, err := util.ParseUUID(noteID)
	if err != nil {
		return false, err
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return false, err
	}
	return a.Cerebro.CanUserEditNote(ctx, cerebrodb.CanUserEditNoteParams{
		ArtifactID: noteUUID,
		OwnerID:    userUUID,
	})
}
