// Phase 7b — Merge train HTTP handlers.
//
// Endpoints:
//   POST /api/releases/{id}/start_merge   → kicks off the orchestrator
//   POST /api/releases/{id}/resume_merge  → resumes a paused train
//   POST /api/releases/{id}/abort_merge   → aborts merging release
//   GET  /api/releases/{id}/merge_state   → poll endpoint for non-WS UIs
//
// All endpoints sit under the same workspace-member middleware as the
// rest of Ship Hub Phase 7a. Auth + workspace scoping flow through
// the Phase-1 helpers (requireShipHubEnabled, loadRelease).

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service/channel"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// busMergePublisher adapts h.Bus to ship.MergeEventPublisher. Defining
// it as a small wrapper here keeps the ship package independent of
// the events package.
type busMergePublisher struct{ bus *events.Bus }

func (p *busMergePublisher) PublishMergeEvent(eventType, workspaceID string, payload map[string]any) {
	if p == nil || p.bus == nil {
		return
	}
	p.bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

// mergeTrainDeps builds the dependency bundle the service expects.
// Centralized so the three merge endpoints share one wiring path.
//
// PostToReleaseChannel is wired to the orchestrator-agent attribution
// path so progress lines render as system / agent posts in the channel
// (no specific user is "the" author of a multi-PR train). When the
// workspace doesn't have an orchestrator agent configured, the post
// is silently dropped — the channel surface degrades gracefully and
// the merge train itself is unaffected.
func (h *Handler) mergeTrainDeps(workspaceID pgtype.UUID) *ship.MergeTrainDeps {
	parentCtx := h.ServiceCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return &ship.MergeTrainDeps{
		ParentCtx:            parentCtx,
		ChannelOps:           &releaseChannelOps{h: h},
		Publisher:            &busMergePublisher{bus: h.Bus},
		PostToReleaseChannel: h.makeReleaseChannelPoster(workspaceID),
	}
}

// makeReleaseChannelPoster returns a closure that posts to the
// release's auto-created channel attributed to the workspace's
// orchestrator agent. Returns nil (gracefully no-op-able by the
// service-side wrapper) when no orchestrator agent is configured.
func (h *Handler) makeReleaseChannelPoster(workspaceID pgtype.UUID) func(ctx context.Context, channelID pgtype.UUID, content string) error {
	// Resolve once per orchestrator start-up — the workspace's
	// orchestrator agent is stable for the lifetime of a release.
	ws, err := h.Queries.GetWorkspace(context.Background(), workspaceID)
	if err != nil || !ws.OrchestratorAgentID.Valid {
		return nil
	}
	agentID := ws.OrchestratorAgentID
	return func(ctx context.Context, channelID pgtype.UUID, content string) error {
		_, err := h.ChannelMessageService.Create(ctx, channel.CreateMessageParams{
			ChannelID: channelID,
			Author:    channel.Actor{Type: channel.ActorAgent, ID: agentID},
			Content:   content,
		})
		return err
	}
}

// ----- request shapes -------------------------------------------------------

// StartMergeRequest is the body for POST /api/releases/{id}/start_merge.
// merge_method is optional; empty / absent defaults to "merge".
type StartMergeRequest struct {
	MergeMethod string `json:"merge_method"`
}

// ResumeMergeRequest is the body for POST /api/releases/{id}/resume_merge.
// skip_pr_ids lists membership rows that the user wants to abandon
// (mark merge_state=skipped) before the train resumes.
type ResumeMergeRequest struct {
	SkipPRIDs []string `json:"skip_pr_ids"`
}

// AbortMergeRequest is the body for POST /api/releases/{id}/abort_merge.
type AbortMergeRequest struct {
	Reason string `json:"reason"`
}

// ----- handlers -------------------------------------------------------------

// StartMergeRelease handles POST /api/releases/{id}/start_merge.
// Returns 202 Accepted on success; clients poll merge_state or listen
// on WS for progress.
func (h *Handler) StartMergeRelease(w http.ResponseWriter, r *http.Request) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	requestedBy, _ := h.parseUserUUIDOrZero(userID)

	var req StartMergeRequest
	// Empty body is allowed; the merge_method defaults to "merge"
	// (Phase 7a's default).
	_ = json.NewDecoder(r.Body).Decode(&req)

	svc, ok := h.shipServiceFromWorkspace(w, r, ws, true)
	if !ok {
		return
	}

	deps := h.mergeTrainDeps(wsID)
	if err := svc.StartMerge(r.Context(), rel.ID, requestedBy, req.MergeMethod, deps); err != nil {
		// Translate typed errors into the appropriate HTTP status.
		switch {
		case errors.Is(err, ship.ErrInvalidMergeMethod):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ship.ErrReleaseStageMismatch):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrMergeAlreadyRunning):
			// Idempotent: someone else just started it. 202 Accepted
			// with a no-op body is friendlier than 409 because the
			// caller's intent (the train is running) is satisfied.
			writeJSON(w, http.StatusAccepted, map[string]any{
				"release_id": uuidToString(rel.ID),
				"status":     "already_running",
			})
		case errors.Is(err, ship.ErrTokenMissing):
			writeError(w, http.StatusBadRequest, "GitHub token not configured")
		case errors.Is(err, ship.ErrPreconditionFailed):
			// MergePreconditionError carries the per-reason list; surface
			// it in the response so the dialog can render each entry.
			var pre *ship.MergePreconditionError
			if errors.As(err, &pre) {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
					"error":   err.Error(),
					"reasons": pre.Reasons,
				})
				return
			}
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			slog.Warn("ship: start merge failed",
				"release_id", uuidToString(rel.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to start merge: "+err.Error())
		}
		return
	}

	// 202 Accepted — orchestrator runs in the background.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"release_id": uuidToString(rel.ID),
		"status":     "started",
	})
}

// ResumeMergeRelease handles POST /api/releases/{id}/resume_merge.
func (h *Handler) ResumeMergeRelease(w http.ResponseWriter, r *http.Request) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	requestedBy, _ := h.parseUserUUIDOrZero(userID)

	var req ResumeMergeRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	skip := make([]pgtype.UUID, 0, len(req.SkipPRIDs))
	for _, idStr := range req.SkipPRIDs {
		uid, ok := parseUUIDOrBadRequest(w, idStr, "skip_pr_ids")
		if !ok {
			return
		}
		skip = append(skip, uid)
	}

	svc, ok := h.shipServiceFromWorkspace(w, r, ws, true)
	if !ok {
		return
	}

	deps := h.mergeTrainDeps(wsID)
	if err := svc.ResumeMerge(r.Context(), rel.ID, requestedBy, skip, deps); err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseStageMismatch):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrMergeNotPaused):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrMergeAlreadyRunning):
			writeJSON(w, http.StatusAccepted, map[string]any{
				"release_id": uuidToString(rel.ID),
				"status":     "already_running",
			})
		case errors.Is(err, ship.ErrTokenMissing):
			writeError(w, http.StatusBadRequest, "GitHub token not configured")
		default:
			slog.Warn("ship: resume merge failed",
				"release_id", uuidToString(rel.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to resume merge: "+err.Error())
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"release_id": uuidToString(rel.ID),
		"status":     "resumed",
	})
}

// AbortMergeRelease handles POST /api/releases/{id}/abort_merge.
func (h *Handler) AbortMergeRelease(w http.ResponseWriter, r *http.Request) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	cancelledBy, _ := h.parseUserUUIDOrZero(userID)

	var req AbortMergeRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	svc := &ship.Service{Q: h.Queries}
	deps := h.mergeTrainDeps(wsID)
	updated, err := svc.AbortMergeTrain(
		r.Context(), rel.ID, strings.TrimSpace(req.Reason), cancelledBy,
		&releaseChannelOps{h: h}, &releaseIssueOps{h: h}, deps,
	)
	if err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseStageMismatch):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to abort merge: "+err.Error())
		}
		return
	}
	count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), updated.ID)
	h.publish(protocol.EventReleaseCancelled, uuidToString(wsID), "member", userID, map[string]any{
		"release_id": uuidToString(updated.ID),
	})
	writeJSON(w, http.StatusOK, releaseToResponse(updated, int(count)))
}

// MergeStateResponse is the GET /api/releases/{id}/merge_state body.
// Lightweight on purpose — clients that aren't on WS can poll this
// every couple seconds without re-loading the full release detail.
type MergeStateResponse struct {
	ReleaseID    string                 `json:"release_id"`
	Stage        string                 `json:"stage"`
	MergePaused  bool                   `json:"merge_paused"`
	MergeMethod  string                 `json:"merge_method"`
	MergedCount  int                    `json:"merged_count"`
	Total        int                    `json:"total"`
	PullRequests []mergeStatePRResponse `json:"pull_requests"`
}

type mergeStatePRResponse struct {
	PullRequestID string  `json:"pull_request_id"`
	Position      int32   `json:"position"`
	MergeState    string  `json:"merge_state"`
	MergedSHA     *string `json:"merged_sha"`
	MergeError    *string `json:"merge_error"`
}

// GetReleaseMergeState handles GET /api/releases/{id}/merge_state.
func (h *Handler) GetReleaseMergeState(w http.ResponseWriter, r *http.Request) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	rows, err := h.Queries.ListReleasePullRequests(r.Context(), rel.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list release PRs")
		return
	}
	prs := make([]mergeStatePRResponse, 0, len(rows))
	merged := 0
	for _, row := range rows {
		if row.MembershipMergeState == db.PrMergeStateMerged {
			merged++
		}
		prs = append(prs, mergeStatePRResponse{
			PullRequestID: uuidToString(row.ID),
			Position:      row.MembershipPosition,
			MergeState:    string(row.MembershipMergeState),
			MergedSHA:     textToPtr(row.MembershipMergedSha),
			MergeError:    textToPtr(row.MembershipMergeError),
		})
	}
	writeJSON(w, http.StatusOK, MergeStateResponse{
		ReleaseID:    uuidToString(rel.ID),
		Stage:        string(rel.Stage),
		MergePaused:  rel.MergePaused,
		MergeMethod:  rel.MergeMethod,
		MergedCount:  merged,
		Total:        len(rows),
		PullRequests: prs,
	})
}
