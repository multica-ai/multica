package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
)

// Restricted-base access and per-base settings (plan 10.13, 10.11). Owners and
// admins only: the router gates these routes, and requireKnowledgeAdmin keeps
// agents out as a backstop.

// writeKnowledgeAdminError answers a failure of an admin action on the platform
// side of knowledge, and falls back to the general knowledge mapping.
func writeKnowledgeAdminError(w http.ResponseWriter, r *http.Request, workspaceID string, err error) {
	switch {
	case errors.Is(err, service.ErrKnowledgeSubjectNotFound):
		writeError(w, http.StatusNotFound, "the squad, floor or agent is not in this workspace")
	case errors.Is(err, service.ErrKnowledgeLLMNotConfigured):
		writeError(w, http.StatusConflict, "no chat model is configured for knowledge on this deployment")
	case errors.Is(err, service.ErrKnowledgeOwnerLoginMissing):
		writeError(w, http.StatusNotFound, "no knowledge login exists for you yet; enable knowledge again")
	case errors.Is(err, service.ErrKnowledgeOwnerPasswordShown):
		writeError(w, http.StatusConflict, "the knowledge password was already shown")
	case errors.Is(err, service.ErrKnowledgeDeprovisionNotHeld):
		writeError(w, http.StatusNotFound, "no knowledge tenant is held for this workspace")
	default:
		writeKnowledgeError(w, r, workspaceID, err)
	}
}

type knowledgeVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

type knowledgeGrantRequest struct {
	Access string `json:"access"`
}

type knowledgeSettingsRequest struct {
	Wiki      *bool `json:"wiki_enabled"`
	Summaries *bool `json:"summaries_enabled"`
}

// knowledgeCurator reports whether a request acts as a person who curates every
// base: an owner or admin. An agent is never a curator, even when it runs on an
// owner's token; its reach is its grants.
func (h *Handler) knowledgeCurator(r *http.Request, actorType string) bool {
	if actorType != service.KnowledgeActorMember {
		return false
	}
	m, ok := ctxMember(r.Context())
	return ok && (m.Role == "owner" || m.Role == "admin")
}

// knowledgeReadActor is who a knowledge read is for: the agent or the person
// making the request.
func (h *Handler) knowledgeReadActor(r *http.Request, workspaceID string) service.KnowledgeActor {
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	actor := service.KnowledgeActor{Type: actorType, Curator: h.knowledgeCurator(r, actorType)}
	if id, err := util.ParseUUID(actorID); err == nil {
		actor.ID = id
	}
	return actor
}

// recomputeKnowledgeAccess updates an agent's bindings to the restricted bases
// after its squad membership changed. It runs after the membership commits; a
// failure is logged, and the next membership change or enable recomputes it.
func (h *Handler) recomputeKnowledgeAccess(r *http.Request, workspaceID, agentID pgtype.UUID) {
	if h.Knowledge == nil {
		return
	}
	if err := h.Knowledge.RecomputeAgentKnowledgeAccess(r.Context(), workspaceID, agentID); err != nil {
		slog.Warn("knowledge: agent access not recomputed after a membership change", append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(workspaceID), "agent_id", uuidToString(agentID))...)
	}
}

// recomputeSquadKnowledgeAccess updates every agent of a squad after the squad
// moved floors or was archived: both change which floor grants reach it.
func (h *Handler) recomputeSquadKnowledgeAccess(r *http.Request, workspaceID, squadID pgtype.UUID) {
	if h.Knowledge == nil {
		return
	}
	agents, err := h.Queries.ListLiveAgentsForKnowledgeMember(r.Context(), squadID)
	if err != nil {
		slog.Warn("knowledge: squad agents not listed for access recompute", append(logger.RequestAttrs(r), "error", err, "squad_id", uuidToString(squadID))...)
		return
	}
	for _, agent := range agents {
		h.recomputeKnowledgeAccess(r, workspaceID, agent)
	}
}

// adminKnowledgeBaseKey resolves the {key} of an admin action on a base.
func (h *Handler) adminKnowledgeBaseKey(w http.ResponseWriter, r *http.Request) (workspaceID, key string, ok bool) {
	workspaceID = h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return "", "", false
	}
	key = chi.URLParam(r, "key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "knowledge base key is required")
		return "", "", false
	}
	return workspaceID, key, true
}

// GetKnowledgeBaseAccess (GET /api/knowledge/bases/{key}/access) returns a
// base's visibility and grants.
func (h *Handler) GetKnowledgeBaseAccess(w http.ResponseWriter, r *http.Request) {
	workspaceID, key, ok := h.adminKnowledgeBaseKey(w, r)
	if !ok {
		return
	}
	view, err := h.Knowledge.KnowledgeAccess(r.Context(), parseUUID(workspaceID), key)
	if err != nil {
		writeKnowledgeAdminError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// SetKnowledgeBaseVisibility (PUT /api/knowledge/bases/{key}/visibility) makes a
// base restricted or workspace-visible.
func (h *Handler) SetKnowledgeBaseVisibility(w http.ResponseWriter, r *http.Request) {
	workspaceID, key, ok := h.adminKnowledgeBaseKey(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var req knowledgeVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	view, err := h.Knowledge.SetKnowledgeVisibility(r.Context(), parseUUID(workspaceID), actorUUID, key, req.Visibility)
	if err != nil {
		writeKnowledgeAdminError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// PutKnowledgeGrant (PUT /api/knowledge/bases/{key}/access/{subject_type}/{subject_id})
// gives a squad, floor or agent read or write access to a restricted base.
func (h *Handler) PutKnowledgeGrant(w http.ResponseWriter, r *http.Request) {
	workspaceID, key, ok := h.adminKnowledgeBaseKey(w, r)
	if !ok {
		return
	}
	var req knowledgeGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	view, err := h.Knowledge.SetKnowledgeGrant(r.Context(), parseUUID(workspaceID), key,
		chi.URLParam(r, "subject_type"), chi.URLParam(r, "subject_id"), req.Access)
	if err != nil {
		writeKnowledgeAdminError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// DeleteKnowledgeGrant (DELETE /api/knowledge/bases/{key}/access/{subject_type}/{subject_id})
// withdraws a grant.
func (h *Handler) DeleteKnowledgeGrant(w http.ResponseWriter, r *http.Request) {
	workspaceID, key, ok := h.adminKnowledgeBaseKey(w, r)
	if !ok {
		return
	}
	view, err := h.Knowledge.RemoveKnowledgeGrant(r.Context(), parseUUID(workspaceID), key,
		chi.URLParam(r, "subject_type"), chi.URLParam(r, "subject_id"))
	if err != nil {
		writeKnowledgeAdminError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// PatchKnowledgeBaseSettings (PATCH /api/knowledge/bases/{key}/settings) turns
// Wiki or document summaries on or off for one base. Wiki is limited to three
// bases.
func (h *Handler) PatchKnowledgeBaseSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, key, ok := h.adminKnowledgeBaseKey(w, r)
	if !ok {
		return
	}
	var req knowledgeSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	view, err := h.Knowledge.UpdateBaseSettings(r.Context(), parseUUID(workspaceID), key, service.KnowledgeBaseSettingsPatch{
		Wiki: req.Wiki, Summaries: req.Summaries,
	})
	var conflict *service.KnowledgeConflictError
	if errors.As(err, &conflict) {
		writeError(w, http.StatusConflict, conflict.Message)
		return
	}
	if err != nil {
		writeKnowledgeAdminError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
