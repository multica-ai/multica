package handler

import (
	"errors"
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
)

// writeKnowledgeUploadError answers an upload failure. An oversized file is 413;
// everything else is the general knowledge mapping.
func writeKnowledgeUploadError(w http.ResponseWriter, r *http.Request, workspaceID string, err error) {
	var tooLarge *service.KnowledgeTooLargeError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, tooLarge.Error())
		return
	}
	writeKnowledgeError(w, r, workspaceID, err)
}

// uploadFormOverhead is the room kept beyond the file for the form fields and
// the multipart framing.
const uploadFormOverhead = 1 << 20

// uploadMemory is how much of an upload stays in memory before it spills to disk.
const uploadMemory = 4 << 20

// UploadKnowledgeDocument (POST /api/knowledge/documents/upload) stores a file
// in a knowledge base and publishes it at once: the person is the curator. The
// form carries file, kb (the base key), kind (required) and an optional
// idempotency_key. Agents are refused by the route; they write drafts.
func (h *Handler) UploadKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	limit := h.Knowledge.UploadLimit()
	r.Body = http.MaxBytesReader(w, r.Body, limit+uploadFormOverhead)
	if err := r.ParseMultipartForm(uploadMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeKnowledgeUploadError(w, r, workspaceID, &service.KnowledgeTooLargeError{Max: limit})
			return
		}
		writeError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "a file is required")
		return
	}
	defer file.Close()
	if header.Size > limit {
		writeKnowledgeUploadError(w, r, workspaceID, &service.KnowledgeTooLargeError{Max: limit})
		return
	}

	// The service refuses an agent: agents write drafts, they do not upload.
	actor := service.KnowledgeActor{
		Type:    actorType,
		ID:      actorUUID,
		Curator: h.knowledgeCurator(r, actorType),
	}
	view, created, err := h.Knowledge.UploadDocument(r.Context(), parseUUID(workspaceID), actor, service.KnowledgeUploadInput{
		KB:             r.FormValue("kb"),
		Kind:           r.FormValue("kind"),
		Name:           header.Filename,
		Size:           header.Size,
		Content:        file,
		IdempotencyKey: r.FormValue("idempotency_key"),
	})
	if err != nil {
		writeKnowledgeUploadError(w, r, workspaceID, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, view)
}

// ListKnowledgeSources (GET /api/knowledge/sources) lists the data sources of
// every knowledge base, with their last sync and failures. Owners and admins
// only; creating a source is done in WeKnora's UI, which the response links to.
func (h *Handler) ListKnowledgeSources(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	view, err := h.Knowledge.ListSources(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
