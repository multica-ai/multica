package handler

import (
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ProjectSubscriptionResponse struct {
	Subscribed bool `json:"subscribed"`
}

// loadProjectForSubscription resolves the visible project and the human
// caller. Project subscriptions belong to members: an agent already hears
// about its issues through assignment and mentions.
func (h *Handler) loadProjectForSubscription(w http.ResponseWriter, r *http.Request) (db.Project, string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Project{}, "", false
	}
	project, ok := h.loadVisibleProject(w, r)
	if !ok {
		return db.Project{}, "", false
	}
	if actorType, _ := h.resolveActor(r, userID, uuidToString(project.WorkspaceID)); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot subscribe to projects")
		return db.Project{}, "", false
	}
	return project, userID, true
}

func (h *Handler) writeProjectSubscription(w http.ResponseWriter, r *http.Request, project db.Project, userID string) {
	subscribed, err := h.Queries.IsProjectSubscriber(r.Context(), db.IsProjectSubscriberParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project subscription")
		return
	}
	writeJSON(w, http.StatusOK, ProjectSubscriptionResponse{Subscribed: subscribed})
}

// GetProjectSubscription — GET /api/projects/{id}/subscription
func (h *Handler) GetProjectSubscription(w http.ResponseWriter, r *http.Request) {
	project, userID, ok := h.loadProjectForSubscription(w, r)
	if !ok {
		return
	}
	h.writeProjectSubscription(w, r, project, userID)
}

// SubscribeToProject — PUT /api/projects/{id}/subscription
func (h *Handler) SubscribeToProject(w http.ResponseWriter, r *http.Request) {
	project, userID, ok := h.loadProjectForSubscription(w, r)
	if !ok {
		return
	}
	if err := h.Queries.AddProjectSubscriber(r.Context(), db.AddProjectSubscriberParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
		UserID:      parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe to project")
		return
	}
	h.writeProjectSubscription(w, r, project, userID)
}

// UnsubscribeFromProject — DELETE /api/projects/{id}/subscription
func (h *Handler) UnsubscribeFromProject(w http.ResponseWriter, r *http.Request) {
	project, userID, ok := h.loadProjectForSubscription(w, r)
	if !ok {
		return
	}
	if err := h.Queries.RemoveProjectSubscriber(r.Context(), db.RemoveProjectSubscriberParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
		UserID:      parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unsubscribe from project")
		return
	}
	writeJSON(w, http.StatusOK, ProjectSubscriptionResponse{Subscribed: false})
}
