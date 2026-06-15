// Package note holds the cerebro-only HTTP handlers for the Notes feature
// (TECH-3421). A Note is a lightweight, private-by-default note built on top of
// the upstream artifact (Document) machinery: it is an artifact with
// kind='note' plus a cerebro_note row that carries owner + visibility + pin
// state. The handler creates the artifact via the upstream Queries and the
// note-specific state via the cerebro Queries, so notes reuse the artifact
// editor, storage, folders and search while staying fork-clean (the upstream
// artifact table is never modified).
package note

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EventNoteMentioned fires when a note save introduces one or more new person
// (member) mentions. The notification listener (cerebro_note_mentions.go) turns
// each mentioned member into a routed "mentioned" inbox notification, reusing
// the same engine + settings as comment mentions. Payload keys: "note_id",
// "note_title", "member_ids" ([]string of the newly-mentioned user IDs).
const EventNoteMentioned = "cerebro:note_mentioned"

// validVisibility gates the visibility values a client may set. Mirrors the
// CHECK constraint in migration 9073_cerebro_note.
var validVisibility = map[string]bool{
	"private":   true,
	"shared":    true,
	"workspace": true,
}

const (
	defaultListLimit = 50
	maxListLimit     = 200
	recentLimit      = 5
)

// Handler exposes the cerebro-only Notes endpoints. It carries the upstream
// sqlc Queries (to create/read/delete the underlying artifact) and the cerebro
// Queries (for note owner/visibility/pin/share state).
type Handler struct {
	Upstream *db.Queries
	Cerebro  *cerebrodb.Queries
	Bus      *events.Bus
}

// New constructs the handler. The router wires both query packages plus the
// event bus (used to fan person-mentions out to the notification listener).
func New(upstream *db.Queries, cerebro *cerebrodb.Queries, bus *events.Bus) *Handler {
	return &Handler{Upstream: upstream, Cerebro: cerebro, Bus: bus}
}

// notifyNoteMentions handles person (@member) mentions introduced by a note
// save. It diffs the new body against the old (oldBody is "" on create) and,
// for every member mention that is newly added and is not the author:
//   - shares the note with them so the notification is openable — a private
//     note is bumped to 'shared' the first time it gains a tagged person, and
//   - publishes EventNoteMentioned so the notification listener creates a
//     routed "mentioned" inbox item, reusing the comment-mention engine.
//
// Everything here is best-effort: a note save must never fail because a share
// or notification could not be created.
func (h *Handler) notifyNoteMentions(ctx context.Context, wsID, artifactID, ownerID pgtype.UUID, title, oldBody, newBody, visibility string) {
	added := newMemberMentions(oldBody, newBody, uuidStr(ownerID))
	if len(added) == 0 {
		return
	}
	for _, uid := range added {
		mu, err := util.ParseUUID(uid)
		if err != nil {
			continue
		}
		_ = h.Cerebro.AddNoteShare(ctx, cerebrodb.AddNoteShareParams{ArtifactID: artifactID, UserID: mu})
	}
	// Shares only grant access when visibility is 'shared' (or 'workspace'), so
	// a private note becomes shared the moment it gains a tagged person.
	if visibility == "private" {
		_ = h.Cerebro.SetNoteVisibility(ctx, cerebrodb.SetNoteVisibilityParams{ArtifactID: artifactID, Visibility: "shared"})
	}
	if h.Bus == nil {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        EventNoteMentioned,
		WorkspaceID: uuidStr(wsID),
		ActorType:   "member",
		ActorID:     uuidStr(ownerID),
		Payload: map[string]any{
			"note_id":    uuidStr(artifactID),
			"note_title": title,
			"member_ids": added,
		},
	})
}

// newMemberMentions returns the user IDs mentioned as members in newBody that
// were not already mentioned in oldBody, excluding excludeID (the author).
// Order is stable and duplicates are removed, so re-saving a note never
// re-notifies someone already tagged.
func newMemberMentions(oldBody, newBody, excludeID string) []string {
	old := map[string]bool{}
	for _, m := range util.ParseMentions(oldBody) {
		if m.Type == "member" {
			old[m.ID] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range util.ParseMentions(newBody) {
		if m.Type != "member" || m.ID == excludeID || old[m.ID] || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m.ID)
	}
	return out
}

// Routes mounts the Notes endpoints under /api/notes.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.ListNotes)
	r.Post("/", h.CreateNote)
	r.Get("/recent", h.ListRecentNotes)
	r.Get("/{id}", h.GetNote)
	r.Put("/{id}", h.UpdateNote)
	r.Delete("/{id}", h.DeleteNote)
	r.Put("/{id}/visibility", h.SetVisibility)
	r.Put("/{id}/pin", h.SetPin)
	r.Get("/{id}/shares", h.ListShares)
	// References on a note (TECH-3421): mirror of the issue-reference feature,
	// keyed by the note's artifact id. See references.go.
	r.Get("/{id}/references", h.ListReferences)
	r.Post("/{id}/references", h.CreateReference)
	r.Delete("/{id}/references/{refId}", h.DeleteReference)

	// Wave 3 / G1 — comments + suggestions on a note. See comments.go.
	r.Get("/{id}/comments", h.ListComments)
	r.Post("/{id}/comments", h.CreateComment)
	r.Put("/{id}/comments/{commentId}", h.UpdateComment)
	r.Delete("/{id}/comments/{commentId}", h.DeleteComment)
	r.Post("/{id}/comments/{commentId}/resolve", h.ResolveComment)
	r.Post("/{id}/comments/{commentId}/suggestion", h.DecideSuggestion)

	// Wave 3 / G2 — version history. See versions.go.
	r.Get("/{id}/versions", h.ListVersions)
	r.Post("/{id}/versions", h.SaveVersion)
	r.Post("/{id}/versions/{versionId}/restore", h.RestoreVersion)

	// Wave 3 / G3 — interim edit lock. See lock.go.
	r.Get("/{id}/lock", h.GetLock)
	r.Post("/{id}/lock", h.AcquireLock)
	r.Delete("/{id}/lock", h.ReleaseLock)
}

// --- request / response shapes ---

type createNoteRequest struct {
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	FolderID   *string `json:"folder_id,omitempty"`
	Visibility string  `json:"visibility,omitempty"`
}

type updateNoteRequest struct {
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
}

type setVisibilityRequest struct {
	Visibility    string   `json:"visibility"`
	SharedUserIDs []string `json:"shared_user_ids,omitempty"`
}

type setPinRequest struct {
	Pinned bool `json:"pinned"`
}

// NoteResponse is the wire shape for a note: the artifact essentials plus the
// note-specific state. It is intentionally lighter than the full artifact
// response — the Notes list and editor only need these fields.
type NoteResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	FolderID    *string `json:"folder_id"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	OwnerID     string  `json:"owner_id"`
	Visibility  string  `json:"visibility"`
	Pinned      bool    `json:"pinned"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// --- handlers ---

// CreateNote creates an empty-or-filled note: an artifact (kind='note') plus a
// cerebro_note row owned by the caller, private by default. No title is
// required — when absent the note is stored titleless and the client derives
// the title from the first line.
func (h *Handler) CreateNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var req createNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	visibility := req.Visibility
	if visibility == "" {
		visibility = "private"
	}
	if !validVisibility[visibility] {
		writeError(w, http.StatusBadRequest, "invalid visibility; expected private|shared|workspace")
		return
	}

	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	ownerUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var folderID pgtype.UUID
	if req.FolderID != nil && *req.FolderID != "" {
		folder, err := h.Upstream.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
			ID:          mustUUID(*req.FolderID),
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "folder not found")
			return
		}
		folderID = folder.ID
	}

	id, _ := uuid.NewV7()
	artifact, err := h.Upstream.CreateArtifact(r.Context(), db.CreateArtifactParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: wsUUID,
		FolderID:    folderID,
		Kind:        "note",
		Format:      "md",
		Title:       req.Title,
		Body:        req.Body,
		Metadata:    []byte("{}"),
		AuthorType:  "member",
		AuthorID:    ownerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create note")
		return
	}

	noteRow, err := h.Cerebro.UpsertNote(r.Context(), cerebrodb.UpsertNoteParams{
		ArtifactID: artifact.ID,
		OwnerID:    ownerUUID,
		Visibility: visibility,
		Pinned:     false,
	})
	if err != nil {
		// Best-effort cleanup so we don't leave an artifact with no note row.
		_ = h.Upstream.DeleteArtifact(r.Context(), db.DeleteArtifactParams{ID: artifact.ID, WorkspaceID: wsUUID})
		writeError(w, http.StatusInternalServerError, "failed to create note")
		return
	}

	// Tagging a person in the note body notifies them (and shares the note).
	h.notifyNoteMentions(r.Context(), wsUUID, artifact.ID, ownerUUID, artifact.Title, "", artifact.Body, noteRow.Visibility)

	writeJSON(w, http.StatusCreated, NoteResponse{
		ID:          uuidStr(artifact.ID),
		WorkspaceID: uuidStr(artifact.WorkspaceID),
		FolderID:    uuidPtr(artifact.FolderID),
		Title:       artifact.Title,
		Body:        artifact.Body,
		OwnerID:     uuidStr(noteRow.OwnerID),
		Visibility:  noteRow.Visibility,
		Pinned:      noteRow.Pinned,
		CreatedAt:   tsStr(artifact.CreatedAt),
		UpdatedAt:   tsStr(artifact.UpdatedAt),
	})
}

// ListNotes returns the caller's visible notes in a workspace, pinned-first
// then most-recent. An optional ?q= filters by title/body (private notes only
// ever match for their owner).
func (h *Handler) ListNotes(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	limit, offset := pageParams(r)

	q := r.URL.Query().Get("q")
	var rows []NoteResponse
	if q != "" {
		res, err := h.Cerebro.SearchNotesForUser(r.Context(), cerebrodb.SearchNotesForUserParams{
			WorkspaceID: wsUUID,
			OwnerID:     ownerUUID,
			Q:           q,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to search notes")
			return
		}
		rows = make([]NoteResponse, 0, len(res))
		for _, n := range res {
			rows = append(rows, searchRowToResponse(n))
		}
	} else {
		res, err := h.Cerebro.ListNotesForUser(r.Context(), cerebrodb.ListNotesForUserParams{
			WorkspaceID: wsUUID,
			OwnerID:     ownerUUID,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list notes")
			return
		}
		rows = make([]NoteResponse, 0, len(res))
		for _, n := range res {
			rows = append(rows, listRowToResponse(n))
		}
	}
	writeJSON(w, http.StatusOK, rows)
}

// ListRecentNotes feeds the Notes box in the dynamic inbox: a small pinned-first
// feed of the caller's visible notes.
func (h *Handler) ListRecentNotes(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	limit := int32(recentLimit)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20 {
			limit = int32(n)
		}
	}
	res, err := h.Cerebro.ListRecentNotesForUser(r.Context(), cerebrodb.ListRecentNotesForUserParams{
		WorkspaceID: wsUUID,
		OwnerID:     ownerUUID,
		Limit:       limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recent notes")
		return
	}
	rows := make([]NoteResponse, 0, len(res))
	for _, n := range res {
		rows = append(rows, recentRowToResponse(n))
	}
	writeJSON(w, http.StatusOK, rows)
}

// GetNote returns a single note when the caller is allowed to see it.
func (h *Handler) GetNote(w http.ResponseWriter, r *http.Request) {
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
	allowed, err := h.Cerebro.CanUserSeeNote(r.Context(), cerebrodb.CanUserSeeNoteParams{
		ArtifactID: noteID,
		OwnerID:    ownerUUID,
	})
	if err != nil || !allowed {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	artifact, err := h.Upstream.GetArtifact(r.Context(), db.GetArtifactParams{ID: noteID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	noteRow, err := h.Cerebro.GetNote(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	writeJSON(w, http.StatusOK, NoteResponse{
		ID:          uuidStr(artifact.ID),
		WorkspaceID: uuidStr(artifact.WorkspaceID),
		FolderID:    uuidPtr(artifact.FolderID),
		Title:       artifact.Title,
		Body:        artifact.Body,
		OwnerID:     uuidStr(noteRow.OwnerID),
		Visibility:  noteRow.Visibility,
		Pinned:      noteRow.Pinned,
		CreatedAt:   tsStr(artifact.CreatedAt),
		UpdatedAt:   tsStr(artifact.UpdatedAt),
	})
}

// UpdateNote edits a note's title and/or body. Only the owner may edit. The
// body/title live on the underlying artifact, so this loads the artifact,
// merges the provided fields, and writes it back via the upstream update.
func (h *Handler) UpdateNote(w http.ResponseWriter, r *http.Request) {
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
	var req updateNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	artifact, err := h.Upstream.GetArtifact(r.Context(), db.GetArtifactParams{ID: noteID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	title := artifact.Title
	if req.Title != nil {
		title = *req.Title
	}
	body := artifact.Body
	if req.Body != nil {
		body = *req.Body
	}
	updated, err := h.Upstream.UpdateArtifact(r.Context(), db.UpdateArtifactParams{
		ID:            noteID,
		WorkspaceID:   wsUUID,
		Title:         title,
		Body:          body,
		Metadata:      artifact.Metadata,
		FileUrl:       artifact.FileUrl,
		FileSizeBytes: artifact.FileSizeBytes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update note")
		return
	}
	noteRow, err := h.Cerebro.GetNote(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update note")
		return
	}

	// Notify only people newly tagged by this edit (diff vs. the old body).
	h.notifyNoteMentions(r.Context(), wsUUID, noteID, ownerUUID, updated.Title, artifact.Body, updated.Body, noteRow.Visibility)

	// Wave 3 / G2 — snapshot a version for the history. Coalesced: a burst of
	// edits by the same author becomes one entry (see snapshotVersion). Only
	// snapshot when something actually changed.
	if updated.Title != artifact.Title || updated.Body != artifact.Body {
		h.snapshotVersion(r.Context(), noteID, ownerUUID, updated.Title, updated.Body, "edit", "", true)
	}

	writeJSON(w, http.StatusOK, NoteResponse{
		ID:          uuidStr(updated.ID),
		WorkspaceID: uuidStr(updated.WorkspaceID),
		FolderID:    uuidPtr(updated.FolderID),
		Title:       updated.Title,
		Body:        updated.Body,
		OwnerID:     uuidStr(noteRow.OwnerID),
		Visibility:  noteRow.Visibility,
		Pinned:      noteRow.Pinned,
		CreatedAt:   tsStr(updated.CreatedAt),
		UpdatedAt:   tsStr(updated.UpdatedAt),
	})
}

// SetVisibility changes who can see a note. Only the owner may change it. When
// visibility is 'shared' the share list is replaced with shared_user_ids.
func (h *Handler) SetVisibility(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}

	var req setVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validVisibility[req.Visibility] {
		writeError(w, http.StatusBadRequest, "invalid visibility; expected private|shared|workspace")
		return
	}
	if err := h.Cerebro.SetNoteVisibility(r.Context(), cerebrodb.SetNoteVisibilityParams{
		ArtifactID: noteID,
		Visibility: req.Visibility,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update visibility")
		return
	}
	// Replace shares: clear, then re-add for 'shared'.
	if err := h.Cerebro.ReplaceNoteShares(r.Context(), noteID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update shares")
		return
	}
	if req.Visibility == "shared" {
		for _, uid := range req.SharedUserIDs {
			u, err := util.ParseUUID(uid)
			if err != nil {
				continue
			}
			_ = h.Cerebro.AddNoteShare(r.Context(), cerebrodb.AddNoteShareParams{ArtifactID: noteID, UserID: u})
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"visibility": req.Visibility})
}

// SetPin pins or unpins a note for the fast list. Only the owner may pin.
func (h *Handler) SetPin(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}
	var req setPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.Cerebro.SetNotePinned(r.Context(), cerebrodb.SetNotePinnedParams{
		ArtifactID: noteID,
		Pinned:     req.Pinned,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update pin")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"pinned": req.Pinned})
}

// DeleteNote removes a note: the cerebro_note row, its shares, and the
// underlying artifact. Only the owner may delete.
func (h *Handler) DeleteNote(w http.ResponseWriter, r *http.Request) {
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
	_ = h.Cerebro.ReplaceNoteShares(r.Context(), noteID)
	if err := h.Cerebro.DeleteNote(r.Context(), noteID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete note")
		return
	}
	if err := h.Upstream.DeleteArtifact(r.Context(), db.DeleteArtifactParams{ID: noteID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete note")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListShares returns the user IDs a 'shared' note is shared with.
func (h *Handler) ListShares(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}
	shares, err := h.Cerebro.ListNoteShares(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list shares")
		return
	}
	ids := make([]string, 0, len(shares))
	for _, s := range shares {
		ids = append(ids, uuidStr(s.UserID))
	}
	writeJSON(w, http.StatusOK, map[string][]string{"shared_user_ids": ids})
}

// --- helpers ---

// scope resolves the workspace + owner UUIDs from the request context and the
// authenticated user. Returns false (and writes an error) when either is bad.
func (h *Handler) scope(w http.ResponseWriter, r *http.Request, userID string) (pgtype.UUID, pgtype.UUID, bool) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	ownerUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return wsUUID, ownerUUID, true
}

// requireOwner returns true only when the caller owns the note.
func (h *Handler) requireOwner(w http.ResponseWriter, r *http.Request, noteID, ownerUUID pgtype.UUID) bool {
	noteRow, err := h.Cerebro.GetNote(r.Context(), noteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "note not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load note")
		}
		return false
	}
	if uuidStr(noteRow.OwnerID) != uuidStr(ownerUUID) {
		writeError(w, http.StatusForbidden, "only the owner can change this note")
		return false
	}
	return true
}

func pathUUID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	id := chi.URLParam(r, "id")
	u, err := util.ParseUUID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid note id")
		return pgtype.UUID{}, false
	}
	return u, true
}

func pageParams(r *http.Request) (limit, offset int32) {
	limit = defaultListLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > maxListLimit {
				n = maxListLimit
			}
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}
	return limit, offset
}

func listRowToResponse(n cerebrodb.ListNotesForUserRow) NoteResponse {
	return NoteResponse{
		ID:          uuidStr(n.ID),
		WorkspaceID: uuidStr(n.WorkspaceID),
		FolderID:    uuidPtr(n.FolderID),
		Title:       n.Title,
		Body:        n.Body,
		OwnerID:     uuidStr(n.OwnerID),
		Visibility:  n.Visibility,
		Pinned:      n.Pinned,
		CreatedAt:   tsStr(n.CreatedAt),
		UpdatedAt:   tsStr(n.UpdatedAt),
	}
}

func searchRowToResponse(n cerebrodb.SearchNotesForUserRow) NoteResponse {
	return NoteResponse{
		ID:          uuidStr(n.ID),
		WorkspaceID: uuidStr(n.WorkspaceID),
		FolderID:    uuidPtr(n.FolderID),
		Title:       n.Title,
		Body:        n.Body,
		OwnerID:     uuidStr(n.OwnerID),
		Visibility:  n.Visibility,
		Pinned:      n.Pinned,
		CreatedAt:   tsStr(n.CreatedAt),
		UpdatedAt:   tsStr(n.UpdatedAt),
	}
}

func recentRowToResponse(n cerebrodb.ListRecentNotesForUserRow) NoteResponse {
	return NoteResponse{
		ID:          uuidStr(n.ID),
		WorkspaceID: uuidStr(n.WorkspaceID),
		FolderID:    uuidPtr(n.FolderID),
		Title:       n.Title,
		Body:        n.Body,
		OwnerID:     uuidStr(n.OwnerID),
		Visibility:  n.Visibility,
		Pinned:      n.Pinned,
		CreatedAt:   tsStr(n.CreatedAt),
		UpdatedAt:   tsStr(n.UpdatedAt),
	}
}

func mustUUID(s string) pgtype.UUID {
	u, _ := util.ParseUUID(s)
	return u
}

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := uuid.UUID(u.Bytes).String()
	return &s
}

func tsStr(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
