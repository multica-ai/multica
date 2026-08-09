package handler

// CEREBRO-PATCH(document-image-attachments): FIR-4699 — GET
// /api/artifacts/{id}/attachments lists the images owned by a document,
// mirroring GET /api/issues/{id}/attachments. Workspace-scoped: the artifact
// must belong to the caller's workspace and sit in a folder the caller may see.

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ListArtifactAttachments — GET /api/artifacts/{id}/attachments
func (h *Handler) ListArtifactAttachments(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	artifact, err := h.Queries.GetArtifact(r.Context(), db.GetArtifactParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil || !h.folderVisibleToCaller(r, artifact.FolderID) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}

	attachments, err := h.Queries.ListAttachmentsByArtifact(r.Context(), db.ListAttachmentsByArtifactParams{
		ArtifactID:  artifact.ID,
		WorkspaceID: artifact.WorkspaceID,
	})
	if err != nil {
		slog.Error("failed to list artifact attachments", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list attachments")
		return
	}

	resp := make([]AttachmentResponse, len(attachments))
	for i, a := range attachments {
		resp[i] = h.attachmentToResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}
