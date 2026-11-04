package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The Secretary setup for the digital company. Owners and admins only.
//
//	GET  /api/company/setup   readiness checklist, no changes
//	POST /api/company/setup   idempotent setup; returns the checklist after it runs

// companySetupRuntimeProvider is the runtime the Secretary runs on. Hermes keeps
// the Secretary's memory per runtime, which is why it is bound to one machine.
const companySetupRuntimeProvider = "hermes"

// CompanySetupReadinessResponse is the checklist the first-run card reads.
// SecretaryID is null until the workspace names a live Secretary.
type CompanySetupReadinessResponse struct {
	RuntimeOnline         bool    `json:"runtime_online"`
	SecretaryID           *string `json:"secretary_id"`
	ExecutiveFloor        bool    `json:"executive_floor"`
	AutopilotDaily        bool    `json:"autopilot_daily"`
	AutopilotWeeklyPaused bool    `json:"autopilot_weekly_paused"`
	Ready                 bool    `json:"ready"`
}

type companySetupRequest struct {
	// SecretaryInstructions is the secretary instruction template. It is required
	// on every call; an existing Secretary keeps the instructions it has.
	SecretaryInstructions string `json:"secretary_instructions"`
	// Timezone is the IANA timezone of the 08:00 daily brief. When empty, the
	// caller's saved timezone is used, then UTC.
	Timezone string `json:"timezone"`
}

func companySetupReadinessToResponse(r service.CompanySetupReadiness) CompanySetupReadinessResponse {
	resp := CompanySetupReadinessResponse{
		RuntimeOnline:         r.RuntimeOnline,
		ExecutiveFloor:        r.ExecutiveFloor,
		AutopilotDaily:        r.AutopilotDaily,
		AutopilotWeeklyPaused: r.AutopilotWeeklyPaused,
		Ready:                 r.Ready,
	}
	if r.SecretaryID != "" {
		id := r.SecretaryID
		resp.SecretaryID = &id
	}
	return resp
}

// secretaryRuntime returns the first online Hermes runtime in the workspace that
// the member may use to create an agent.
func (h *Handler) secretaryRuntime(ctx context.Context, member db.Member, workspaceID pgtype.UUID) (db.AgentRuntime, bool, error) {
	runtimes, err := h.Queries.ListAgentRuntimes(ctx, workspaceID)
	if err != nil {
		return db.AgentRuntime{}, false, err
	}
	for _, rt := range runtimes {
		if rt.Provider == companySetupRuntimeProvider && rt.Status == "online" && canUseRuntimeForAgent(member, rt) {
			return rt, true, nil
		}
	}
	return db.AgentRuntime{}, false, nil
}

// GetCompanySetup reports the readiness checklist. It changes nothing.
func (h *Handler) GetCompanySetup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	_, online, err := h.secretaryRuntime(r.Context(), member, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load runtimes")
		return
	}
	readiness, err := service.NewCompanySetupService(h.Queries, h.TxStarter).Readiness(r.Context(), wsUUID, online)
	if err != nil {
		slog.Warn("company setup readiness failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to read the company setup")
		return
	}
	writeJSON(w, http.StatusOK, companySetupReadinessToResponse(readiness))
}

// SetupCompany creates the Secretary, its Executive floor and squad, the setting
// that names the Secretary, and the daily and weekly brief autopilots. Anything
// that exists already is reused, so a second call changes nothing.
//
// Without an online Hermes runtime the call is refused with 409
// runtime_required and the install path, before anything is written.
func (h *Handler) SetupCompany(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req companySetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	instructions := strings.TrimSpace(req.SecretaryInstructions)
	if instructions == "" {
		writeError(w, http.StatusBadRequest, "secretary_instructions is required")
		return
	}
	if utf8.RuneCountInString(instructions) > service.CompanySetupInstructionsMax {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("secretary_instructions must be %d characters or fewer", service.CompanySetupInstructionsMax))
		return
	}
	timezone := strings.TrimSpace(req.Timezone)
	if timezone != "" {
		if err := service.ValidateTimezone(timezone); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	runtime, online, err := h.secretaryRuntime(r.Context(), member, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load runtimes")
		return
	}
	if !online {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "an online Hermes runtime is required before the Secretary can be set up",
			"code":  "runtime_required",
			"hint":  "Install Hermes on the machine that runs the Multica daemon, start the daemon, then try again.",
			// Relative to the docs site, which serves the install guide here.
			"docs_path": "/install-agent-runtime",
		})
		return
	}

	svc := service.NewCompanySetupService(h.Queries, h.TxStarter)
	created, err := svc.Setup(r.Context(), service.CompanySetupInput{
		WorkspaceID:           wsUUID,
		Actor:                 member,
		Runtime:               runtime,
		SecretaryInstructions: instructions,
		Timezone:              timezone,
	})
	if err != nil {
		writeCompanySetupError(w, r, workspaceID, err)
		return
	}
	h.publishCompanySetup(r.Context(), workspaceID, uuidToString(member.UserID), created)

	readiness, err := svc.Readiness(r.Context(), wsUUID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the company setup")
		return
	}
	writeJSON(w, http.StatusOK, companySetupReadinessToResponse(readiness))
}

func writeCompanySetupError(w http.ResponseWriter, r *http.Request, workspaceID string, err error) {
	switch {
	case errors.Is(err, service.ErrSecretaryNameTaken):
		writeErrorCode(w, http.StatusConflict, "secretary_name_taken", err.Error())
	case errors.Is(err, service.ErrExecutiveFloorFull):
		writeErrorCode(w, http.StatusConflict, "floor_full", err.Error())
	case errors.Is(err, service.ErrCompanyFloorLimit):
		writeErrorCode(w, http.StatusConflict, "floor_limit", err.Error())
	default:
		slog.Warn("company setup failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to set up the company")
	}
}

// publishCompanySetup tells clients about what the call created. Reused rows are
// not published again.
func (h *Handler) publishCompanySetup(ctx context.Context, workspaceID, userID string, created service.CompanySetupCreated) {
	if created.Floor != nil {
		h.publish(protocol.EventCompanyFloorCreated, workspaceID, "member", userID, map[string]any{
			"floor": companyFloorToResponse(*created.Floor),
		})
	}
	if created.Secretary != nil {
		h.publish(protocol.EventAgentCreated, workspaceID, "member", userID, map[string]any{
			"agent": broadcastAgentResponse(h.agentToResponse(*created.Secretary)),
		})
	}
	if created.Squad != nil {
		if sr, err := h.squadToResponseWithPreview(ctx, *created.Squad); err == nil {
			h.publish(protocol.EventSquadCreated, workspaceID, "member", userID, map[string]any{"squad": sr})
		}
	}
	if created.Workspace != nil {
		h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{
			"workspace": h.workspaceToResponse(*created.Workspace),
		})
	}
	for _, ap := range []*db.Autopilot{created.DailyAutopilot, created.WeeklyAutopilot} {
		if ap != nil {
			h.publish(protocol.EventAutopilotCreated, workspaceID, "member", userID, map[string]any{
				"autopilot": autopilotToResponse(*ap, nil),
			})
		}
	}
}
