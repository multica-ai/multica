package note

// FIR-2595 Stage 2 — the "give access?" prompt's backend. When a user @mentions
// someone who cannot open a foldered note, the client first asks the server who
// actually lacks access (MentionAccessCheck), shows the prompt only for those
// people, and — if the author chooses "give access" — grants each named member
// viewer access on the note's folder (GrantMentionAccess) BEFORE the mention is
// saved. notifyNoteMentions then notifies them, because they can now open the
// note. This replaces the old silent auto-share (which, for a folder-gated
// note, produced a notification the recipient could not open — the FIR-2589
// symptom).

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type grantMentionAccessRequest struct {
	MemberIDs []string `json:"member_ids"`
}

type mentionAccessCheckResponse struct {
	// Of the members asked about, those who cannot currently open the note.
	NoAccess []string `json:"no_access"`
}

type grantMentionAccessResponse struct {
	// The members that were granted access (invalid ids are skipped).
	Granted []string `json:"granted"`
}

// MentionAccessCheck (GET /{id}/mention-access?members=a,b,c) returns which of
// the given members cannot currently open the note, so the client prompts only
// for the people who actually need access. The caller must be able to comment
// on the note (they are the one composing the mention).
func (h *Handler) MentionAccessCheck(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanComment(w, r, noteID, ownerUUID) {
		return
	}
	members := splitMemberIDs(r.URL.Query().Get("members"))
	writeJSON(w, http.StatusOK, mentionAccessCheckResponse{
		NoAccess: h.membersWithoutNoteAccess(r.Context(), noteID, members),
	})
}

// GrantMentionAccess (POST /{id}/mention-access, body {member_ids}) grants each
// named member viewer access so a tagged person can open the note. A foldered
// note grants viewer on its folder (the Collections model — "via mappen"); a
// note at the workspace root has no folder to gate it, so a note-share (with the
// note bumped to 'shared' if it was private) is enough. The caller must be able
// to comment on the note and own it; viewing/commenting alone must not let a
// member expand the audience.
func (h *Handler) GrantMentionAccess(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanComment(w, r, noteID, ownerUUID) {
		return
	}
	if !h.requireCanGrantMentionAccess(w, r, noteID, ownerUUID) {
		return
	}
	var req grantMentionAccessRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	art, err := h.Upstream.GetArtifact(r.Context(), db.GetArtifactParams{ID: noteID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}

	// A root (folderless) note is ungated, so a note-share opens it — but only
	// when the note is 'shared'/'workspace'. Bump a private root note once, the
	// same rule notifyNoteMentions uses.
	rootPrivate := false
	if !art.FolderID.Valid {
		if noteRow, nerr := h.Cerebro.GetNote(r.Context(), noteID); nerr == nil && noteRow.Visibility == "private" {
			rootPrivate = true
		}
	}

	var granted []string
	for _, uid := range req.MemberIDs {
		mu, perr := util.ParseUUID(uid)
		if perr != nil {
			continue
		}
		if art.FolderID.Valid {
			if _, gerr := h.Cerebro.UpsertCerebroFolderGrant(r.Context(), cerebrodb.UpsertCerebroFolderGrantParams{
				Surface:     "artifact",
				FolderID:    art.FolderID,
				GranteeType: "member",
				GranteeID:   mu,
				Role:        "viewer",
				CreatedBy:   ownerUUID,
			}); gerr != nil {
				continue
			}
		} else {
			if aerr := h.Cerebro.AddNoteShare(r.Context(), cerebrodb.AddNoteShareParams{ArtifactID: noteID, UserID: mu}); aerr != nil {
				continue
			}
			if rootPrivate {
				_ = h.Cerebro.SetNoteVisibility(r.Context(), cerebrodb.SetNoteVisibilityParams{ArtifactID: noteID, Visibility: "shared"})
				rootPrivate = false
			}
		}
		granted = append(granted, uid)
	}
	writeJSON(w, http.StatusOK, grantMentionAccessResponse{Granted: granted})
}

func (h *Handler) requireCanGrantMentionAccess(w http.ResponseWriter, r *http.Request, noteID, ownerUUID pgtype.UUID) bool {
	noteRow, err := h.Cerebro.GetNote(r.Context(), noteID)
	if err != nil || !noteRow.OwnerID.Valid || !ownerUUID.Valid || noteRow.OwnerID.Bytes != ownerUUID.Bytes {
		writeError(w, http.StatusForbidden, "only the note owner can give access")
		return false
	}
	return true
}

// membersWithoutNoteAccess returns the subset of memberIDs that cannot currently
// open the note. Invalid ids are skipped (never reported as lacking access).
func (h *Handler) membersWithoutNoteAccess(ctx context.Context, noteID pgtype.UUID, memberIDs []string) []string {
	var out []string
	for _, uid := range memberIDs {
		mu, err := util.ParseUUID(uid)
		if err != nil {
			continue
		}
		ok, err := h.Cerebro.CanUserSeeNote(ctx, cerebrodb.CanUserSeeNoteParams{ArtifactID: noteID, OwnerID: mu})
		if err == nil && !ok {
			out = append(out, uid)
		}
	}
	return out
}

// splitMemberIDs parses a comma-separated ?members= list into trimmed, non-empty
// ids.
func splitMemberIDs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
