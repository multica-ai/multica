// Version history on notes (Wave 3 / G2, TECH-3556). Mirrors the reference +
// comment handlers: registered on the existing note.Handler.Routes under
// /api/notes/{id}/versions, reusing the same scope + visibility gating.
//
// A version is a snapshot of the note's title+body. Snapshots are created by
// snapshotVersion (called from UpdateNote, manual "Save version", restore, and
// suggestion-accept). Reads are visible to anyone who can see the note; writes
// that mutate the note (restore) are owner-only.
package note

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// coalesceWindow groups consecutive saves by the same author into one version
// (see migration 9078). A save within this window of the latest version, by the
// same author, with reason 'edit', rolls that version forward instead of adding
// a new row — so a typing burst becomes one history entry, not dozens.
const coalesceWindow = 5 * time.Minute

// VersionResponse is the wire shape for one history entry. Body is included so
// the client can diff/preview without a per-row round-trip (notes are small).
type VersionResponse struct {
	ID         string `json:"id"`
	NoteID     string `json:"note_id"`
	VersionNo  int32  `json:"version_no"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	ByteSize   int32  `json:"byte_size"`
	Reason     string `json:"reason"`
	Label      string `json:"label"`
	AuthorType string `json:"author_type"`
	AuthorID   string `json:"author_id"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func versionToResponse(v cerebrodb.CerebroNoteVersion) VersionResponse {
	return VersionResponse{
		ID:         uuidStr(v.ID),
		NoteID:     uuidStr(v.NoteID),
		VersionNo:  v.VersionNo,
		Title:      v.Title,
		Body:       v.Body,
		ByteSize:   v.ByteSize,
		Reason:     v.Reason,
		Label:      v.Label.String,
		AuthorType: v.AuthorType,
		AuthorID:   uuidStr(v.AuthorID),
		CreatedAt:  tsStr(v.CreatedAt),
		UpdatedAt:  tsStr(v.UpdatedAt),
	}
}

// snapshotVersion records the given title+body as a version of the note. When
// coalesce is true an 'edit' save by the same author within coalesceWindow of
// the latest version rolls that version forward (no new row); otherwise — or
// for 'manual'/'restore' reasons — a new monotonic version is inserted.
//
// Best-effort: callers treat a snapshot failure as non-fatal (the live note
// content has already been saved on the artifact). reason is one of
// 'edit'|'manual'|'restore'.
func (h *Handler) snapshotVersion(ctx context.Context, noteID, authorID pgtype.UUID, title, body, reason, label string, coalesce bool) {
	latest, err := h.Cerebro.GetLatestNoteVersion(ctx, noteID)
	hasLatest := err == nil

	if coalesce && hasLatest && reason == "edit" && latest.Reason == "edit" &&
		uuidStr(latest.AuthorID) == uuidStr(authorID) &&
		latest.UpdatedAt.Valid && time.Since(latest.UpdatedAt.Time) < coalesceWindow {
		_, _ = h.Cerebro.RollForwardNoteVersion(ctx, cerebrodb.RollForwardNoteVersionParams{
			ID:       latest.ID,
			Title:    title,
			Body:     body,
			ByteSize: int32(len(body)),
		})
		return
	}

	nextNo := int32(1)
	if hasLatest {
		nextNo = latest.VersionNo + 1
	}
	_, _ = h.Cerebro.InsertNoteVersion(ctx, cerebrodb.InsertNoteVersionParams{
		NoteID:     noteID,
		VersionNo:  nextNo,
		Title:      title,
		Body:       body,
		ByteSize:   int32(len(body)),
		Reason:     reason,
		Label:      pgText(label),
		AuthorType: "member",
		AuthorID:   authorID,
	})
}

// --- request shapes ---

type saveVersionRequest struct {
	Label string `json:"label,omitempty"`
}

// --- handlers ---

// ListVersions returns the note's history, newest first. Any viewer who can see
// the note may read its history.
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireCanSee(w, r, noteID, ownerUUID) {
		return
	}
	rows, err := h.Cerebro.ListNoteVersions(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}
	out := make([]VersionResponse, 0, len(rows))
	for _, v := range rows {
		out = append(out, versionToResponse(v))
	}
	writeJSON(w, http.StatusOK, out)
}

// SaveVersion captures the current note state as a manual milestone version
// (always a fresh row, optionally labelled). Owner-only — it is an explicit
// "checkpoint this" action on a note you own.
func (h *Handler) SaveVersion(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}
	var req saveVersionRequest
	_ = decodeJSON(r, &req)

	artifact, err := h.Upstream.GetArtifact(r.Context(), db.GetArtifactParams{ID: noteID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	h.snapshotVersion(r.Context(), noteID, ownerUUID, artifact.Title, artifact.Body, "manual", req.Label, false)
	latest, err := h.Cerebro.GetLatestNoteVersion(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save version")
		return
	}
	writeJSON(w, http.StatusCreated, versionToResponse(latest))
}

// RestoreVersion sets the note's live content back to a chosen version. Owner
// only. The current state is snapshotted first (so the restore is undoable),
// then the body/title are written, then a 'restore' version is recorded.
func (h *Handler) RestoreVersion(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}
	versionID, ok := subPathUUID(w, r, "versionId")
	if !ok {
		return
	}
	target, err := h.Cerebro.GetNoteVersion(r.Context(), cerebrodb.GetNoteVersionParams{NoteID: noteID, ID: versionID})
	if err != nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	artifact, err := h.Upstream.GetArtifact(r.Context(), db.GetArtifactParams{ID: noteID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	// Capture the pre-restore state so the restore can itself be undone.
	h.snapshotVersion(r.Context(), noteID, ownerUUID, artifact.Title, artifact.Body, "edit", "", false)

	updated, err := h.Upstream.UpdateArtifact(r.Context(), db.UpdateArtifactParams{
		ID:            noteID,
		WorkspaceID:   wsUUID,
		Title:         target.Title,
		Body:          target.Body,
		Metadata:      artifact.Metadata,
		FileUrl:       artifact.FileUrl,
		FileSizeBytes: artifact.FileSizeBytes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore version")
		return
	}
	// Record the restored state as its own version entry.
	h.snapshotVersion(r.Context(), noteID, ownerUUID, updated.Title, updated.Body, "restore", "", false)

	writeJSON(w, http.StatusOK, NoteResponse{
		ID:          uuidStr(updated.ID),
		WorkspaceID: uuidStr(updated.WorkspaceID),
		FolderID:    uuidPtr(updated.FolderID),
		Title:       updated.Title,
		Body:        updated.Body,
		OwnerID:     uuidStr(ownerUUID),
		CreatedAt:   tsStr(updated.CreatedAt),
		UpdatedAt:   tsStr(updated.UpdatedAt),
	})
}

// --- helpers shared with comments.go / lock.go ---

// requireCanSee returns true only when the caller may see the note (owner,
// workspace-visible, or shared with them). Used by read + comment endpoints
// that any viewer — not just the owner — is allowed to hit.
func (h *Handler) requireCanSee(w http.ResponseWriter, r *http.Request, noteID, ownerUUID pgtype.UUID) bool {
	allowed, err := h.Cerebro.CanUserSeeNote(r.Context(), cerebrodb.CanUserSeeNoteParams{
		ArtifactID: noteID,
		OwnerID:    ownerUUID,
	})
	if err != nil || !allowed {
		writeError(w, http.StatusNotFound, "note not found")
		return false
	}
	return true
}

func pgText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// subPathUUID parses a secondary UUID URL param (e.g. {versionId}, {commentId}).
func subPathUUID(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	id := chi.URLParam(r, name)
	u, err := util.ParseUUID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return u, true
}

// decodeJSON decodes a request body into v, tolerating an empty body (returns
// nil) so endpoints with an optional payload don't 400 on a bodyless POST.
func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(r.Body).Decode(v)
	if err == io.EOF {
		return nil
	}
	return err
}
