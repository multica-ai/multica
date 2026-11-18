package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projectaccess"
)

// The exported surface server/ee builds its handlers on. Commercial packages
// mount routes through package extension and reach request context, events
// and visibility only through these, so open-source code never imports them.

// WorkspaceID is the workspace the request is scoped to.
func (h *Handler) WorkspaceID(r *http.Request) string { return h.resolveWorkspaceID(r) }

// Actor is who the request acts as in the workspace: a member, or the agent
// running on the member's behalf.
func (h *Handler) Actor(r *http.Request, userID, workspaceID string) (actorType, actorID string) {
	return h.resolveActor(r, userID, workspaceID)
}

// Publish broadcasts a realtime event to the workspace.
func (h *Handler) Publish(eventType, workspaceID, actorType, actorID string, payload any) {
	h.publish(eventType, workspaceID, actorType, actorID, payload)
}

// RequireProjectVisible answers 404 unless the caller can see the project.
func (h *Handler) RequireProjectVisible(w http.ResponseWriter, r *http.Request, workspaceID string, projectID pgtype.UUID) bool {
	return h.requireProjectVisible(w, r, workspaceID, projectID, http.StatusNotFound, "project not found")
}

// HiddenProjects resolves the projects the caller cannot see, answering 500
// when it cannot.
func (h *Handler) HiddenProjects(w http.ResponseWriter, r *http.Request, workspaceID string) (projectaccess.Hidden, bool) {
	return h.requestHiddenProjects(w, r, workspaceID)
}

func RequireUserID(w http.ResponseWriter, r *http.Request) (string, bool) { return requireUserID(w, r) }

func WriteJSON(w http.ResponseWriter, status int, v any) { writeJSON(w, status, v) }

func WriteError(w http.ResponseWriter, status int, msg string) { writeError(w, status, msg) }

func ParseUUIDOrBadRequest(w http.ResponseWriter, s, fieldName string) (pgtype.UUID, bool) {
	return parseUUIDOrBadRequest(w, s, fieldName)
}

func IsUniqueViolation(err error) bool { return isUniqueViolation(err) }
