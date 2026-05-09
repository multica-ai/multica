// Phase 3 — Ship Hub card-action chip handlers.
//
// Each chip on the Kanban card maps to one POST endpoint registered in
// router.go. They share a small amount of boilerplate (gate on
// ship_hub_enabled, load the PR, build a workspace-scoped service,
// authorize per-action) which lives in dispatchAction.
//
// Authorization model:
//
//   - Destructive actions (merge, dismiss_review, close_as_stale,
//     run_smoke_tests) require workspace owner/admin role.
//   - Non-destructive actions (comment, rebase_on_main, nudge_author,
//     diagnose_ci_failure, summarize_review_feedback) require any
//     workspace member.
//
// We deliberately don't have a separate "project member" tier — projects
// in this codebase are workspace-wide (no project_member table). The
// destructive-action gate is the strongest available signal.

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/service/channel"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// loadPullRequestForAction resolves the PR by its UUID URL param, gates
// on ship_hub_enabled, builds a Service with the workspace's GitHub
// token, and returns everything the chip dispatcher needs.
//
// Returns ok=false (with the error response already written) when the
// PR doesn't exist, the workspace flag is off, or the token isn't
// configured. The handler must return immediately on ok=false.
func (h *Handler) loadPullRequestForAction(w http.ResponseWriter, r *http.Request) (
	*ship.Service,
	db.PullRequest,
	pgtype.UUID,
	db.Workspace,
	bool,
) {
	wsID, ws, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return nil, db.PullRequest{}, pgtype.UUID{}, db.Workspace{}, false
	}
	prUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "pull request id")
	if !ok {
		return nil, db.PullRequest{}, pgtype.UUID{}, db.Workspace{}, false
	}
	pr, err := h.Queries.GetPullRequest(r.Context(), prUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return nil, db.PullRequest{}, pgtype.UUID{}, db.Workspace{}, false
	}
	if uuidToString(pr.WorkspaceID) != uuidToString(wsID) {
		// PR belongs to a different workspace — surface as 404 (don't
		// confirm existence to a non-member).
		writeError(w, http.StatusNotFound, "pull request not found")
		return nil, db.PullRequest{}, pgtype.UUID{}, db.Workspace{}, false
	}
	svc, ok := h.shipServiceFromWorkspace(w, r, ws, true)
	if !ok {
		return nil, db.PullRequest{}, pgtype.UUID{}, db.Workspace{}, false
	}
	return svc, pr, wsID, ws, true
}

// readActionBody drains the request body for the chip handlers. We
// accept up to 64 KiB — comment bodies are the largest realistic
// payload and 64 KiB matches GitHub's own comment-body limit.
const maxActionBodyBytes = 64 * 1024

func readActionBody(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	if r.Body == nil {
		return nil, true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxActionBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return nil, false
	}
	if len(body) > maxActionBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return nil, false
	}
	return body, true
}

// shipTaskEnqueuer adapts the concrete *service.TaskService to the
// ship.TaskEnqueuer interface. The adapter exists so the ship service
// stays free of a direct dependency on the handler-layer service
// package — see actions.go for why TaskEnqueuer is defined there.
type shipTaskEnqueuer struct {
	inner *service.TaskService
}

func (e *shipTaskEnqueuer) EnqueueShipCardActionTask(ctx context.Context, p ship.ShipCardActionTaskRequest) (db.AgentTaskQueue, error) {
	return e.inner.EnqueueShipCardActionTask(ctx, service.EnqueueShipCardActionTaskParams{
		WorkspaceID:   p.WorkspaceID,
		AgentID:       p.AgentID,
		ProjectID:     p.ProjectID,
		PullRequestID: p.PullRequestID,
		RepoURL:       p.RepoURL,
		PRNumber:      p.PRNumber,
		HeadSHA:       p.HeadSHA,
		RequesterID:   p.RequesterID,
		Action:        p.Action,
	})
}

// destructiveActions are the chips that require owner/admin privileges.
// Centralized so the gate lives in one place — adding a new destructive
// action is a one-line change.
var destructiveActions = map[string]bool{
	ship.ActionMerge:         true,
	ship.ActionDismissReview: true,
	ship.ActionCloseAsStale:  true,
	ship.ActionRunSmokeTests: true,
}

// dispatchAction is the shared entry point invoked by every chip
// endpoint. It handles auth, body parsing, ExecuteAction invocation,
// the WS event publish, and the HTTP status mapping.
func (h *Handler) dispatchAction(w http.ResponseWriter, r *http.Request, action string) {
	svc, pr, wsID, ws, ok := h.loadPullRequestForAction(w, r)
	if !ok {
		return
	}
	// Authorization: every action requires workspace membership.
	// Destructive actions require owner or admin on top.
	wsIDStr := uuidToString(wsID)
	var member db.Member
	if destructiveActions[action] {
		var memberOK bool
		member, memberOK = h.requireWorkspaceRole(w, r, wsIDStr, "workspace not found", "owner", "admin")
		if !memberOK {
			return
		}
	} else {
		var memberOK bool
		member, memberOK = h.requireWorkspaceMember(w, r, wsIDStr, "workspace not found")
		if !memberOK {
			return
		}
	}

	body, ok := readActionBody(w, r)
	if !ok {
		return
	}

	// orchestrator agent + smoke workflow are only required by some
	// actions; we always read them so the service stays oblivious.
	smokeWorkflow := ""
	if ws.ShipHubSmokeWorkflow.Valid {
		smokeWorkflow = ws.ShipHubSmokeWorkflow.String
	}

	enqueuer := &shipTaskEnqueuer{inner: h.TaskService}

	// Phase 6.5 — wire the channel-post hook for the submit_review action.
	// Other actions don't read this field; setting it unconditionally is
	// cheap and keeps the loader path simple. The author is the human
	// who clicked the chip — submitting a review is a personal action,
	// not a system notice, so the channel post should attribute to them.
	memberUserID := member.UserID
	svc.PostToPRChannel = func(ctx context.Context, channelID pgtype.UUID, content string) error {
		_, err := h.ChannelMessageService.Create(ctx, channel.CreateMessageParams{
			ChannelID: channelID,
			Author:    channel.Actor{Type: channel.ActorMember, ID: memberUserID},
			Content:   content,
		})
		return err
	}

	result, err := svc.ExecuteAction(
		r.Context(),
		wsID,
		pr,
		member.UserID,
		action,
		body,
		enqueuer,
		ws.OrchestratorAgentID,
		smokeWorkflow,
	)
	if err != nil {
		status := mapActionError(err)
		// On error we still return the ActionResult so the frontend can
		// show the audit-trail row id + recorded reason.
		if result == nil {
			result = &ship.ActionResult{Status: ship.StatusFailed, Error: err.Error()}
		}
		writeJSON(w, status, result)
		return
	}

	// Publish the audit signal so other clients refresh the card's
	// "recent actions" footer in real time.
	h.publish(protocol.EventCardAction, wsIDStr, "member", uuidToString(member.UserID), map[string]any{
		"pull_request_id": uuidToString(pr.ID),
		"action":          action,
		"result":          result,
	})

	writeJSON(w, http.StatusOK, result)
}

// mapActionError translates the typed errors from the service + GitHub
// client into HTTP statuses. Defaults to 500 for anything unexpected so
// a leaky abstraction doesn't get rendered as a 200.
func mapActionError(err error) int {
	switch {
	case errors.Is(err, ship.ErrInvalidPayload):
		return http.StatusBadRequest
	case errors.Is(err, ship.ErrActionUnknown):
		return http.StatusBadRequest
	case errors.Is(err, ship.ErrSmokeWorkflowNotConfigured):
		return http.StatusBadRequest
	case errors.Is(err, ship.ErrNotImplemented):
		return http.StatusNotImplemented
	case errors.Is(err, gh.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, gh.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, gh.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, gh.ErrUnprocessable):
		return http.StatusUnprocessableEntity
	case errors.Is(err, gh.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, gh.ErrRateLimited):
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// MergePullRequest — POST /api/pull_requests/{id}/merge
func (h *Handler) MergePullRequest(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionMerge)
}

// RebasePullRequestOnMain — POST /api/pull_requests/{id}/rebase_on_main
func (h *Handler) RebasePullRequestOnMain(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionRebaseOnMain)
}

// CommentOnPullRequest — POST /api/pull_requests/{id}/comment
func (h *Handler) CommentOnPullRequest(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionComment)
}

// DismissPullRequestReview — POST /api/pull_requests/{id}/dismiss_review
func (h *Handler) DismissPullRequestReview(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionDismissReview)
}

// DiagnoseCIFailure — POST /api/pull_requests/{id}/diagnose_ci_failure
func (h *Handler) DiagnoseCIFailure(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionDiagnoseCIFailure)
}

// SummarizeReviewFeedback — POST /api/pull_requests/{id}/summarize_review_feedback
func (h *Handler) SummarizeReviewFeedback(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionSummarizeReviewFeedback)
}

// NudgeAuthor — POST /api/pull_requests/{id}/nudge_author
func (h *Handler) NudgeAuthor(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionNudgeAuthor)
}

// RunSmokeTests — POST /api/pull_requests/{id}/run_smoke_tests
func (h *Handler) RunSmokeTests(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionRunSmokeTests)
}

// ClosePullRequestAsStale — POST /api/pull_requests/{id}/close_as_stale
func (h *Handler) ClosePullRequestAsStale(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionCloseAsStale)
}

// SubmitPullRequestReview — POST /api/pull_requests/{id}/review (Phase 6.5).
// Posts a GitHub PR review (APPROVE / REQUEST_CHANGES / COMMENT) without
// leaving Multica. Workspace-member auth (same as comment); the destructive
// gate is intentionally NOT applied — submitting a review is reversible
// (dismissal exists) and reads as a normal collaborative action.
func (h *Handler) SubmitPullRequestReview(w http.ResponseWriter, r *http.Request) {
	h.dispatchAction(w, r, ship.ActionSubmitReview)
}
