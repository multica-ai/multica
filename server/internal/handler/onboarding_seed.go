// onboarding_seed.go — v3 no-runtime onboarding content, seeded server-side.
//
// The v3 skip-path welcome hook (packages/views/workspace/
// welcome-after-onboarding.tsx) used to create its two starter issues and
// the follow-up comment through the generic CreateIssue / CreateComment
// endpoints, which stamped the just-onboarded member as creator/author.
// Product-wise those rows are platform content — "Multica set up your next
// steps" — not something the user wrote, so attributing them to the user
// reads wrong on the timeline ("<user> created this issue") (MUL-5118).
//
// This endpoint creates the whole bundle in one transaction with system
// attribution (issue.creator_type='system', comment.author_type='system',
// zero-UUID ids — same convention as the child-done parent notification,
// MUL-2538). The localized copy stays client-owned in
// packages/views/onboarding/templates/ per the v3 split; the client cannot
// choose attribution, statuses, priorities, or assignee — only the text.
//
// Trust note: unlike child-done system comments, the CONTENT here is
// client-supplied, so a workspace member could hand-craft a call and get
// arbitrary text attributed to "Multica" inside their own workspace. That
// is bounded by workspace membership, per-title dedupe, and the size caps
// below, and display attribution carries no permissions. Do not widen this
// endpoint into a general "post as system" surface.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/issueguard"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// Placeholder tokens the client embeds where a mention chip to a
	// bundle sibling belongs. The server substitutes them after the
	// referenced issue exists, because the chip needs the issue's
	// identifier + uuid which are only known mid-transaction.
	installIssueRefToken = "{{install_issue_ref}}"
	agentGuideRefToken   = "{{agent_guide_ref}}"

	// Size caps: generous for real onboarding copy (the agent-guide body
	// embeds the full Helper instructions, ~4k runes in CJK locales), tight
	// enough that the endpoint is useless as bulk storage.
	seedTitleMaxRunes       = 300
	seedDescriptionMaxRunes = 20000
	seedCommentMaxRunes     = 2000
	seedBodyLimit           = 64 * 1024
)

type seedIssueContent struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type seedCommentContent struct {
	Content string `json:"content"`
}

type seedOnboardingNoRuntimeRequest struct {
	WorkspaceID     string             `json:"workspace_id"`
	InstallIssue    seedIssueContent   `json:"install_issue"`
	AgentGuideIssue seedIssueContent   `json:"agent_guide_issue"`
	FollowupComment seedCommentContent `json:"followup_comment"`
}

type seedOnboardingNoRuntimeResponse struct {
	WorkspaceID     string        `json:"workspace_id"`
	InstallIssue    IssueResponse `json:"install_issue"`
	AgentGuideIssue IssueResponse `json:"agent_guide_issue"`
}

func validateSeedIssueContent(w http.ResponseWriter, field string, c seedIssueContent) bool {
	if strings.TrimSpace(c.Title) == "" {
		writeError(w, http.StatusBadRequest, field+".title is required")
		return false
	}
	if utf8.RuneCountInString(c.Title) > seedTitleMaxRunes {
		writeError(w, http.StatusBadRequest, field+".title is too long")
		return false
	}
	if strings.TrimSpace(c.Description) == "" {
		writeError(w, http.StatusBadRequest, field+".description is required")
		return false
	}
	if utf8.RuneCountInString(c.Description) > seedDescriptionMaxRunes {
		writeError(w, http.StatusBadRequest, field+".description is too long")
		return false
	}
	return true
}

// SeedOnboardingNoRuntime creates the skip-path onboarding bundle — the
// install-runtime issue (in_progress), the create-agent-guide issue (todo),
// and one follow-up comment on the install issue linking to the guide — all
// attributed to the platform and assigned to the calling member.
//
// Idempotent per title: re-entries (StrictMode double effects, client
// retries) reuse the active issue with the same normalized title instead of
// creating a duplicate, mirroring the deprecated pre-v3 bootstrap shim. The
// comment is only posted when at least one issue in the bundle is new, so a
// full re-entry does not stack "Your next step" comments.
func (h *Handler) SeedOnboardingNoRuntime(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, seedBodyLimit)
	var req seedOnboardingNoRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if !validateSeedIssueContent(w, "install_issue", req.InstallIssue) {
		return
	}
	if !validateSeedIssueContent(w, "agent_guide_issue", req.AgentGuideIssue) {
		return
	}
	if strings.TrimSpace(req.FollowupComment.Content) == "" {
		writeError(w, http.StatusBadRequest, "followup_comment.content is required")
		return
	}
	if utf8.RuneCountInString(req.FollowupComment.Content) > seedCommentMaxRunes {
		writeError(w, http.StatusBadRequest, "followup_comment.content is too long")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), wsUUID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to seed onboarding content")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	// System actor: zero UUID with Valid=true — the column is NOT NULL.
	// Frontends branch on the type ('system' → "Multica"), never on the id.
	systemID := pgtype.UUID{Valid: true}

	installIssue, installCreated, err := h.seedSystemIssue(r, qtx, wsUUID, seedIssueParams{
		Title:       req.InstallIssue.Title,
		Description: req.InstallIssue.Description,
		Status:      "in_progress",
		Priority:    "high",
		AssigneeID:  parseUUID(userID),
		CreatorID:   systemID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to seed onboarding content")
		return
	}

	installRef := issueMentionChip(prefix, installIssue)
	guideDescription := strings.ReplaceAll(req.AgentGuideIssue.Description, installIssueRefToken, installRef)

	guideIssue, guideCreated, err := h.seedSystemIssue(r, qtx, wsUUID, seedIssueParams{
		Title:       req.AgentGuideIssue.Title,
		Description: guideDescription,
		Status:      "todo",
		Priority:    "medium",
		AssigneeID:  parseUUID(userID),
		CreatorID:   systemID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to seed onboarding content")
		return
	}

	// Only post the follow-up when something in the bundle is new; a full
	// re-entry means the comment already went out on the first pass.
	var comment db.Comment
	commentCreated := false
	if installCreated || guideCreated {
		content := strings.ReplaceAll(req.FollowupComment.Content, agentGuideRefToken, issueMentionChip(prefix, guideIssue))
		content = strings.ReplaceAll(content, installIssueRefToken, installRef)
		comment, err = qtx.CreateComment(r.Context(), db.CreateCommentParams{
			IssueID:     installIssue.ID,
			WorkspaceID: wsUUID,
			AuthorType:  "system",
			AuthorID:    systemID,
			Content:     content,
			Type:        "comment",
			ParentID:    pgtype.UUID{Valid: false},
		})
		if err != nil {
			slog.Warn("seed onboarding no-runtime: create comment failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to seed onboarding content")
			return
		}
		commentCreated = true
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to seed onboarding content")
		return
	}

	platform, _, _ := middleware.ClientMetadataFromContext(r.Context())
	for _, seeded := range []struct {
		issue   db.Issue
		created bool
	}{{installIssue, installCreated}, {guideIssue, guideCreated}} {
		if !seeded.created {
			continue
		}
		resp := issueToResponse(seeded.issue, prefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "system", "", map[string]any{"issue": resp})
		// Analytics keeps the human in the funnel — attribution is a
		// display concern, the acting user is still the one onboarding.
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueCreated(
			userID, req.WorkspaceID, uuidToString(seeded.issue.ID),
			"", "", "", analytics.SourceOnboarding, platform,
		))
	}
	if commentCreated {
		h.publish(protocol.EventCommentCreated, req.WorkspaceID, "system", "", map[string]any{
			"comment":             commentToResponse(comment, nil, nil),
			"issue_title":         installIssue.Title,
			"issue_assignee_type": textToPtr(installIssue.AssigneeType),
			"issue_assignee_id":   uuidToPtr(installIssue.AssigneeID),
			"issue_status":        installIssue.Status,
		})
	}

	writeJSON(w, http.StatusOK, seedOnboardingNoRuntimeResponse{
		WorkspaceID:     req.WorkspaceID,
		InstallIssue:    issueToResponse(installIssue, prefix),
		AgentGuideIssue: issueToResponse(guideIssue, prefix),
	})
}

type seedIssueParams struct {
	Title       string
	Description string
	Status      string
	Priority    string
	AssigneeID  pgtype.UUID
	CreatorID   pgtype.UUID
}

// seedSystemIssue creates one system-attributed, member-assigned issue, or
// returns the existing active issue with the same normalized title. The
// returned bool reports whether a row was created.
func (h *Handler) seedSystemIssue(r *http.Request, qtx *db.Queries, wsUUID pgtype.UUID, p seedIssueParams) (db.Issue, bool, error) {
	var emptyUUID pgtype.UUID
	existing, found, err := issueguard.LockAndFindActiveDuplicate(
		r.Context(), qtx, wsUUID, emptyUUID, emptyUUID, p.Title, false,
	)
	if err != nil {
		slog.Warn("seed onboarding no-runtime: duplicate check failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(wsUUID))...)
		return db.Issue{}, false, err
	}
	if found {
		return existing, false, nil
	}
	issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
	if err != nil {
		return db.Issue{}, false, err
	}
	issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID:   wsUUID,
		Title:         p.Title,
		Description:   pgtype.Text{String: p.Description, Valid: true},
		Status:        p.Status,
		Priority:      p.Priority,
		AssigneeType:  pgtype.Text{String: "member", Valid: true},
		AssigneeID:    p.AssigneeID,
		CreatorType:   "system",
		CreatorID:     p.CreatorID,
		ParentIssueID: emptyUUID,
		Position:      0,
		Number:        issueNumber,
		ProjectID:     emptyUUID,
	})
	if err != nil {
		slog.Warn("seed onboarding no-runtime: create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(wsUUID))...)
		return db.Issue{}, false, err
	}
	return issue, true, nil
}

// issueMentionChip renders the markdown mention-chip link for an issue —
// the same protocol the comment editor produces, rendered as a styled
// IssueChip pill by the frontend.
func issueMentionChip(prefix string, issue db.Issue) string {
	identifier := prefix + "-" + strconv.Itoa(int(issue.Number))
	return "[" + identifier + "](mention://issue/" + uuidToString(issue.ID) + ")"
}
