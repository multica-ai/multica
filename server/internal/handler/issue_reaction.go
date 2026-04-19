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
		ActorType: r.ActorType.String,
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

	// Phase 3d write-through: on a GitLab-connected workspace, human-authored
	// reactions go to GitLab first, then the returned award is upserted into
	// the cache. Agent reactions skip GitLab entirely and fall through to the
	// legacy Multica-only path — GitLab's award_emoji endpoint can't attribute
	// awards to Multica agents, so agent reactions stay local.
	//
	// Unicode emojis outside our translation map also fall through to the
	// legacy Multica-only path — GitLab's award_emoji requires a named
	// shortcode (e.g. "thumbsup"), and we don't have one for arbitrary picks.
	// See EmojiUnicodeToShortcode for the supported set.
	if h.GitlabEnabled && h.GitlabResolver != nil && actorType != "agent" {
		if shortcode, ok := gitlabsync.EmojiUnicodeToShortcode(req.Emoji); ok {
			_, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), issue.WorkspaceID)
			if wsErr == nil {
				if !issue.GitlabIid.Valid || !issue.GitlabProjectID.Valid {
					slog.Error("gitlab connected workspace but issue has no gitlab refs",
						"issue_id", issueID, "workspace_id", workspaceID)
					writeError(w, http.StatusBadGateway, "issue not linked to gitlab")
					return
				}
				h.addIssueReactionWriteThrough(w, r, issue, req.Emoji, shortcode, actorType, actorID, workspaceID, issueID)
				return
			}
			// wsErr != nil → fall through to legacy path (non-connected workspace).
		}
		// Unsupported emoji (unicode not in the translation map) → fall through.
	}

	reaction, err := h.Queries.AddIssueReaction(r.Context(), db.AddIssueReactionParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		ActorType:   pgtype.Text{String: actorType, Valid: true},
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

// addIssueReactionWriteThrough implements the Phase 3d write-through branch of
// POST /api/issues/{id}/reactions: POST the award_emoji to GitLab, then upsert
// the cache row from the returned representation keyed by gitlab_award_id.
//
// On GitLab error returns a non-2xx status and aborts — we must NOT fall
// through to the legacy path, which would produce orphaned cache rows on a
// connected workspace.
func (h *Handler) addIssueReactionWriteThrough(
	w http.ResponseWriter,
	r *http.Request,
	issue db.Issue,
	unicodeEmoji, gitlabShortcode string,
	actorType, actorID, workspaceID, issueID string,
) {
	ctx := r.Context()

	token, _, err := h.GitlabResolver.ResolveTokenForWrite(ctx, workspaceID, actorType, actorID)
	if err != nil {
		slog.Error("resolve gitlab token", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
		return
	}

	award, err := h.Gitlab.CreateIssueAwardEmoji(ctx,
		token,
		issue.GitlabProjectID.Int64,
		int(issue.GitlabIid.Int32),
		gitlabShortcode,
	)
	if err != nil {
		slog.Error("gitlab create issue award_emoji", "error", err, "issue_id", issueID)
		writeError(w, http.StatusBadGateway, "gitlab create award_emoji failed")
		return
	}

	var glActor pgtype.Int8
	if award.User.ID != 0 {
		glActor = pgtype.Int8{Int64: award.User.ID, Valid: true}
	}
	externalUpdatedAt := parseGitlabTS(award.UpdatedAt)

	// Cache stores unicode (Multica convention) so the frontend renders
	// the emoji natively. award.Name would be the GitLab shortcode
	// ("thumbsup") — write the user's original unicode instead.
	row, upErr := h.Queries.UpsertIssueReactionFromGitlab(ctx, db.UpsertIssueReactionFromGitlabParams{
		WorkspaceID:       issue.WorkspaceID,
		IssueID:           issue.ID,
		ActorType:         pgtype.Text{String: actorType, Valid: true},
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
			loaded, loadErr := h.Queries.GetIssueReactionByGitlabAwardID(ctx,
				pgtype.Int8{Int64: award.ID, Valid: true})
			if loadErr != nil {
				slog.Error("load issue reaction after clobber-guard short-circuit",
					"error", loadErr, "gitlab_award_id", award.ID)
				writeError(w, http.StatusInternalServerError, "failed to add reaction")
				return
			}
			row = loaded
		} else {
			slog.Error("upsert gitlab issue reaction cache row", "error", upErr)
			writeError(w, http.StatusInternalServerError, "cache upsert failed")
			return
		}
	}

	resp := issueReactionToResponse(row)
	slog.Info("issue reaction added (gitlab write-through)",
		append(logger.RequestAttrs(r), "reaction_id", uuidToString(row.ID),
			"issue_id", issueID, "gitlab_award_id", award.ID)...)
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

	// Phase 3d write-through: on a GitLab-connected workspace, human-authored
	// reaction removals look up the cache row's gitlab_award_id, DELETE the
	// award on GitLab, then drop the local row. Agent reactions have no GitLab
	// representation — they fall through to the legacy path.
	if h.GitlabEnabled && h.GitlabResolver != nil && actorType != "agent" {
		_, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), issue.WorkspaceID)
		if wsErr == nil {
			existing, exErr := h.Queries.GetIssueReactionByKey(r.Context(), db.GetIssueReactionByKeyParams{
				IssueID:   issue.ID,
				ActorType: pgtype.Text{String: actorType, Valid: true},
				ActorID:   parseUUID(actorID),
				Emoji:     req.Emoji,
			})
			if exErr != nil {
				if errors.Is(exErr, pgx.ErrNoRows) {
					// No local row — idempotent success. No GitLab call.
					w.WriteHeader(http.StatusNoContent)
					return
				}
				slog.Error("lookup issue reaction for delete", "error", exErr, "issue_id", issueID)
				writeError(w, http.StatusInternalServerError, "failed to remove reaction")
				return
			}

			if !issue.GitlabIid.Valid || !issue.GitlabProjectID.Valid {
				slog.Error("gitlab connected workspace but issue has no gitlab refs",
					"issue_id", issueID, "workspace_id", workspaceID)
				writeError(w, http.StatusBadGateway, "issue not linked to gitlab")
				return
			}

			if existing.GitlabAwardID.Valid {
				token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
				if tokErr != nil {
					slog.Error("resolve gitlab token", "error", tokErr, "workspace_id", workspaceID)
					writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
					return
				}

				if err := h.Gitlab.DeleteIssueAwardEmoji(r.Context(), token,
					issue.GitlabProjectID.Int64, int(issue.GitlabIid.Int32), existing.GitlabAwardID.Int64); err != nil {
					slog.Error("gitlab delete issue award_emoji", "error", err, "issue_id", issueID)
					writeError(w, http.StatusBadGateway, "gitlab delete award_emoji failed")
					return
				}
			}
			// No gitlab_award_id → Multica-native row (pre-connection). Just drop locally.

			if err := h.Queries.DeleteIssueReactionByID(r.Context(), existing.ID); err != nil {
				slog.Error("delete issue reaction cache row", "error", err, "reaction_id", uuidToString(existing.ID))
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
			return
		}
		// wsErr != nil → fall through to legacy path (non-connected workspace).
	}

	if err := h.Queries.RemoveIssueReaction(r.Context(), db.RemoveIssueReactionParams{
		IssueID:   issue.ID,
		ActorType: pgtype.Text{String: actorType, Valid: true},
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
