package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/issueposition"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	installIssueRefToken = "{{install_issue_ref}}"
	agentGuideRefToken   = "{{agent_guide_ref}}"

	installSeedOriginType = "onboarding_no_runtime_install"
	guideSeedOriginType   = "onboarding_no_runtime_guide"

	completeNoRuntimeBodyLimit = 4 * 1024
)

type completeOnboardingNoRuntimeRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Locale      string `json:"locale"`
}

type completeOnboardingNoRuntimeResponse struct {
	User            UserResponse  `json:"user"`
	WorkspaceID     string        `json:"workspace_id"`
	InstallIssue    IssueResponse `json:"install_issue"`
	AgentGuideIssue IssueResponse `json:"agent_guide_issue"`
}

// CompleteOnboardingNoRuntime atomically completes the skip-runtime path and
// creates its two platform-authored starter issues. The request chooses only
// an allowlisted locale; every persisted title, description, and comment is
// loaded from server-owned content so callers cannot publish arbitrary text as
// Multica.
//
// The two issues carry durable origin markers keyed by the onboarding user.
// A user-row lock serializes concurrent retries, and a completed retry returns
// those exact rows rather than matching an unrelated issue by title. Once a
// user has completed onboarding, a missing bundle is never recreated through
// this privileged endpoint.
func (h *Handler) CompleteOnboardingNoRuntime(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, completeNoRuntimeBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req completeOnboardingNoRuntimeRequest
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)
	content, ok := onboardingSeedContentForLocale(req.Locale)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported locale")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	userUUID := parseUUID(userID)

	before, err := qtx.GetUserForUpdate(r.Context(), userUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}
	member, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}
	if member.Role != "owner" {
		writeError(w, http.StatusForbidden, "workspace owner access required")
		return
	}
	workspace, err := qtx.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}
	prefix := workspace.IssuePrefix
	if prefix == "" {
		prefix = generateIssuePrefix(workspace.Name)
	}

	installIssue, installFound, err := findOnboardingSeedIssue(
		r.Context(), qtx, wsUUID, userUUID, installSeedOriginType,
	)
	if err != nil {
		slog.Warn("complete onboarding no-runtime: load install issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}
	guideIssue, guideFound, err := findOnboardingSeedIssue(
		r.Context(), qtx, wsUUID, userUUID, guideSeedOriginType,
	)
	if err != nil {
		slog.Warn("complete onboarding no-runtime: load guide issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}
	if installFound != guideFound {
		writeError(w, http.StatusConflict, "onboarding content is incomplete")
		return
	}

	created := false
	var comment db.Comment
	if !installFound {
		if before.OnboardedAt.Valid {
			writeError(w, http.StatusConflict, "onboarding is already complete")
			return
		}
		systemID := pgtype.UUID{Valid: true}
		installIssue, err = h.createSystemOnboardingIssue(r.Context(), tx, qtx, createSystemOnboardingIssueParams{
			WorkspaceID: wsUUID,
			Title:       content.InstallTitle,
			Description: content.InstallDescription,
			Status:      "in_progress",
			Priority:    "high",
			AssigneeID:  userUUID,
			CreatorID:   systemID,
			OriginType:  installSeedOriginType,
			OriginID:    userUUID,
		})
		if err != nil {
			slog.Warn("complete onboarding no-runtime: create install issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
			return
		}

		installRef := issueMentionChip(prefix, installIssue)
		guideIssue, err = h.createSystemOnboardingIssue(r.Context(), tx, qtx, createSystemOnboardingIssueParams{
			WorkspaceID: wsUUID,
			Title:       content.AgentGuideTitle,
			Description: strings.ReplaceAll(content.AgentGuideDescription, installIssueRefToken, installRef),
			Status:      "todo",
			Priority:    "medium",
			AssigneeID:  userUUID,
			CreatorID:   systemID,
			OriginType:  guideSeedOriginType,
			OriginID:    userUUID,
		})
		if err != nil {
			slog.Warn("complete onboarding no-runtime: create guide issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
			return
		}

		commentContent := strings.ReplaceAll(content.FollowupComment, agentGuideRefToken, issueMentionChip(prefix, guideIssue))
		commentContent = strings.ReplaceAll(commentContent, installIssueRefToken, installRef)
		comment, err = qtx.CreateComment(r.Context(), db.CreateCommentParams{
			IssueID:     installIssue.ID,
			WorkspaceID: wsUUID,
			AuthorType:  "system",
			AuthorID:    systemID,
			Content:     commentContent,
			Type:        "comment",
			ParentID:    pgtype.UUID{},
		})
		if err != nil {
			slog.Warn("complete onboarding no-runtime: create comment failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
			return
		}
		created = true
	}

	firstCompletion := !before.OnboardedAt.Valid
	updatedUser := before
	if firstCompletion {
		updatedUser, err = qtx.MarkUserOnboarded(r.Context(), userUUID)
		if err != nil {
			slog.Warn("complete onboarding no-runtime: mark user onboarded failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}

	platform, _, _ := middleware.ClientMetadataFromContext(r.Context())
	if created {
		for _, issue := range []db.Issue{installIssue, guideIssue} {
			response := issueToResponse(issue, prefix)
			h.publish(protocol.EventIssueCreated, req.WorkspaceID, "system", "", map[string]any{"issue": response})
			obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueCreated(
				userID, req.WorkspaceID, uuidToString(issue.ID),
				"", "", "", analytics.SourceOnboarding, platform,
			))
		}
		h.publish(protocol.EventCommentCreated, req.WorkspaceID, "system", "", map[string]any{
			"comment":             commentToResponse(comment, nil, nil),
			"issue_title":         installIssue.Title,
			"issue_assignee_type": textToPtr(installIssue.AssigneeType),
			"issue_assignee_id":   uuidToPtr(installIssue.AssigneeID),
			"issue_status":        installIssue.Status,
		})
	}
	if firstCompletion {
		onboardedAt := ""
		if updatedUser.OnboardedAt.Valid {
			onboardedAt = updatedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID,
			req.WorkspaceID,
			analytics.OnboardingPathRuntimeSkipped,
			onboardedAt,
			updatedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, completeOnboardingNoRuntimeResponse{
		User:            userToResponse(updatedUser),
		WorkspaceID:     req.WorkspaceID,
		InstallIssue:    issueToResponse(installIssue, prefix),
		AgentGuideIssue: issueToResponse(guideIssue, prefix),
	})
}

func findOnboardingSeedIssue(
	ctx context.Context,
	queries *db.Queries,
	workspaceID pgtype.UUID,
	userID pgtype.UUID,
	originType string,
) (db.Issue, bool, error) {
	issue, err := queries.GetIssueByOrigin(ctx, db.GetIssueByOriginParams{
		WorkspaceID: workspaceID,
		OriginType:  pgtype.Text{String: originType, Valid: true},
		OriginID:    userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Issue{}, false, nil
	}
	if err != nil {
		return db.Issue{}, false, err
	}
	if issue.CreatorType != "system" {
		return db.Issue{}, false, errors.New("onboarding seed origin has non-system creator")
	}
	return issue, true, nil
}

type createSystemOnboardingIssueParams struct {
	WorkspaceID pgtype.UUID
	Title       string
	Description string
	Status      string
	Priority    string
	AssigneeID  pgtype.UUID
	CreatorID   pgtype.UUID
	OriginType  string
	OriginID    pgtype.UUID
}

func (h *Handler) createSystemOnboardingIssue(
	ctx context.Context,
	tx pgx.Tx,
	queries *db.Queries,
	params createSystemOnboardingIssueParams,
) (db.Issue, error) {
	position, err := issueposition.NextTopPosition(ctx, tx, params.WorkspaceID, params.Status)
	if err != nil {
		return db.Issue{}, err
	}
	issueNumber, err := queries.IncrementIssueCounter(ctx, params.WorkspaceID)
	if err != nil {
		return db.Issue{}, err
	}
	return queries.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		WorkspaceID:   params.WorkspaceID,
		Title:         params.Title,
		Description:   pgtype.Text{String: params.Description, Valid: true},
		Status:        params.Status,
		Priority:      params.Priority,
		AssigneeType:  pgtype.Text{String: "member", Valid: true},
		AssigneeID:    params.AssigneeID,
		CreatorType:   "system",
		CreatorID:     params.CreatorID,
		ParentIssueID: pgtype.UUID{},
		Position:      position,
		Number:        issueNumber,
		ProjectID:     pgtype.UUID{},
		OriginType:    pgtype.Text{String: params.OriginType, Valid: true},
		OriginID:      params.OriginID,
	})
}

func issueMentionChip(prefix string, issue db.Issue) string {
	identifier := prefix + "-" + strconv.Itoa(int(issue.Number))
	return "[" + identifier + "](mention://issue/" + uuidToString(issue.ID) + ")"
}
