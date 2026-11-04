package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

// Company controls (spec 4.5). Reads are open to every member. Changing the
// state and the quiet hours is for owners and admins; agents cannot change them.

// QuietHoursBody is the daily quiet window, as the API reads and writes it.
type QuietHoursBody struct {
	Enabled  bool   `json:"enabled"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone"`
}

// CompanyStateResponse is the company's state now.
type CompanyStateResponse struct {
	// State is the state in force now: running, paused or quiet.
	State string `json:"state"`
	// Setting is the state the boss set. A quiet window does not change it.
	Setting    string         `json:"setting"`
	QuietHours QuietHoursBody `json:"quiet_hours"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// SetCompanyStateRequest changes the state. stop_running with paused also
// cancels the running routine and delegated runs.
type SetCompanyStateRequest struct {
	State       string `json:"state"`
	StopRunning bool   `json:"stop_running"`
}

func companyStateResponse(v service.CompanyStateView) CompanyStateResponse {
	return CompanyStateResponse{
		State:   v.State,
		Setting: v.Setting,
		QuietHours: QuietHoursBody{
			Enabled: v.QuietHours.Enabled, Start: v.QuietHours.Start,
			End: v.QuietHours.End, Timezone: v.QuietHours.Timezone,
		},
		UpdatedAt: v.UpdatedAt,
	}
}

// companyStateService builds the service for one request.
func (h *Handler) companyStateService() *service.CompanyStateService {
	return &service.CompanyStateService{Queries: h.Queries, TxStarter: h.TxStarter, Tasks: h.TaskService}
}

// companyActorFor resolves who changes the controls. Only an owner or admin
// member may; an agent or any other member is refused.
func (h *Handler) companyActorFor(w http.ResponseWriter, r *http.Request, workspaceID string) (service.GovernanceActor, bool) {
	c, ok := h.initiativeCallerFor(w, r, workspaceID)
	if !ok {
		return service.GovernanceActor{}, false
	}
	if !c.admin {
		writeError(w, http.StatusForbidden, "only the owner or an admin can change the company's controls")
		return service.GovernanceActor{}, false
	}
	return service.GovernanceActor{Type: c.actor.Type, ID: c.actor.ID}, true
}

// GetCompanyState returns the company's state and quiet hours.
func (h *Handler) GetCompanyState(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	view, err := h.companyStateService().Get(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("read company state failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to read the company state")
		return
	}
	writeJSON(w, http.StatusOK, companyStateResponse(view))
}

// SetCompanyState changes the state. Pausing with stop_running also cancels the
// running routine and delegated runs.
func (h *Handler) SetCompanyState(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	actor, ok := h.companyActorFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req SetCompanyStateRequest
	if !decodeInitiativeBody(w, r, &req) {
		return
	}
	if req.StopRunning && req.State != service.CompanyStatePaused {
		writeError(w, http.StatusBadRequest, "stop_running applies only when the state is paused")
		return
	}
	svc := h.companyStateService()
	var (
		view service.CompanyStateView
		err  error
	)
	if req.StopRunning {
		view, _, err = svc.PauseAndStop(r.Context(), wsUUID, actor)
	} else {
		view, err = svc.Set(r.Context(), wsUUID, actor, req.State)
	}
	if errors.Is(err, service.ErrCompanyStateInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		slog.Warn("set company state failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to change the company state")
		return
	}
	writeJSON(w, http.StatusOK, companyStateResponse(view))
}

// SetQuietHours saves the daily quiet window. The response is the company state.
func (h *Handler) SetQuietHours(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.companyActorFor(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req QuietHoursBody
	if !decodeInitiativeBody(w, r, &req) {
		return
	}
	view, err := h.companyStateService().SetQuietHours(r.Context(), wsUUID, service.QuietHours{
		Enabled: req.Enabled, Start: req.Start, End: req.End, Timezone: req.Timezone,
	})
	if errors.Is(err, service.ErrQuietHoursInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		slog.Warn("save quiet hours failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save the quiet hours")
		return
	}
	writeJSON(w, http.StatusOK, companyStateResponse(view))
}
