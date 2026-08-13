package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type pluginInstallationResponse struct {
	ID                string   `json:"id"`
	PluginKey         string   `json:"plugin_key"`
	DisplayName       string   `json:"display_name"`
	DesiredVersion    string   `json:"desired_version"`
	ActiveVersion     string   `json:"active_version,omitempty"`
	Enabled           bool     `json:"enabled"`
	DesiredGeneration int64    `json:"desired_generation"`
	ActiveGeneration  int64    `json:"active_generation"`
	LifecycleStatus   string   `json:"lifecycle_status"`
	HealthState       string   `json:"health_state,omitempty"`
	HealthReason      string   `json:"health_reason,omitempty"`
	Contributions     []string `json:"contributions"`
}

func (h *Handler) pluginInstallationResponse(r *http.Request, installation db.PluginInstallation, health *db.PluginHealth) (pluginInstallationResponse, error) {
	identity, err := h.Queries.GetPluginIdentity(r.Context(), installation.PluginID)
	if err != nil {
		return pluginInstallationResponse{}, err
	}
	desired, err := h.Queries.GetPluginRelease(r.Context(), installation.DesiredReleaseID)
	if err != nil {
		return pluginInstallationResponse{}, err
	}
	response := pluginInstallationResponse{
		ID:                uuidToString(installation.ID),
		PluginKey:         identity.PluginKey,
		DisplayName:       identity.DisplayName,
		DesiredVersion:    desired.Version,
		Enabled:           installation.Enabled,
		DesiredGeneration: installation.DesiredGeneration,
		ActiveGeneration:  installation.ActiveGeneration,
		LifecycleStatus:   installation.LifecycleStatus,
		Contributions:     []string{},
	}
	if contributions, err := h.Queries.ListPluginContributionsByRelease(r.Context(), desired.ID); err == nil {
		for _, contribution := range contributions {
			response.Contributions = append(response.Contributions, contribution.ContributionKey)
		}
	}
	if installation.ActiveReleaseID.Valid {
		if active, err := h.Queries.GetPluginRelease(r.Context(), installation.ActiveReleaseID); err == nil {
			response.ActiveVersion = active.Version
		}
	}
	if health != nil {
		response.HealthState = health.State
		response.HealthReason = health.ReasonCode
	}
	return response, nil
}

func (h *Handler) ListPlugins(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	installations, err := h.Queries.ListWorkspacePluginInstallations(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list plugins")
		return
	}
	healthRows, _ := h.Queries.ListWorkspacePluginHealth(r.Context(), workspaceID)
	healthByInstallation := make(map[string]db.PluginHealth)
	for _, health := range healthRows {
		key := uuidToString(health.InstallationID)
		if _, exists := healthByInstallation[key]; !exists {
			healthByInstallation[key] = health
		}
	}
	responses := make([]pluginInstallationResponse, 0, len(installations))
	for _, installation := range installations {
		health, hasHealth := healthByInstallation[uuidToString(installation.ID)]
		var healthPtr *db.PluginHealth
		if hasHealth {
			healthPtr = &health
		}
		response, err := h.pluginInstallationResponse(r, installation, healthPtr)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load plugin")
			return
		}
		responses = append(responses, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plugins": responses,
	})
}

type pluginBindingRequest struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
}

func (h *Handler) EnablePlugin(w http.ResponseWriter, r *http.Request) {
	h.setPluginEnabled(w, r, true)
}

func (h *Handler) DisablePlugin(w http.ResponseWriter, r *http.Request) {
	h.setPluginEnabled(w, r, false)
}

func (h *Handler) setPluginEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	workspaceID, actorID, ok := pluginRequestIDs(w, r)
	if !ok {
		return
	}
	installationID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation_id")
	if !ok {
		return
	}
	request := pluginBindingRequest{ScopeType: "workspace", ScopeID: uuidToString(workspaceID)}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if request.ScopeType == "workspace" && request.ScopeID == "" {
		request.ScopeID = uuidToString(workspaceID)
	}
	scopeID, err := util.ParseUUID(request.ScopeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "scope_id must be a UUID")
		return
	}
	var installation db.PluginInstallation
	if enabled {
		installation, err = h.PluginService.EnablePlugin(r.Context(), workspaceID, installationID, actorID, request.ScopeType, scopeID)
	} else {
		installation, err = h.PluginService.DisablePlugin(r.Context(), workspaceID, installationID, actorID, request.ScopeType, scopeID)
	}
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	response, err := h.pluginInstallationResponse(r, installation, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load plugin")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) RollbackPlugin(w http.ResponseWriter, r *http.Request) {
	workspaceID, actorID, ok := pluginRequestIDs(w, r)
	if !ok {
		return
	}
	installationID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation_id")
	if !ok {
		return
	}
	var request struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Version == "" {
		writeError(w, http.StatusBadRequest, "version is required")
		return
	}
	installation, err := h.PluginService.RollbackPlugin(r.Context(), workspaceID, installationID, actorID, request.Version)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	response, err := h.pluginInstallationResponse(r, installation, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load plugin")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func pluginRequestIDs(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	actorID, err := util.ParseUUID(requestUserID(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return workspaceID, actorID, true
}
