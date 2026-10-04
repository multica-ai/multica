package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueScheduleResponse is the wire shape for POST/GET/DELETE
// /api/issues/{id}/schedule (#5927).
type IssueScheduleResponse struct {
	ID              string  `json:"id"`
	IssueID         string  `json:"issue_id"`
	RunAt           string  `json:"run_at"`
	Status          string  `json:"status"`
	MissedPolicy    string  `json:"missed_policy"`
	CreatedByUserID string  `json:"created_by_user_id"`
	FiredAt         *string `json:"fired_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

func issueScheduleResponse(t db.IssueScheduledTrigger) IssueScheduleResponse {
	resp := IssueScheduleResponse{
		ID:              uuidToString(t.ID),
		IssueID:         uuidToString(t.IssueID),
		RunAt:           t.RunAt.Time.UTC().Format(time.RFC3339),
		Status:          t.Status,
		MissedPolicy:    t.MissedPolicy,
		CreatedByUserID: uuidToString(t.CreatedByUserID),
		CreatedAt:       t.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	if t.FiredAt.Valid {
		firedAt := t.FiredAt.Time.UTC().Format(time.RFC3339)
		resp.FiredAt = &firedAt
	}
	return resp
}

type createIssueScheduleRequest struct {
	RunAt string `json:"run_at"`
}

// CreateIssueSchedule handles POST /api/issues/{id}/schedule: register a
// one-time future run for the issue's CURRENT assignee (#5927).
//
// The invocation-permission gate (canInvokeIssueAssignee below) runs here,
// once, at creation time — see service.IssueScheduleService.DispatchIssueSchedule's
// doc comment for why the scheduler's fire-time dispatch deliberately does
// not re-check it.
func (h *Handler) CreateIssueSchedule(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)

	var req createIssueScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runAt, err := time.Parse(time.RFC3339, req.RunAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "run_at must be an RFC3339 timestamp")
		return
	}

	// hasAssignee before the permission gate: an unassigned issue has no
	// agent/squad for canInvokeIssueAssignee to judge, so checking the gate
	// first would report a misleading 403 ("you cannot trigger this
	// assignee") instead of the accurate 400 ("no assignee").
	if !issue.AssigneeType.Valid || issue.AssigneeType.String == "" || !issue.AssigneeID.Valid {
		writeError(w, http.StatusBadRequest, "issue has no assignee to schedule a run for")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeIssueAssignee(r, issue, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you cannot trigger this issue's assignee")
		return
	}

	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user")
		return
	}

	trigger, err := h.IssueScheduleService.CreateSchedule(r.Context(), issue, runAt, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrIssueScheduleRunAtNotFuture):
			writeError(w, http.StatusBadRequest, "run_at must be in the future")
		case errors.Is(err, service.ErrIssueScheduleNoAssignee):
			writeError(w, http.StatusBadRequest, "issue has no assignee to schedule a run for")
		case errors.Is(err, service.ErrIssueScheduleAlreadyPending):
			writeError(w, http.StatusConflict, "issue already has a pending schedule")
		default:
			slog.Error("create issue schedule failed", "issue_id", uuidToString(issue.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create schedule")
		}
		return
	}

	writeJSON(w, http.StatusCreated, issueScheduleResponse(trigger))
}

// GetIssueSchedule handles GET /api/issues/{id}/schedule: read the issue's
// pending schedule, if any. Returns 404 (not an empty 200) when there is
// none, so the frontend can distinguish "never scheduled" from a transport
// failure the same way every other single-resource GET in this handler
// does.
func (h *Handler) GetIssueSchedule(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	trigger, err := h.IssueScheduleService.GetPendingSchedule(r.Context(), issue.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no pending schedule")
			return
		}
		slog.Error("get issue schedule failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load schedule")
		return
	}

	writeJSON(w, http.StatusOK, issueScheduleResponse(trigger))
}

// CancelIssueSchedule handles DELETE /api/issues/{id}/schedule: cancel the
// issue's pending schedule, if any.
func (h *Handler) CancelIssueSchedule(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	trigger, err := h.IssueScheduleService.CancelSchedule(r.Context(), issue.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no pending schedule")
			return
		}
		slog.Error("cancel issue schedule failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to cancel schedule")
		return
	}

	writeJSON(w, http.StatusOK, issueScheduleResponse(trigger))
}

// canInvokeIssueAssignee is the schedule-creation-time invocation-permission
// gate: can the requesting actor trigger issue's CURRENT assignee (agent
// directly, or squad leader)? Mirrors issueTriggerPreviewProbe's
// CanAccessAgent wiring (agent_access.go) and enqueueSquadLeaderTask's
// canEnqueueSquadLeader call (squad.go), but as a pure check with no enqueue
// side effect — this endpoint only registers a future trigger, it never
// dispatches one itself.
func (h *Handler) canInvokeIssueAssignee(r *http.Request, issue db.Issue, actorType, actorID, workspaceID string) bool {
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return false
	}
	ctx := r.Context()
	originatorUserID := h.invokeOriginatorFromRequest(r, actorType, actorID)
	switch issue.AssigneeType.String {
	case "agent":
		agent, err := h.Queries.GetAgent(ctx, issue.AssigneeID)
		if err != nil {
			return false
		}
		return h.canInvokeAgent(ctx, agent, actorType, actorID, originatorUserID, workspaceID)
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          issue.AssigneeID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return false
		}
		return h.canEnqueueSquadLeader(ctx, squad.LeaderID, actorType, actorID, originatorUserID, workspaceID)
	default:
		return false
	}
}
