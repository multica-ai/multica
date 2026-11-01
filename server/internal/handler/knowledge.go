package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

// requireKnowledgeAdmin gates the knowledge endpoints. The router already
// restricts them to owner and admin; this keeps an agent running under an
// owner's token from enabling the sidecar, which is a human decision.
func (h *Handler) requireKnowledgeAdmin(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot manage workspace knowledge")
		return false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return false
	}
	return true
}

// GetKnowledge (GET /api/knowledge) reports whether knowledge is offered and
// enabled, its state, and its knowledge bases. A sidecar that is down is the
// "offline" state, never an error.
func (h *Handler) GetKnowledge(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return
	}
	status, err := h.Knowledge.Status(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("knowledge status failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to load knowledge status")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// EnableKnowledge (POST /api/knowledge/enable) provisions knowledge for the
// workspace. It is idempotent and resumable: calling it again after a failure
// continues where the last attempt stopped.
func (h *Handler) EnableKnowledge(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	status, err := h.Knowledge.Enable(r.Context(), parseUUID(workspaceID), parseUUID(userID))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, status)
		return
	case errors.Is(err, service.ErrKnowledgeNotOffered):
		writeError(w, http.StatusNotFound, "knowledge is not offered on this deployment")
	case errors.Is(err, service.ErrKnowledgeBusy):
		writeError(w, http.StatusConflict, "knowledge is already being set up for this workspace")
	case errors.Is(err, knowledge.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "the knowledge service is offline")
	case errors.Is(err, knowledge.ErrAuth), errors.Is(err, knowledge.ErrForbidden):
		writeError(w, http.StatusBadGateway, "the knowledge service rejected Multica's service account")
	case errors.Is(err, knowledge.ErrQuota):
		writeError(w, http.StatusBadGateway, "the knowledge service refused to create another workspace")
	default:
		writeError(w, http.StatusInternalServerError, "failed to enable knowledge")
	}
	if !errors.Is(err, service.ErrKnowledgeNotOffered) {
		slog.Warn("enable knowledge failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
	}
}
