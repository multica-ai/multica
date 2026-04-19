package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
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
		ActorType: r.ActorType.String,
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

	// Phase 3d write-through: on a GitLab-connected workspace, human-authored
	// reactions on comments go to GitLab's note award_emoji endpoint first,
	// then the returned award is upserted into the cache. Agent reactions skip
	// GitLab entirely and fall through to the legacy Multica-only path —
	// GitLab's award_emoji endpoint can't attribute awards to Multica agents.
	//
	// Unicode emojis outside our translation map also fall through to the
	// legacy Multica-only path — GitLab's award_emoji requires a named
	// shortcode, and we don't have one for arbitrary picks.
	if h.GitlabEnabled && h.GitlabResolver != nil && actorType != "agent" {
		if shortcode, ok := gitlabsync.EmojiUnicodeToShortcode(req.Emoji); ok {
			_, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), comment.WorkspaceID)
			if wsErr == nil {
				issue, issueErr := h.Queries.GetIssue(r.Context(), comment.IssueID)
				if issueErr != nil {
					slog.Error("load parent issue for gitlab reaction write-through",
						"error", issueErr, "comment_id", commentId)
					writeError(w, http.StatusInternalServerError, "failed to load issue")
					return
				}
				if !issue.GitlabIid.Valid || !issue.GitlabProjectID.Valid || !comment.GitlabNoteID.Valid {
					slog.Error("gitlab connected workspace but comment/issue missing gitlab refs",
						"comment_id", commentId, "workspace_id", workspaceID)
					writeError(w, http.StatusBadGateway, "comment or issue not linked to gitlab")
					return
				}
				h.addCommentReactionWriteThrough(w, r, comment, issue, req.Emoji, shortcode, actorType, actorID, workspaceID, commentId)
				return
			}
			// wsErr != nil → fall through to legacy path (non-connected workspace).
		}
		// Unsupported emoji → fall through to Multica-only path.
	}

	reaction, err := h.Queries.AddReaction(r.Context(), db.AddReactionParams{
		CommentID:   comment.ID,
		WorkspaceID: parseUUID(workspaceID),
		ActorType:   pgtype.Text{String: actorType, Valid: actorType != ""},
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
		"comment_author_type": comment.AuthorType.String,
		"comment_author_id":   uuidToString(comment.AuthorID),
	})
	writeJSON(w, http.StatusCreated, resp)
}

// addCommentReactionWriteThrough implements the Phase 3d write-through branch
// of POST /api/comments/{id}/reactions: POST the award_emoji to GitLab's note
// endpoint, then upsert the cache row from the returned representation keyed
// by gitlab_award_id.
//
// On GitLab error returns a non-2xx status and aborts — we must NOT fall
// through to the legacy path, which would produce orphaned cache rows on a
// connected workspace.
func (h *Handler) addCommentReactionWriteThrough(
	w http.ResponseWriter,
	r *http.Request,
	comment db.Comment,
	issue db.Issue,
	unicodeEmoji, gitlabShortcode string,
	actorType, actorID, workspaceID, commentID string,
) {
	ctx := r.Context()

	token, _, err := h.GitlabResolver.ResolveTokenForWrite(ctx, workspaceID, actorType, actorID)
	if err != nil {
		slog.Error("resolve gitlab token", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
		return
	}

	award, err := h.Gitlab.CreateNoteAwardEmoji(ctx,
		token,
		issue.GitlabProjectID.Int64,
		int(issue.GitlabIid.Int32),
		comment.GitlabNoteID.Int64,
		gitlabShortcode,
	)
	if err != nil {
		slog.Error("gitlab create note award_emoji", "error", err, "comment_id", commentID)
		writeError(w, http.StatusBadGateway, "gitlab create award_emoji failed")
		return
	}

	var glActor pgtype.Int8
	if award.User.ID != 0 {
		glActor = pgtype.Int8{Int64: award.User.ID, Valid: true}
	}
	externalUpdatedAt := parseGitlabTS(award.UpdatedAt)

	// Cache stores unicode (Multica convention) so the frontend renders the
	// emoji natively. award.Name is the GitLab shortcode — write the user's
	// original unicode instead.
	row, upErr := h.Queries.UpsertCommentReactionFromGitlab(ctx, db.UpsertCommentReactionFromGitlabParams{
		WorkspaceID:       comment.WorkspaceID,
		CommentID:         comment.ID,
		ActorType:         pgtype.Text{String: actorType, Valid: actorType != ""},
		ActorID:           parseUUID(actorID),
		GitlabActorUserID: glActor,
		Emoji:             unicodeEmoji,
		GitlabAwardID:     pgtype.Int8{Int64: award.ID, Valid: true},
		ExternalUpdatedAt: externalUpdatedAt,
	})
	if upErr != nil {
		if errors.Is(upErr, pgx.ErrNoRows) {
			// Clobber guard short-circuited: a concurrent webhook wrote a
			// newer-or-equal row. Load the existing cache copy so the
			// response reflects reality.
			loaded, loadErr := h.Queries.GetCommentReactionByGitlabAwardID(ctx,
				pgtype.Int8{Int64: award.ID, Valid: true})
			if loadErr != nil {
				slog.Error("load comment reaction after clobber-guard short-circuit",
					"error", loadErr, "gitlab_award_id", award.ID)
				writeError(w, http.StatusInternalServerError, "failed to add reaction")
				return
			}
			row = loaded
		} else {
			slog.Error("upsert gitlab comment reaction cache row", "error", upErr)
			writeError(w, http.StatusInternalServerError, "cache upsert failed")
			return
		}
	}

	resp := reactionToResponse(row)
	slog.Info("comment reaction added (gitlab write-through)",
		append(logger.RequestAttrs(r), "reaction_id", uuidToString(row.ID),
			"comment_id", commentID, "gitlab_award_id", award.ID)...)
	h.publish(protocol.EventReactionAdded, workspaceID, actorType, actorID, map[string]any{
		"reaction":            resp,
		"issue_id":            uuidToString(comment.IssueID),
		"issue_title":         issue.Title,
		"issue_status":        issue.Status,
		"comment_id":          uuidToString(comment.ID),
		"comment_author_type": comment.AuthorType.String,
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

	// Phase 3d write-through: on a GitLab-connected workspace, human-authored
	// reaction removals look up the cache row's gitlab_award_id, DELETE the
	// award on GitLab's note endpoint, then drop the local row. Agent
	// reactions have no GitLab representation — they fall through to the
	// legacy path.
	if h.GitlabEnabled && h.GitlabResolver != nil && actorType != "agent" {
		_, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), comment.WorkspaceID)
		if wsErr == nil {
			existing, exErr := h.Queries.GetCommentReactionByKey(r.Context(), db.GetCommentReactionByKeyParams{
				CommentID: comment.ID,
				ActorType: pgtype.Text{String: actorType, Valid: actorType != ""},
				ActorID:   parseUUID(actorID),
				Emoji:     req.Emoji,
			})
			if exErr != nil {
				if errors.Is(exErr, pgx.ErrNoRows) {
					// No local row — idempotent success. No GitLab call.
					w.WriteHeader(http.StatusNoContent)
					return
				}
				slog.Error("lookup comment reaction for delete", "error", exErr, "comment_id", commentId)
				writeError(w, http.StatusInternalServerError, "failed to remove reaction")
				return
			}

			issue, issueErr := h.Queries.GetIssue(r.Context(), comment.IssueID)
			if issueErr != nil {
				slog.Error("load parent issue for gitlab reaction delete",
					"error", issueErr, "comment_id", commentId)
				writeError(w, http.StatusInternalServerError, "failed to load issue")
				return
			}
			if !issue.GitlabIid.Valid || !issue.GitlabProjectID.Valid || !comment.GitlabNoteID.Valid {
				slog.Error("gitlab connected workspace but comment/issue missing gitlab refs",
					"comment_id", commentId, "workspace_id", workspaceID)
				writeError(w, http.StatusBadGateway, "comment or issue not linked to gitlab")
				return
			}

			if existing.GitlabAwardID.Valid {
				token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
				if tokErr != nil {
					slog.Error("resolve gitlab token", "error", tokErr, "workspace_id", workspaceID)
					writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
					return
				}

				if err := h.Gitlab.DeleteNoteAwardEmoji(r.Context(), token,
					issue.GitlabProjectID.Int64, int(issue.GitlabIid.Int32),
					comment.GitlabNoteID.Int64, existing.GitlabAwardID.Int64); err != nil {
					slog.Error("gitlab delete note award_emoji", "error", err, "comment_id", commentId)
					writeError(w, http.StatusBadGateway, "gitlab delete award_emoji failed")
					return
				}
			}
			// No gitlab_award_id → Multica-native row (pre-connection). Just drop locally.

			if err := h.Queries.DeleteCommentReactionByID(r.Context(), existing.ID); err != nil {
				slog.Error("delete comment reaction cache row", "error", err, "reaction_id", uuidToString(existing.ID))
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
			return
		}
		// wsErr != nil → fall through to legacy path (non-connected workspace).
	}

	if err := h.Queries.RemoveReaction(r.Context(), db.RemoveReactionParams{
		CommentID: comment.ID,
		ActorType: pgtype.Text{String: actorType, Valid: actorType != ""},
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
