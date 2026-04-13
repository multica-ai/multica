package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SubscriberResponse is the JSON shape returned for each issue subscriber.
type SubscriberResponse struct {
	IssueID   string `json:"issue_id"`
	UserType  string `json:"user_type"`
	UserID    string `json:"user_id"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

func subscriberToResponse(s db.IssueSubscriber) SubscriberResponse {
	return SubscriberResponse{
		IssueID:   uuidToString(s.IssueID),
		UserType:  s.UserType,
		UserID:    uuidToString(s.UserID),
		Reason:    s.Reason,
		CreatedAt: timestampToString(s.CreatedAt),
	}
}

type issueSubscriberRequest struct {
	UserID   *string `json:"user_id"`
	UserType *string `json:"user_type"`
}

func (h *Handler) resolveIssueSubscriberTarget(w http.ResponseWriter, r *http.Request, workspaceID string) (string, string, bool) {
	callerID, ok := requireUserID(w, r)
	if !ok {
		return "", "", false
	}

	targetUserID := callerID
	targetUserType := "member"

	var req issueSubscriberRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return "", "", false
		}
	}

	if req.UserID != nil && *req.UserID != "" {
		targetUserID = *req.UserID
	}
	if req.UserType != nil && *req.UserType != "" {
		targetUserType = *req.UserType
	}

	switch targetUserType {
	case "member":
		if targetUserID != callerID {
			if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "issue not found", "owner", "admin"); !ok {
				return "", "", false
			}
		}
		if _, err := h.getWorkspaceMember(r.Context(), targetUserID, workspaceID); err != nil {
			writeError(w, http.StatusNotFound, "subscriber target not found")
			return "", "", false
		}
	case "agent":
		if req.UserID == nil || *req.UserID == "" {
			writeError(w, http.StatusBadRequest, "user_id is required for agent subscribers")
			return "", "", false
		}
		if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "issue not found", "owner", "admin"); !ok {
			return "", "", false
		}
		if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          parseUUID(targetUserID),
			WorkspaceID: parseUUID(workspaceID),
		}); err != nil {
			writeError(w, http.StatusNotFound, "subscriber target not found")
			return "", "", false
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid user_type")
		return "", "", false
	}

	return targetUserType, targetUserID, true
}

// ListIssueSubscribers returns all subscribers for an issue.
func (h *Handler) ListIssueSubscribers(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	subscribers, err := h.Queries.ListIssueSubscribers(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list subscribers")
		return
	}

	resp := make([]SubscriberResponse, len(subscribers))
	for i, s := range subscribers {
		resp[i] = subscriberToResponse(s)
	}

	writeJSON(w, http.StatusOK, resp)
}

// SubscribeToIssue subscribes a user to an issue with reason "manual".
// If request body contains user_id, subscribes that user; otherwise subscribes the caller.
func (h *Handler) SubscribeToIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	targetUserType, targetUserID, ok := h.resolveIssueSubscriberTarget(w, r, workspaceID)
	if !ok {
		return
	}

	err := h.Queries.AddIssueSubscriber(r.Context(), db.AddIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: targetUserType,
		UserID:   parseUUID(targetUserID),
		Reason:   "manual",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe")
		return
	}

	callerID := requestUserID(r)
	subActorType, subActorID := h.resolveActor(r, callerID, workspaceID)
	h.publish(protocol.EventSubscriberAdded, workspaceID, subActorType, subActorID, map[string]any{
		"issue_id":  issueID,
		"user_type": targetUserType,
		"user_id":   targetUserID,
		"reason":    "manual",
	})

	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": true})
}

// UnsubscribeFromIssue removes a user's subscription from an issue.
// If request body contains user_id, unsubscribes that user; otherwise unsubscribes the caller.
func (h *Handler) UnsubscribeFromIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	targetUserType, targetUserID, ok := h.resolveIssueSubscriberTarget(w, r, workspaceID)
	if !ok {
		return
	}

	err := h.Queries.RemoveIssueSubscriber(r.Context(), db.RemoveIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: targetUserType,
		UserID:   parseUUID(targetUserID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unsubscribe")
		return
	}

	callerID := requestUserID(r)
	unsubActorType, unsubActorID := h.resolveActor(r, callerID, workspaceID)
	h.publish(protocol.EventSubscriberRemoved, workspaceID, unsubActorType, unsubActorID, map[string]any{
		"issue_id":  issueID,
		"user_type": targetUserType,
		"user_id":   targetUserID,
	})

	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": false})
}
