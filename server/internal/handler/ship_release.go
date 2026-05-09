// Phase 7a — Release HTTP handlers.
//
// All endpoints sit under the workspace-member middleware (the same
// gate as the rest of Ship Hub Phase 4+). Auth + workspace scoping
// flow through the Phase-1 helpers (requireShipHubEnabled,
// loadShipProject); the new helpers in this file are loadRelease
// (resolves the {id} URL param to a row already scoped to the
// caller's workspace) and the channelOps / issueOps adapters that
// satisfy the service-layer interfaces.

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/channel"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// releaseChannelOps adapts h.ChannelService to the ship.ChannelOps
// interface so the service layer can stay independent of the handler
// package (avoids an import cycle). One adapter per request because
// the underlying service is shared and stateless.
type releaseChannelOps struct{ h *Handler }

func (a *releaseChannelOps) CreateReleaseChannel(
	ctx context.Context,
	workspaceID pgtype.UUID,
	name, displayName, description string,
	creator ship.ChannelMember,
	members []ship.ChannelMember,
) (db.Channel, error) {
	creatorActor := channel.Actor{Type: creator.Type, ID: creator.ID}
	ch, err := a.h.ChannelService.Create(ctx, channel.CreateChannelParams{
		WorkspaceID: workspaceID,
		Name:        name,
		DisplayName: displayName,
		Description: description,
		Kind:        channel.KindChannel,
		// Private — release coordination shouldn't broadcast to the
		// whole workspace by default. Members are seeded from the
		// PR set + approver + orchestrator.
		Visibility: channel.VisibilityPrivate,
		CreatedBy:  creatorActor,
	})
	if err != nil {
		// Mirror the Phase 4 conversation-channel reuse-on-conflict
		// pattern: a re-run of the create flow with the same title +
		// date should attach the existing channel rather than fail.
		if errors.Is(err, channel.ErrConflict) {
			existing, getErr := a.h.Queries.GetChannelByName(ctx, db.GetChannelByNameParams{
				WorkspaceID: workspaceID,
				Kind:        channel.KindChannel,
				Name:        name,
			})
			if getErr == nil {
				ch = existing
			} else {
				return db.Channel{}, fmt.Errorf("conflict + lookup failed: %w", getErr)
			}
		} else {
			return db.Channel{}, err
		}
	}
	// Seed membership. ON CONFLICT DO NOTHING in the channel service
	// makes this idempotent against the conflict-reuse path.
	for _, m := range members {
		if !m.ID.Valid {
			continue
		}
		_, _ = a.h.ChannelService.AddMember(ctx, ch.ID, channel.AddMemberParams{
			Member: channel.Actor{Type: m.Type, ID: m.ID},
			Role:   channel.RoleMember,
		})
	}
	return ch, nil
}

func (a *releaseChannelOps) ArchiveReleaseChannel(ctx context.Context, channelID pgtype.UUID) error {
	return a.h.ChannelService.Archive(ctx, channelID)
}

// releaseIssueOps adapts the issue queries + counter to the
// ship.IssueOps interface. We mint a fresh issue number through
// IncrementIssueCounter so the new issue gets a real workspace
// identifier (MUL-NN) and shows up in normal issue lists.
type releaseIssueOps struct{ h *Handler }

func (a *releaseIssueOps) CreateReleaseIssue(
	ctx context.Context,
	workspaceID, projectID pgtype.UUID,
	title, description string,
	creator pgtype.UUID,
) (db.Issue, error) {
	tx, err := a.h.TxStarter.Begin(ctx)
	if err != nil {
		return db.Issue{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := a.h.Queries.WithTx(tx)
	number, err := qtx.IncrementIssueCounter(ctx, workspaceID)
	if err != nil {
		return db.Issue{}, fmt.Errorf("increment issue counter: %w", err)
	}
	creatorType := "member"
	if !creator.Valid {
		// CreatorID is NOT NULL — fall back to a synthesized zero
		// UUID. In practice every release-create is user-driven so
		// this branch should be unreachable; we leave the defensive
		// fallback so the FK doesn't nil-pointer in test contexts.
		creator = pgtype.UUID{}
	}
	issue, err := qtx.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: workspaceID,
		Title:       title,
		Description: pgtype.Text{String: description, Valid: true},
		Status:      "in_progress",
		Priority:    "medium",
		CreatorType: creatorType,
		CreatorID:   creator,
		Position:    0,
		Number:      number,
		ProjectID:   projectID,
	})
	if err != nil {
		return db.Issue{}, fmt.Errorf("create release issue: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Issue{}, fmt.Errorf("commit issue tx: %w", err)
	}
	return issue, nil
}

func (a *releaseIssueOps) CloseReleaseIssue(ctx context.Context, issueID pgtype.UUID, status string) error {
	if status == "" {
		status = "done"
	}
	_, err := a.h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issueID,
		Status: status,
	})
	return err
}

// ----- response shapes ------------------------------------------------------

type releaseResponse struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspace_id"`
	ProjectID          string  `json:"project_id"`
	Title              string  `json:"title"`
	Description        *string `json:"description"`
	Stage              string  `json:"stage"`
	RiskLevel          string  `json:"risk_level"`
	ChannelID          *string `json:"channel_id"`
	IssueID            *string `json:"issue_id"`
	ApproverID         *string `json:"approver_id"`
	SecondApproverID   *string `json:"second_approver_id"`
	StagingDeployID    *string `json:"staging_deploy_id"`
	ProductionDeployID *string `json:"production_deploy_id"`
	CreatedBy          *string `json:"created_by"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	MergedAt           *string `json:"merged_at"`
	StagedAt           *string `json:"staged_at"`
	PromotedAt         *string `json:"promoted_at"`
	DoneAt             *string `json:"done_at"`
	RollbackReason     *string `json:"rollback_reason"`
	PRCount            int     `json:"pr_count"`
	// Phase 7b — merge train state. merge_paused signals "the
	// orchestrator stopped on a failure; the user can resume". The
	// UI gates the Resume / Skip / Abort affordances on this flag.
	MergePaused bool   `json:"merge_paused"`
	MergeMethod string `json:"merge_method"`
	// Phase 7c — staging-stage signals. All optional and additive
	// per CLAUDE.md API drift contract.
	SmokeRunID       *string `json:"smoke_run_id"`
	SmokeRunURL      *string `json:"smoke_run_url"`
	SmokeStatus      *string `json:"smoke_status"`
	SmokeCompletedAt *string `json:"smoke_completed_at"`
	QAVerifiedAt     *string `json:"qa_verified_at"`
	QAVerifiedBy     *string `json:"qa_verified_by"`
	MergedMainSHA    *string `json:"merged_main_sha"`
}

func releaseToResponse(r db.ShipRelease, prCount int) releaseResponse {
	return releaseResponse{
		ID:                 uuidToString(r.ID),
		WorkspaceID:        uuidToString(r.WorkspaceID),
		ProjectID:          uuidToString(r.ProjectID),
		Title:              r.Title,
		Description:        textToPtr(r.Description),
		Stage:              string(r.Stage),
		RiskLevel:          string(r.RiskLevel),
		ChannelID:          uuidToPtr(r.ChannelID),
		IssueID:            uuidToPtr(r.IssueID),
		ApproverID:         uuidToPtr(r.ApproverID),
		SecondApproverID:   uuidToPtr(r.SecondApproverID),
		StagingDeployID:    uuidToPtr(r.StagingDeployID),
		ProductionDeployID: uuidToPtr(r.ProductionDeployID),
		CreatedBy:          uuidToPtr(r.CreatedBy),
		CreatedAt:          timestampToString(r.CreatedAt),
		UpdatedAt:          timestampToString(r.UpdatedAt),
		MergedAt:           timestampToPtr(r.MergedAt),
		StagedAt:           timestampToPtr(r.StagedAt),
		PromotedAt:         timestampToPtr(r.PromotedAt),
		DoneAt:             timestampToPtr(r.DoneAt),
		RollbackReason:     textToPtr(r.RollbackReason),
		PRCount:          prCount,
		MergePaused:      r.MergePaused,
		MergeMethod:      r.MergeMethod,
		SmokeRunID:       textToPtr(r.SmokeRunID),
		SmokeRunURL:      textToPtr(r.SmokeRunUrl),
		SmokeStatus:      textToPtr(r.SmokeStatus),
		SmokeCompletedAt: timestampToPtr(r.SmokeCompletedAt),
		QAVerifiedAt:     timestampToPtr(r.QaVerifiedAt),
		QAVerifiedBy:     uuidToPtr(r.QaVerifiedBy),
		MergedMainSHA:    textToPtr(r.MergedMainSha),
	}
}

type releasePullRequestResponse struct {
	pullRequestResponse
	Position    int32   `json:"position"`
	MergedSHA   *string `json:"merged_sha"`
	MergedAtRel *string `json:"merged_at_release"`
	MergeError  *string `json:"merge_error"`
	AddedAt     string  `json:"added_at"`
	IsActive    bool    `json:"is_active"`
	// Phase 7b — per-PR merge state in the train. Possible values:
	// queued | merging | merged | failed | skipped. Drives the UI
	// pill rendering on the release detail page.
	MergeState string `json:"merge_state"`
}

func releasePRRowToResponse(row db.ListReleasePullRequestsRow) releasePullRequestResponse {
	pr := db.PullRequest{
		ID:                     row.ID,
		WorkspaceID:            row.WorkspaceID,
		ProjectID:              row.ProjectID,
		RepoUrl:                row.RepoUrl,
		PrNumber:               row.PrNumber,
		Title:                  row.Title,
		State:                  row.State,
		IsDraft:                row.IsDraft,
		AuthorLogin:            row.AuthorLogin,
		AuthorAvatarUrl:        row.AuthorAvatarUrl,
		BaseRef:                row.BaseRef,
		HeadRef:                row.HeadRef,
		HeadSha:                row.HeadSha,
		HtmlUrl:                row.HtmlUrl,
		Body:                   row.Body,
		CiStatus:               row.CiStatus,
		ReviewDecision:         row.ReviewDecision,
		Mergeable:              row.Mergeable,
		Additions:              row.Additions,
		Deletions:              row.Deletions,
		ChangedFiles:           row.ChangedFiles,
		Labels:                 row.Labels,
		PrCreatedAt:            row.PrCreatedAt,
		PrUpdatedAt:            row.PrUpdatedAt,
		PrMergedAt:             row.PrMergedAt,
		PrClosedAt:             row.PrClosedAt,
		FetchedAt:              row.FetchedAt,
		OriginatingIssueID:     row.OriginatingIssueID,
		OriginatingAgentTaskID: row.OriginatingAgentTaskID,
		AutoCloseIssueOnMerge:  row.AutoCloseIssueOnMerge,
		ConversationChannelID:  row.ConversationChannelID,
		StackParentPrID:        row.StackParentPrID,
		Source:                 row.Source,
		RiskLevel:              row.RiskLevel,
		RiskReasons:            row.RiskReasons,
		RiskClassifiedAt:       row.RiskClassifiedAt,
	}
	out := releasePullRequestResponse{
		pullRequestResponse: pullRequestToResponse(pr),
		Position:            row.MembershipPosition,
		MergedSHA:           textToPtr(row.MembershipMergedSha),
		MergedAtRel:         timestampToPtr(row.MembershipMergedAt),
		MergeError:          textToPtr(row.MembershipMergeError),
		AddedAt:             timestampToString(row.MembershipAddedAt),
		IsActive:            row.MembershipIsActive,
		MergeState:          string(row.MembershipMergeState),
	}
	return out
}

type releaseEventResponse struct {
	ID          string `json:"id"`
	ReleaseID   string `json:"release_id"`
	EventType   string `json:"event_type"`
	ActorUserID *string `json:"actor_user_id"`
	Payload     any    `json:"payload"`
	CreatedAt   string `json:"created_at"`
}

func releaseEventToResponse(e db.ShipReleaseEvent) releaseEventResponse {
	resp := releaseEventResponse{
		ID:          uuidToString(e.ID),
		ReleaseID:   uuidToString(e.ReleaseID),
		EventType:   e.EventType,
		ActorUserID: uuidToPtr(e.ActorUserID),
		CreatedAt:   timestampToString(e.CreatedAt),
	}
	if len(e.Payload) > 0 {
		var v any
		if err := json.Unmarshal(e.Payload, &v); err == nil {
			resp.Payload = v
		}
	}
	return resp
}

// ----- handlers -------------------------------------------------------------

// loadRelease resolves the {id} URL param to a release row scoped to
// the caller's workspace. Returns (release, workspaceID, ok).
func (h *Handler) loadRelease(w http.ResponseWriter, r *http.Request) (db.ShipRelease, pgtype.UUID, bool) {
	wsID, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return db.ShipRelease{}, pgtype.UUID{}, false
	}
	releaseUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "release id")
	if !ok {
		return db.ShipRelease{}, pgtype.UUID{}, false
	}
	rel, err := h.Queries.GetReleaseInWorkspace(r.Context(), db.GetReleaseInWorkspaceParams{
		ID:          releaseUUID,
		WorkspaceID: wsID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "release not found")
		return db.ShipRelease{}, pgtype.UUID{}, false
	}
	return rel, wsID, true
}

// CreateReleaseRequest is the body for POST /api/projects/{id}/releases.
type CreateReleaseRequest struct {
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	PullRequestIDs   []string `json:"pull_request_ids"`
	ApproverID       *string  `json:"approver_id"`
	SecondApproverID *string  `json:"second_approver_id"`
}

// CreateRelease handles POST /api/projects/{id}/releases.
func (h *Handler) CreateRelease(w http.ResponseWriter, r *http.Request) {
	project, wsID, _, ok := h.loadShipProject(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(req.PullRequestIDs) == 0 {
		writeError(w, http.StatusBadRequest, "pull_request_ids must not be empty")
		return
	}

	prUUIDs := make([]pgtype.UUID, 0, len(req.PullRequestIDs))
	for _, idStr := range req.PullRequestIDs {
		prUUID, ok := parseUUIDOrBadRequest(w, idStr, "pull_request_ids")
		if !ok {
			return
		}
		prUUIDs = append(prUUIDs, prUUID)
	}

	var approverID *pgtype.UUID
	if req.ApproverID != nil && *req.ApproverID != "" {
		uid, ok := parseUUIDOrBadRequest(w, *req.ApproverID, "approver_id")
		if !ok {
			return
		}
		approverID = &uid
	}
	var secondApproverID *pgtype.UUID
	if req.SecondApproverID != nil && *req.SecondApproverID != "" {
		uid, ok := parseUUIDOrBadRequest(w, *req.SecondApproverID, "second_approver_id")
		if !ok {
			return
		}
		secondApproverID = &uid
	}

	creatorUUID, _ := h.parseUserUUIDOrZero(userID)
	svc := &ship.Service{Q: h.Queries}
	result, err := svc.CreateRelease(r.Context(), ship.CreateReleaseParams{
		WorkspaceID:      wsID,
		ProjectID:        project.ID,
		Title:            req.Title,
		Description:      req.Description,
		PullRequestIDs:   prUUIDs,
		ApproverID:       approverID,
		SecondApproverID: secondApproverID,
		CreatedBy:        creatorUUID,
	}, &releaseChannelOps{h: h}, &releaseIssueOps{h: h})
	if err != nil {
		// Map the typed errors back to HTTP statuses. Anything we
		// don't recognize falls through to 500 — the service layer
		// keeps its sentinel set narrow on purpose.
		switch {
		case errors.Is(err, ship.ErrReleaseNoPullRequests):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ship.ErrReleasePullRequestNotFound):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ship.ErrReleasePullRequestIneligible):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ship.ErrReleasePullRequestInActive):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrReleasePullRequestProjectMismatch):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			slog.Warn("create release failed", "error", err, "workspace_id", uuidToString(wsID))
			writeError(w, http.StatusInternalServerError, "failed to create release")
		}
		return
	}

	resp := releaseToResponse(result.Release, len(result.PRs))
	body := map[string]any{
		"release":  resp,
		"warnings": result.Warnings,
	}
	if result.Channel != nil {
		body["channel"] = channelToResponse(*result.Channel)
	}
	if result.Issue != nil {
		prefix := h.getIssuePrefix(r.Context(), wsID)
		body["issue"] = issueToResponse(*result.Issue, prefix)
	}

	h.publish(protocol.EventReleaseCreated, uuidToString(wsID), "member", userID, map[string]any{
		"project_id": uuidToString(project.ID),
		"release_id": uuidToString(result.Release.ID),
		"stage":      string(result.Release.Stage),
	})
	writeJSON(w, http.StatusCreated, body)
}

// GetRelease handles GET /api/releases/{id}.
func (h *Handler) GetRelease(w http.ResponseWriter, r *http.Request) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}

	prRows, err := h.Queries.ListReleasePullRequests(r.Context(), rel.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list release PRs")
		return
	}
	prs := make([]releasePullRequestResponse, len(prRows))
	for i, row := range prRows {
		prs[i] = releasePRRowToResponse(row)
	}

	// Bounded event tail. 100 covers a full release lifecycle even
	// under heavy chatter.
	eventRows, err := h.Queries.ListReleaseEvents(r.Context(), db.ListReleaseEventsParams{
		ReleaseID: rel.ID,
		Limit:     100,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list release events")
		return
	}
	events := make([]releaseEventResponse, len(eventRows))
	for i, e := range eventRows {
		events[i] = releaseEventToResponse(e)
	}

	body := map[string]any{
		"release":       releaseToResponse(rel, len(prRows)),
		"pull_requests": prs,
		"events":        events,
	}
	if rel.ChannelID.Valid {
		ch, err := h.Queries.GetChannelInWorkspace(r.Context(), db.GetChannelInWorkspaceParams{
			ID: rel.ChannelID, WorkspaceID: wsID,
		})
		if err == nil {
			body["channel"] = channelToResponse(ch)
		}
	}
	if rel.IssueID.Valid {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID: rel.IssueID, WorkspaceID: wsID,
		})
		if err == nil {
			prefix := h.getIssuePrefix(r.Context(), wsID)
			body["issue"] = issueToResponse(issue, prefix)
		}
	}
	writeJSON(w, http.StatusOK, body)
}

// ListProjectReleases handles GET /api/projects/{id}/releases.
func (h *Handler) ListProjectReleases(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := h.loadShipProject(w, r)
	if !ok {
		return
	}
	includeTerminal := r.URL.Query().Get("status") == "all"
	rows, err := h.Queries.ListReleasesByProject(r.Context(), db.ListReleasesByProjectParams{
		ProjectID:       project.ID,
		IncludeTerminal: includeTerminal,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list releases")
		return
	}
	out := make([]releaseResponse, len(rows))
	for i, r := range rows {
		count, _ := h.Queries.CountActiveReleasePullRequests(context.Background(), r.ID)
		out[i] = releaseToResponse(r, int(count))
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out})
}

// ListWorkspaceActiveReleases handles GET /api/workspaces/{id}/releases/active.
func (h *Handler) ListWorkspaceActiveReleases(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	rows, err := h.Queries.ListActiveReleasesByWorkspace(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list active releases")
		return
	}
	out := make([]releaseResponse, len(rows))
	for i, rel := range rows {
		count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), rel.ID)
		out[i] = releaseToResponse(rel, int(count))
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out})
}

// UpdateReleaseRequest is the body for PATCH /api/releases/{id}.
type UpdateReleaseRequest struct {
	Title            *string `json:"title"`
	Description      *string `json:"description"`
	ApproverID       *string `json:"approver_id"`
	SecondApproverID *string `json:"second_approver_id"`
}

// UpdateRelease handles PATCH /api/releases/{id}.
func (h *Handler) UpdateRelease(w http.ResponseWriter, r *http.Request) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	editor, _ := h.parseUserUUIDOrZero(userID)

	// Read body as raw bytes so we can detect "field present and
	// null" vs. "field absent" for the approver-clear case (mirrors
	// the issue PATCH pattern).
	var rawFields map[string]json.RawMessage
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	if err := json.Unmarshal(body, &rawFields); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var req UpdateReleaseRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var approverID *pgtype.UUID
	approverProvided := false
	if _, present := rawFields["approver_id"]; present {
		approverProvided = true
		if req.ApproverID != nil && *req.ApproverID != "" {
			uid, ok := parseUUIDOrBadRequest(w, *req.ApproverID, "approver_id")
			if !ok {
				return
			}
			approverID = &uid
		} else {
			zero := pgtype.UUID{}
			approverID = &zero
		}
	}
	var secondApproverID *pgtype.UUID
	secondProvided := false
	if _, present := rawFields["second_approver_id"]; present {
		secondProvided = true
		if req.SecondApproverID != nil && *req.SecondApproverID != "" {
			uid, ok := parseUUIDOrBadRequest(w, *req.SecondApproverID, "second_approver_id")
			if !ok {
				return
			}
			secondApproverID = &uid
		} else {
			zero := pgtype.UUID{}
			secondApproverID = &zero
		}
	}

	svc := &ship.Service{Q: h.Queries}
	updated, err := svc.UpdateReleaseMetadata(
		r.Context(), rel.ID,
		req.Title, req.Description,
		approverID, secondApproverID,
		approverProvided, secondProvided,
		editor,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update release")
		return
	}
	count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), updated.ID)
	resp := releaseToResponse(updated, int(count))
	h.publish(protocol.EventReleaseUpdated, uuidToString(wsID), "member", userID, map[string]any{
		"release_id": uuidToString(updated.ID),
		"stage":      string(updated.Stage),
	})
	writeJSON(w, http.StatusOK, resp)
}

// AddPullRequestToReleaseRequest is the body for POST /api/releases/{id}/pull_requests.
type AddPullRequestToReleaseRequest struct {
	PullRequestID string `json:"pull_request_id"`
}

// AddPullRequestToRelease handles POST /api/releases/{id}/pull_requests.
func (h *Handler) AddPullRequestToRelease(w http.ResponseWriter, r *http.Request) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	addedBy, _ := h.parseUserUUIDOrZero(userID)

	var req AddPullRequestToReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prUUID, ok := parseUUIDOrBadRequest(w, req.PullRequestID, "pull_request_id")
	if !ok {
		return
	}

	svc := &ship.Service{Q: h.Queries}
	pr, err := svc.AddPullRequestToRelease(r.Context(), rel.ID, prUUID, addedBy)
	if err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseNotAssembling):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrReleasePullRequestNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ship.ErrReleasePullRequestIneligible):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ship.ErrReleasePullRequestInActive):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrReleasePullRequestProjectMismatch):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to add pull request: "+err.Error())
		}
		return
	}

	h.publish(protocol.EventReleaseUpdated, uuidToString(wsID), "member", userID, map[string]any{
		"release_id": uuidToString(rel.ID),
		"stage":      string(rel.Stage),
	})
	writeJSON(w, http.StatusOK, pullRequestToResponse(pr))
}

// RemovePullRequestFromRelease handles DELETE /api/releases/{id}/pull_requests/{pr_id}.
func (h *Handler) RemovePullRequestFromRelease(w http.ResponseWriter, r *http.Request) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	removedBy, _ := h.parseUserUUIDOrZero(userID)

	prUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "pr_id"), "pr_id")
	if !ok {
		return
	}

	svc := &ship.Service{Q: h.Queries}
	if err := svc.RemovePullRequestFromRelease(r.Context(), rel.ID, prUUID, removedBy); err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseNotAssembling):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to remove pull request: "+err.Error())
		}
		return
	}
	h.publish(protocol.EventReleaseUpdated, uuidToString(wsID), "member", userID, map[string]any{
		"release_id": uuidToString(rel.ID),
		"stage":      string(rel.Stage),
	})
	w.WriteHeader(http.StatusNoContent)
}

// CancelReleaseRequest is the body for POST /api/releases/{id}/cancel.
type CancelReleaseRequest struct {
	Reason string `json:"reason"`
}

// CancelRelease handles POST /api/releases/{id}/cancel.
func (h *Handler) CancelRelease(w http.ResponseWriter, r *http.Request) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	cancelledBy, _ := h.parseUserUUIDOrZero(userID)

	var req CancelReleaseRequest
	// Empty body is allowed; reason defaults to empty.
	_ = json.NewDecoder(r.Body).Decode(&req)

	svc := &ship.Service{Q: h.Queries}
	updated, err := svc.CancelRelease(r.Context(), rel.ID, req.Reason, cancelledBy,
		&releaseChannelOps{h: h}, &releaseIssueOps{h: h})
	if err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseNotAssembling):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to cancel release: "+err.Error())
		}
		return
	}
	count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), updated.ID)
	h.publish(protocol.EventReleaseCancelled, uuidToString(wsID), "member", userID, map[string]any{
		"release_id": uuidToString(updated.ID),
	})
	writeJSON(w, http.StatusOK, releaseToResponse(updated, int(count)))
}

