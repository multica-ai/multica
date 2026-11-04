package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The starter company (spec 3.4). Owners and admins only.
//
//	POST /api/company/company-template/apply   install the chosen squads; idempotent

// companyTemplateModelMax bounds the worker model name the request may name.
const companyTemplateModelMax = 200

// CompanyTemplateApplyRequest is the wizard's apply. Model is optional; empty
// means the runtime's default. Squads are card keys; Start is all, per_floor or
// leave_paused.
type CompanyTemplateApplyRequest struct {
	RuntimeID string   `json:"runtime_id"`
	Model     string   `json:"model"`
	Squads    []string `json:"squads"`
	Start     string   `json:"start"`
}

// CompanyTemplateApplyResponse reports each step. A step with status "failed"
// stops the apply; running the call again completes it. PipelineID is null when
// the apply did not reach the pipeline.
type CompanyTemplateApplyResponse struct {
	Steps         []service.CompanyStep `json:"steps"`
	PipelineID    *string               `json:"pipeline_id"`
	UnownedStages []string              `json:"unowned_stages"`
}

// ApplyCompanyTemplate installs the starter company. A refused request (no such
// runtime, an unknown squad) is a 4xx. A failed step is a 200 whose steps say so,
// because the wizard decides what to show from the steps.
func (h *Handler) ApplyCompanyTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req CompanyTemplateApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runtimeID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	model := strings.TrimSpace(req.Model)
	if utf8.RuneCountInString(model) > companyTemplateModelMax {
		writeError(w, http.StatusBadRequest, "model must be 200 characters or fewer")
		return
	}

	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeID,
		WorkspaceID: wsUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "runtime_id is not a runtime of this workspace")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the runtime")
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "you cannot use this runtime for an agent")
		return
	}

	svc := service.NewCompanyTemplateService(h.Queries, h.TxStarter, h.Knowledge)
	res, err := svc.Apply(r.Context(), service.CompanyApplyInput{
		WorkspaceID: wsUUID,
		ActorUserID: member.UserID,
		Runtime:     runtime,
		Model:       model,
		Squads:      req.Squads,
		Start:       req.Start,
	})
	if errors.Is(err, service.ErrCompanyTemplateInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		slog.Warn("starter company apply failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to apply the starter company")
		return
	}
	userID := uuidToString(member.UserID)
	h.publishCompanyTemplate(r, workspaceID, userID, res)

	resp := CompanyTemplateApplyResponse{Steps: res.Steps, UnownedStages: res.UnownedStages}
	if resp.Steps == nil {
		resp.Steps = []service.CompanyStep{}
	}
	if resp.UnownedStages == nil {
		resp.UnownedStages = []string{}
	}
	if res.PipelineID.Valid {
		id := uuidToString(res.PipelineID)
		resp.PipelineID = &id
	}
	writeJSON(w, http.StatusOK, resp)
}

// publishCompanyTemplate tells clients about the rows the apply created. Reused
// rows are not published again.
func (h *Handler) publishCompanyTemplate(r *http.Request, workspaceID, userID string, res service.CompanyApplyResult) {
	ctx := r.Context()
	for _, f := range res.Floors {
		h.publish(protocol.EventCompanyFloorCreated, workspaceID, "member", userID, map[string]any{
			"floor": companyFloorToResponse(f),
		})
	}
	for _, a := range res.Agents {
		h.publish(protocol.EventAgentCreated, workspaceID, "member", userID, map[string]any{
			"agent": broadcastAgentResponse(h.agentToResponse(a)),
		})
	}
	for _, sq := range res.Squads {
		if resp, err := h.squadToResponseWithPreview(ctx, sq); err == nil {
			h.publish(protocol.EventSquadCreated, workspaceID, "member", userID, map[string]any{"squad": resp})
		}
	}
	for _, ap := range res.Autopilots {
		h.publish(protocol.EventAutopilotCreated, workspaceID, "member", userID, map[string]any{
			"autopilot": autopilotToResponse(ap, nil),
		})
	}
}
