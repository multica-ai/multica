package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

// Squad autonomy levels (spec 4.3). Reads are open to every member; a change is
// for owners and admins, and an agent never changes a level.

// SquadAutonomyResponse is a squad's level and the reason of its last change.
type SquadAutonomyResponse struct {
	SquadID   string  `json:"squad_id"`
	Level     int     `json:"level"`
	LevelName string  `json:"level_name"`
	Reason    string  `json:"reason"`
	ChangedAt *string `json:"changed_at"`
}

// SetSquadAutonomyRequest changes a squad's level. Level is required; Reason says
// why, for the governance log.
type SetSquadAutonomyRequest struct {
	Level  *int   `json:"level"`
	Reason string `json:"reason"`
}

func squadAutonomyResponse(v service.AutonomyView) SquadAutonomyResponse {
	return SquadAutonomyResponse{
		SquadID:   uuidToString(v.SquadID),
		Level:     v.Level,
		LevelName: service.AutonomyLevelName(v.Level),
		Reason:    v.Reason,
		ChangedAt: timePtrRFC3339(v.ChangedAt),
	}
}

func timePtrRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// governanceBossFor resolves who changes a governance setting: an owner or admin
// member. An agent, and any other member, is refused.
func (h *Handler) governanceBossFor(w http.ResponseWriter, r *http.Request, workspaceID string) (service.GovernanceActor, bool) {
	c, ok := h.initiativeCallerFor(w, r, workspaceID)
	if !ok {
		return service.GovernanceActor{}, false
	}
	if c.actor.Type != service.GovernanceActorMember || !c.admin {
		writeError(w, http.StatusForbidden, "only the owner or an admin can change this")
		return service.GovernanceActor{}, false
	}
	return service.GovernanceActor{Type: service.GovernanceActorMember, ID: c.actor.ID}, true
}

// autonomyService builds the service for one request.
func (h *Handler) autonomyService() *service.AutonomyService {
	return &service.AutonomyService{Queries: h.Queries, TxStarter: h.TxStarter}
}

// GetSquadAutonomy returns a squad's autonomy level.
func (h *Handler) GetSquadAutonomy(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	squadID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "squad id")
	if !ok {
		return
	}
	view, err := h.autonomyService().Get(r.Context(), wsUUID, squadID)
	if errors.Is(err, service.ErrAutonomySquadNotFound) {
		writeError(w, http.StatusNotFound, "squad not found")
		return
	}
	if err != nil {
		slog.Warn("read squad autonomy failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to read the autonomy level")
		return
	}
	writeJSON(w, http.StatusOK, squadAutonomyResponse(view))
}

// SetSquadAutonomy changes a squad's autonomy level. Owners and admins only.
func (h *Handler) SetSquadAutonomy(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	actor, ok := h.governanceBossFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	squadID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "squad id")
	if !ok {
		return
	}
	var req SetSquadAutonomyRequest
	if !decodeInitiativeBody(w, r, &req) {
		return
	}
	if req.Level == nil {
		writeError(w, http.StatusBadRequest, "level is required: 0, 1, 2 or 3")
		return
	}
	view, err := h.autonomyService().Set(r.Context(), wsUUID, squadID, *req.Level, actor, req.Reason)
	switch {
	case errors.Is(err, service.ErrAutonomyLevelInvalid), errors.Is(err, service.ErrAutonomyReasonRequired):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, service.ErrAutonomySquadNotFound):
		writeError(w, http.StatusNotFound, "squad not found")
		return
	case errors.Is(err, service.ErrGovernanceActorNotBoss):
		writeError(w, http.StatusForbidden, err.Error())
		return
	case err != nil:
		slog.Warn("set squad autonomy failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to change the autonomy level")
		return
	}
	writeJSON(w, http.StatusOK, squadAutonomyResponse(view))
}
