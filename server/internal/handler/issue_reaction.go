package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type IssueReactionResponse struct {
	ID        string `json:"id"`
	IssueID   string `json:"issue_id"`
	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
	Emoji     string `json:"emoji"`
	CreatedAt string `json:"created_at"`
}

func issueReactionToResponse(r db.IssueReaction) IssueReactionResponse {
	return IssueReactionResponse{
		ID:        uuidToString(r.ID),
		IssueID:   uuidToString(r.IssueID),
		ActorType: r.ActorType,
		ActorID:   uuidToString(r.ActorID),
		Emoji:     r.Emoji,
		CreatedAt: timestampToString(r.CreatedAt),
	}
}

func (h *Handler) AddIssueReaction(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Emoji == "" {
		writeError(w, http.StatusBadRequest, "emoji is required")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	reaction, err := h.Queries.AddIssueReaction(r.Context(), db.AddIssueReactionParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		ActorType:   actorType,
		ActorID:     parseUUID(actorID),
		Emoji:       req.Emoji,
	})
	if err != nil {
		slog.Warn("add issue reaction failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to add reaction")
		return
	}

	resp := issueReactionToResponse(reaction)
	h.publish(protocol.EventIssueReactionAdded, workspaceID, actorType, actorID, map[string]any{
		"reaction":     resp,
		"issue_id":     uuidToString(issue.ID),
		"issue_title":  issue.Title,
		"issue_status": issue.Status,
		"creator_type": issue.CreatorType,
		"creator_id":   uuidToString(issue.CreatorID),
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) RemoveIssueReaction(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Emoji == "" {
		writeError(w, http.StatusBadRequest, "emoji is required")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	if err := h.Queries.RemoveIssueReaction(r.Context(), db.RemoveIssueReactionParams{
		IssueID:   issue.ID,
		ActorType: actorType,
		ActorID:   parseUUID(actorID),
		Emoji:     req.Emoji,
	}); err != nil {
		slog.Warn("remove issue reaction failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to remove reaction")
		return
	}

	h.publish(protocol.EventIssueReactionRemoved, workspaceID, actorType, actorID, map[string]any{
		"issue_id":   uuidToString(issue.ID),
		"emoji":      req.Emoji,
		"actor_type": actorType,
		"actor_id":   actorID,
	})
	w.WriteHeader(http.StatusNoContent)
}
