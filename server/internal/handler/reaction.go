package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ReactionResponse struct {
	ID        string `json:"id"`
	CommentID string `json:"comment_id"`
	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
	Emoji     string `json:"emoji"`
	CreatedAt string `json:"created_at"`
}

func reactionToResponse(r db.CommentReaction) ReactionResponse {
	return ReactionResponse{
		ID:        uuidToString(r.ID),
		CommentID: uuidToString(r.CommentID),
		ActorType: r.ActorType,
		ActorID:   uuidToString(r.ActorID),
		Emoji:     r.Emoji,
		CreatedAt: timestampToString(r.CreatedAt),
	}
}

func (h *Handler) AddReaction(w http.ResponseWriter, r *http.Request) {
	commentId := chi.URLParam(r, "commentId")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          parseUUID(commentId),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
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

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	reaction, err := h.Queries.AddReaction(r.Context(), db.AddReactionParams{
		CommentID:   comment.ID,
		WorkspaceID: parseUUID(workspaceID),
		ActorType:   actorType,
		ActorID:     parseUUID(actorID),
		Emoji:       req.Emoji,
	})
	if err != nil {
		slog.Warn("add reaction failed", append(logger.RequestAttrs(r), "error", err, "comment_id", commentId)...)
		writeError(w, http.StatusInternalServerError, "failed to add reaction")
		return
	}

	resp := reactionToResponse(reaction)

	// Look up issue title for inbox notifications.
	issueID := uuidToString(comment.IssueID)
	var issueTitle, issueStatus string
	if issue, err := h.Queries.GetIssue(r.Context(), comment.IssueID); err == nil {
		issueTitle = issue.Title
		issueStatus = issue.Status
	}

	h.publish(protocol.EventReactionAdded, workspaceID, actorType, actorID, map[string]any{
		"reaction":            resp,
		"issue_id":            issueID,
		"issue_title":         issueTitle,
		"issue_status":        issueStatus,
		"comment_id":          uuidToString(comment.ID),
		"comment_author_type": comment.AuthorType,
		"comment_author_id":   uuidToString(comment.AuthorID),
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) RemoveReaction(w http.ResponseWriter, r *http.Request) {
	commentId := chi.URLParam(r, "commentId")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          parseUUID(commentId),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
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

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	if err := h.Queries.RemoveReaction(r.Context(), db.RemoveReactionParams{
		CommentID: comment.ID,
		ActorType: actorType,
		ActorID:   parseUUID(actorID),
		Emoji:     req.Emoji,
	}); err != nil {
		slog.Warn("remove reaction failed", append(logger.RequestAttrs(r), "error", err, "comment_id", commentId)...)
		writeError(w, http.StatusInternalServerError, "failed to remove reaction")
		return
	}

	h.publish(protocol.EventReactionRemoved, workspaceID, actorType, actorID, map[string]any{
		"comment_id": commentId,
		"issue_id":   uuidToString(comment.IssueID),
		"emoji":      req.Emoji,
		"actor_type": actorType,
		"actor_id":   actorID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// groupReactions fetches reactions for the given comment IDs and groups them by comment_id.
func (h *Handler) groupReactions(r *http.Request, commentIDs []pgtype.UUID) map[string][]ReactionResponse {
	if len(commentIDs) == 0 {
		return nil
	}
	reactions, err := h.Queries.ListReactionsByCommentIDs(r.Context(), commentIDs)
	if err != nil {
		return nil
	}
	grouped := make(map[string][]ReactionResponse, len(commentIDs))
	for _, rx := range reactions {
		cid := uuidToString(rx.CommentID)
		grouped[cid] = append(grouped[cid], reactionToResponse(rx))
	}
	return grouped
}
