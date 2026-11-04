package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// officePulseCache keeps each workspace's pulse for a minute (spec 4.8), so the
// Office header costs one read a minute however many people have it open.
var officePulseCache = service.NewPulseCache(service.PulseTTL)

// GetOfficePulse returns the company at a glance for the Office header strip.
//
//	GET /api/office/pulse
func (h *Handler) GetOfficePulse(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	pulse, err := officePulseCache.Get(r.Context(), workspaceID, func(ctx context.Context) (service.Pulse, error) {
		return (&service.PulseService{
			Queries: h.Queries,
			Goals:   service.NewGoalService(h.Queries, h.TxStarter),
			Budgets: h.budgetService(),
		}).Build(ctx, wsUUID)
	})
	if err != nil {
		slog.Warn("read office pulse failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to read the office pulse")
		return
	}
	writeJSON(w, http.StatusOK, pulse)
}

// SetFloorChiefRequest names the agent that chairs a floor.
type SetFloorChiefRequest struct {
	AgentID string `json:"agent_id"`
}

// FloorChiefResponse is a floor's chief.
type FloorChiefResponse struct {
	FloorID   string `json:"floor_id"`
	AgentID   string `json:"agent_id"`
	UpdatedAt string `json:"updated_at"`
}

// GetFloorChief returns a floor's chief, or 404 when it has none.
//
//	GET /api/office/floors/{id}/chief
func (h *Handler) GetFloorChief(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	floorID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "floor id")
	if !ok {
		return
	}
	row, err := h.Queries.GetFloorChief(r.Context(), db.GetFloorChiefParams{WorkspaceID: wsUUID, FloorID: floorID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "this floor has no chief")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the floor's chief")
		return
	}
	writeJSON(w, http.StatusOK, floorChiefResponse(row))
}

// SetFloorChief names the agent that chairs a floor. Owners and admins only; the
// agent must be a live agent of the workspace.
//
//	PUT /api/office/floors/{id}/chief {agent_id}
func (h *Handler) SetFloorChief(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	actor, ok := h.companyActorFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	floorID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "floor id")
	if !ok {
		return
	}
	var req SetFloorChiefRequest
	if !decodeInitiativeBody(w, r, &req) {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetOfficeFloorByID(r.Context(), db.GetOfficeFloorByIDParams{ID: floorID, WorkspaceID: wsUUID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "floor not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read the floor")
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: wsUUID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the agent")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "an archived agent cannot chair a floor")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set the floor's chief")
		return
	}
	defer tx.Rollback(r.Context())
	row, err := service.SetFloorChiefIn(r.Context(), h.Queries.WithTx(tx), service.FloorChiefInput{
		WorkspaceID: wsUUID, FloorID: floorID, AgentID: agentID,
		ActorUserID: actor.ID, RoutineStatus: "active",
	})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		slog.Warn("set floor chief failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to set the floor's chief")
		return
	}
	writeJSON(w, http.StatusOK, floorChiefResponse(row))
}

// ClearFloorChief removes a floor's chief. The floor's briefs stay, and the
// chief's floor brief routine is paused.
//
//	DELETE /api/office/floors/{id}/chief
func (h *Handler) ClearFloorChief(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.companyActorFor(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	floorID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "floor id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear the floor's chief")
		return
	}
	defer tx.Rollback(r.Context())
	if err := service.ClearFloorChiefIn(r.Context(), h.Queries.WithTx(tx), wsUUID, floorID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear the floor's chief")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear the floor's chief")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DigestModeResponse is the Secretary's digest source: the setting, the number
// of active squads it is judged on, and the source it resolves to.
type DigestModeResponse struct {
	Setting      string `json:"setting"`
	ActiveSquads int    `json:"active_squads"`
	Resolved     string `json:"resolved"`
	// AutoFloorThreshold is the squad count above which auto reads floor briefs.
	AutoFloorThreshold int `json:"auto_floor_threshold"`
}

// SetDigestModeRequest changes the setting.
type SetDigestModeRequest struct {
	Mode string `json:"mode"`
}

func (h *Handler) digestModeOf(ctx context.Context, wsUUID pgtype.UUID) (DigestModeResponse, error) {
	setting, err := h.Queries.GetWorkspaceDigestMode(ctx, wsUUID)
	if err != nil {
		return DigestModeResponse{}, err
	}
	n, err := h.Queries.CountLiveSquads(ctx, wsUUID)
	if err != nil {
		return DigestModeResponse{}, err
	}
	return DigestModeResponse{
		Setting: setting, ActiveSquads: int(n),
		Resolved:           service.ResolveDigestMode(setting, int(n)),
		AutoFloorThreshold: service.DigestAutoFloorThreshold,
	}, nil
}

// GetDigestMode returns the digest source setting and what it resolves to now.
//
//	GET /api/office/digest-mode
func (h *Handler) GetDigestMode(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	out, err := h.digestModeOf(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the digest mode")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// SetDigestMode changes the digest source: auto, squads or floors. Owners and
// admins only.
//
//	PUT /api/office/digest-mode {mode}
func (h *Handler) SetDigestMode(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.companyActorFor(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req SetDigestModeRequest
	if !decodeInitiativeBody(w, r, &req) {
		return
	}
	switch req.Mode {
	case service.DigestModeAuto, service.DigestModeSquads, service.DigestModeFloors:
	default:
		writeError(w, http.StatusBadRequest, "mode must be auto, squads or floors")
		return
	}
	if _, err := h.Queries.SetWorkspaceDigestMode(r.Context(), db.SetWorkspaceDigestModeParams{WorkspaceID: wsUUID, DigestMode: req.Mode}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set the digest mode")
		return
	}
	out, err := h.digestModeOf(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the digest mode")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func floorChiefResponse(c db.FloorChief) FloorChiefResponse {
	return FloorChiefResponse{
		FloorID:   uuidToString(c.FloorID),
		AgentID:   uuidToString(c.AgentID),
		UpdatedAt: c.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}
