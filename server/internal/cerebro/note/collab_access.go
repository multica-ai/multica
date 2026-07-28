package note

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

const liveCollabFlag = "cerebro_note_live_collab"

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

// LiveEnabled resolves the server-side gate with the same precedence as the
// feature-flag client: locked workspace > personal > unlocked workspace >
// registry default. The registry default for live collaboration is false.
func (a CollabAccess) LiveEnabled(ctx context.Context, noteID, userID string) (bool, error) {
	noteUUID, err := util.ParseUUID(noteID)
	if err != nil {
		return false, err
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return false, err
	}
	workspaceID, err := a.Cerebro.GetNoteWorkspaceID(ctx, noteUUID)
	if err != nil {
		return false, err
	}

	var workspaceEnabled, workspaceLocked, workspaceFound bool
	workspaceFlags, err := a.Cerebro.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	for _, flag := range workspaceFlags {
		if flag.FlagKey == liveCollabFlag {
			workspaceEnabled = flag.Enabled
			workspaceLocked = flag.Locked
			workspaceFound = true
			break
		}
	}
	if workspaceFound && workspaceLocked {
		return workspaceEnabled, nil
	}

	personalEnabled, err := a.Cerebro.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      userUUID,
		FlagKey:     liveCollabFlag,
	})
	if err == nil {
		return personalEnabled, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if workspaceFound {
		return workspaceEnabled, nil
	}
	return false, nil
}
