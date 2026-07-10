// CEREBRO-PATCH(agent-memory-settings-route): FIR-1794 — Gate 3's HTTP route.
//
// This is the third and last layer of the Cognee memory access model (locked
// spec 29/6, see docs/agents/permission-system.md row 6a):
//
//  1. workspace feature flag `cerebro_memory` (default OFF)
//  2. group capability `create_memory` (default deny)
//  3. per-(user, agent) read/write toggle — THIS FILE
//
// GET returns the caller's own switches for one agent (harmless to read even
// without create_memory — both default false, so there is nothing to leak).
// PUT requires create_memory: flipping a switch on is "using memory", and the
// same capability already gates enabling/writing memory everywhere else.
package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/agentmemory"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// AgentMemorySettingsService is the seam agentmemory.Service satisfies
// directly (structural typing — no adapter needed, unlike GroupPermissions
// which crosses a would-be import cycle). Declared here so Handler can hold
// the field without the router needing anything beyond a plain assignment.
type AgentMemorySettingsService interface {
	GetSettings(ctx context.Context, userID, agentID pgtype.UUID) (agentmemory.Settings, error)
	SetSettings(ctx context.Context, workspaceID, userID, agentID pgtype.UUID, next agentmemory.Settings) (agentmemory.Settings, error)
}

// agentMemorySettingsResponse is the body both GET and PUT return.
type agentMemorySettingsResponse struct {
	AgentID        string `json:"agent_id"`
	CanReadMemory  bool   `json:"can_read_memory"`
	CanWriteMemory bool   `json:"can_write_memory"`
}

type setAgentMemorySettingsRequest struct {
	CanReadMemory  bool `json:"can_read_memory"`
	CanWriteMemory bool `json:"can_write_memory"`
}

// cerebroMemoryEnabled reports whether the workspace flag cerebro_memory is
// on. Default OFF: a missing row or any lookup failure resolves to false —
// same fail-closed shape as cerebroAgentVaultEnabled.
func (h *Handler) cerebroMemoryEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if h.CerebroQueries == nil {
		return false
	}
	rows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.FlagKey == "cerebro_memory" {
			return row.Enabled
		}
	}
	return false
}

// GetAgentMemorySettings handles GET /api/agents/{id}/memory-settings. Returns
// the caller's own can_read_memory/can_write_memory for this agent — a missing
// row (never toggled on) reads as {false, false} via agentmemory.Service.
func (h *Handler) GetAgentMemorySettings(w http.ResponseWriter, r *http.Request) {
	if h.AgentMemory == nil {
		writeError(w, http.StatusNotFound, "memory is not available on this deployment")
		return
	}
	if _, ok := h.requireCerebroMemoryEnabled(w, r); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "agent id")
	if !ok {
		return
	}
	settings, err := h.AgentMemory.GetSettings(r.Context(), userUUID, agentUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load memory settings")
		return
	}
	writeJSON(w, http.StatusOK, agentMemorySettingsResponse{
		AgentID:        util.UUIDToString(agentUUID),
		CanReadMemory:  settings.CanReadMemory,
		CanWriteMemory: settings.CanWriteMemory,
	})
}

// SetAgentMemorySettings handles PUT /api/agents/{id}/memory-settings. Gated
// on create_memory in addition to the workspace flag — turning a switch on is
// "using memory", so the same capability that gates writing memory gates this.
func (h *Handler) SetAgentMemorySettings(w http.ResponseWriter, r *http.Request) {
	if h.AgentMemory == nil {
		writeError(w, http.StatusNotFound, "memory is not available on this deployment")
		return
	}
	workspaceIDStr := middleware.WorkspaceIDFromContext(r.Context())
	workspaceID, ok := h.requireCerebroMemoryEnabled(w, r)
	if !ok {
		return
	}
	if !h.cerebroRequireCapability(w, r, workspaceIDStr, "create_memory") {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "agent id")
	if !ok {
		return
	}
	var req setAgentMemorySettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	settings, err := h.AgentMemory.SetSettings(r.Context(), workspaceID, userUUID, agentUUID, agentmemory.Settings{
		CanReadMemory:  req.CanReadMemory,
		CanWriteMemory: req.CanWriteMemory,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update memory settings")
		return
	}
	writeJSON(w, http.StatusOK, agentMemorySettingsResponse{
		AgentID:        util.UUIDToString(agentUUID),
		CanReadMemory:  settings.CanReadMemory,
		CanWriteMemory: settings.CanWriteMemory,
	})
}

// requireCerebroMemoryEnabled resolves + validates the request's workspace id
// and confirms the cerebro_memory flag is on, writing the appropriate HTTP
// error and returning ok=false otherwise. Shared by both handlers above.
func (h *Handler) requireCerebroMemoryEnabled(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	workspaceIDStr := middleware.WorkspaceIDFromContext(r.Context())
	workspaceUUID, err := util.ParseUUID(workspaceIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	if !h.cerebroMemoryEnabled(r.Context(), workspaceUUID) {
		writeError(w, http.StatusNotFound, "memory is not enabled for this workspace")
		return pgtype.UUID{}, false
	}
	return workspaceUUID, true
}

// cerebroMemoryToolStates resolves the three memory gates for the calling user
// and this agent, mirroring runtime.CerebroMemoryToolsForTask (which cannot be
// imported here — the runtime package depends on handler). recall follows the
// workspace flag alone (company memory is readable by every agent once the
// feature is on); write additionally needs the create_memory capability and the
// per-(user × agent) write switch. Fail closed on every error path.
//
// CEREBRO-PATCH(memory-tools-offer): FIR-1794.
func (h *Handler) cerebroMemoryToolStates(r *http.Request, agentID, workspaceID pgtype.UUID) (recall, write bool) {
	ctx := r.Context()
	if !h.cerebroMemoryEnabled(ctx, workspaceID) {
		return false, false
	}
	recall = true
	if h.CerebroQueries == nil || h.AgentMemory == nil {
		return recall, false
	}
	userUUID, err := util.ParseUUID(r.Header.Get("X-User-ID"))
	if err != nil {
		return recall, false
	}
	has, err := h.CerebroQueries.HasCerebroCapability(ctx, cerebrodb.HasCerebroCapabilityParams{
		WorkspaceID: workspaceID,
		UserID:      userUUID,
		Capability:  "create_memory",
	})
	if err != nil || !has {
		return recall, false
	}
	settings, err := h.AgentMemory.GetSettings(ctx, userUUID, agentID)
	if err != nil {
		return recall, false
	}
	return recall, settings.CanWriteMemory
}
