// References on notes (TECH-3421). Mirrors the issue-reference feature
// (server/internal/cerebro/references) but keyed by note_id (the note's
// artifact id). A note reference is a generic typed pointer attached to a
// note — an issue today, business objects later. The table shape is
// intentionally generic: `(object, type, ref_id)` describes what is being
// pointed at, `metadata` carries object-specific fields, so new reference
// kinds can be added without a migration per kind.
//
// These handlers are registered on the existing note.Handler.Routes under
// /api/notes/{id}/references, so they reuse the same scope/owner gating and
// the writeJSON/writeError/uuidStr/tsStr helpers already in this package.
package note

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// WS event types broadcast on the workspace channel when a note reference is
// created or removed. The cerebro prefix mirrors the issue-reference events so
// frontends can subscribe without ambiguity.
const (
	EventNoteReferenceCreated = "cerebro.note_reference.created"
	EventNoteReferenceDeleted = "cerebro.note_reference.deleted"
)

// NoteReferenceResponse is the JSON shape returned to clients and embedded in
// WS payloads. Mirrors references.ReferenceResponse but with note_id in place
// of issue_id. Keeps the API stable even if the underlying column types change.
type NoteReferenceResponse struct {
	ID            string          `json:"id"`
	NoteID        string          `json:"note_id"`
	Object        string          `json:"object"`
	Type          *string         `json:"type"`
	RefID         string          `json:"ref_id"`
	Label         *string         `json:"label"`
	URL           *string         `json:"url"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedByType string          `json:"created_by_type"`
	CreatedByID   string          `json:"created_by_id"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

func noteRefToResponse(r cerebrodb.CerebroNoteReference) NoteReferenceResponse {
	meta := json.RawMessage(r.Metadata)
	if len(meta) == 0 {
		meta = json.RawMessage("{}")
	}
	return NoteReferenceResponse{
		ID:            uuidStr(r.ID),
		NoteID:        uuidStr(r.NoteID),
		Object:        r.Object,
		Type:          util.TextToPtr(r.Type),
		RefID:         r.RefID,
		Label:         util.TextToPtr(r.Label),
		URL:           util.TextToPtr(r.Url),
		Metadata:      meta,
		CreatedByType: r.CreatedByType,
		CreatedByID:   uuidStr(r.CreatedByID),
		CreatedAt:     tsStr(r.CreatedAt),
		UpdatedAt:     tsStr(r.UpdatedAt),
	}
}

// createNoteReferenceRequest is the POST body for /api/notes/{id}/references.
// `object` and `ref_id` are required; everything else is optional. `metadata`
// is raw JSON so callers can attach payload-specific fields.
type createNoteReferenceRequest struct {
	Object   string          `json:"object"`
	Type     *string         `json:"type"`
	RefID    string          `json:"ref_id"`
	Label    *string         `json:"label"`
	URL      *string         `json:"url"`
	Metadata json.RawMessage `json:"metadata"`
}

// ListReferences handles GET /api/notes/{id}/references. The caller must be
// able to see the note (CanUserSeeNote), then we return every reference
// attached to it, oldest pin first.
func (h *Handler) ListReferences(w http.ResponseWriter, r *http.Request) {
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
	allowed, err := h.Cerebro.CanUserSeeNote(r.Context(), cerebrodb.CanUserSeeNoteParams{
		ArtifactID: noteID,
		OwnerID:    ownerUUID,
	})
	if err != nil || !allowed {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	rows, err := h.Cerebro.ListNoteReferencesByNote(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load references")
		return
	}
	out := make([]NoteReferenceResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, noteRefToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"references": out})
}

// CreateReference handles POST /api/notes/{id}/references. Owner-only. UPSERT
// semantics on (note_id, object, ref_id): re-posting the same identifier
// refreshes type/label/url/metadata and the updated_at timestamp without
// rewriting the original creator. Returns 201 on first insert, 200 on update.
func (h *Handler) CreateReference(w http.ResponseWriter, r *http.Request) {
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

	var req createNoteReferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Object = strings.TrimSpace(req.Object)
	req.RefID = strings.TrimSpace(req.RefID)
	if req.Object == "" {
		writeError(w, http.StatusBadRequest, "object is required")
		return
	}
	if req.RefID == "" {
		writeError(w, http.StatusBadRequest, "ref_id is required")
		return
	}
	metadata := normalizeNoteRefMetadata(req.Metadata)
	if metadata == nil {
		writeError(w, http.StatusBadRequest, "metadata must be a JSON object")
		return
	}

	row, err := h.Cerebro.UpsertNoteReference(r.Context(), cerebrodb.UpsertNoteReferenceParams{
		NoteID:        noteID,
		Object:        req.Object,
		Type:          util.PtrToText(req.Type),
		RefID:         req.RefID,
		Label:         util.PtrToText(req.Label),
		Url:           util.PtrToText(req.URL),
		Metadata:      metadata,
		CreatedByType: "member",
		CreatedByID:   ownerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create reference")
		return
	}

	resp := noteRefToResponse(row)
	h.publishNoteReference(EventNoteReferenceCreated, uuidStr(wsUUID), "member", uuidStr(ownerUUID), resp)

	status := http.StatusCreated
	if !row.CreatedAt.Time.Equal(row.UpdatedAt.Time) {
		status = http.StatusOK
	}
	writeJSON(w, status, resp)
}

// DeleteReference handles DELETE /api/notes/{id}/references/{refId}. Owner-only.
// Returns 204 on success; the WS event carries the deleted row so subscribers
// can drop it from their cache without a re-fetch.
func (h *Handler) DeleteReference(w http.ResponseWriter, r *http.Request) {
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
	refUUID, err := util.ParseUUID(chi.URLParam(r, "refId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reference id")
		return
	}
	ref, err := h.Cerebro.GetNoteReference(r.Context(), refUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "reference not found")
		return
	}
	// Collapse cross-note access to 404 so callers can't probe reference IDs
	// attached to other notes via this note's owner check.
	if uuidStr(ref.NoteID) != uuidStr(noteID) {
		writeError(w, http.StatusNotFound, "reference not found")
		return
	}
	row, err := h.Cerebro.DeleteNoteReference(r.Context(), ref.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "reference not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete reference")
		return
	}
	resp := noteRefToResponse(row)
	h.publishNoteReference(EventNoteReferenceDeleted, uuidStr(wsUUID), "member", uuidStr(ownerUUID), resp)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) publishNoteReference(eventType, workspaceID, actorType, actorID string, payload any) {
	if h.Bus == nil || workspaceID == "" {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload:     payload,
	})
}

// normalizeNoteRefMetadata coerces the request body's metadata field into the
// JSONB shape the database expects. Empty / null becomes "{}"; non-object JSON
// (arrays, scalars) and malformed JSON are rejected with nil so the caller can
// return 400.
func normalizeNoteRefMetadata(raw json.RawMessage) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []byte("{}")
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil
	}
	return []byte(trimmed)
}
