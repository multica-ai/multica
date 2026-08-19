package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

func (h *Handler) pluginsV1Enabled(ctx context.Context) bool {
	return featureflags.PluginsV1Enabled(ctx, h.FeatureFlags)
}

func (h *Handler) requirePluginsV1(w http.ResponseWriter, r *http.Request) bool {
	if h.pluginsV1Enabled(r.Context()) {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "Plugin management is not enabled")
	return false
}

// writePluginError maps a service error onto a status. Kinds exist so the
// handler never has to string-match a message to pick a status code.
func writePluginError(w http.ResponseWriter, err error, fallback string) {
	var pluginErr *service.PluginError
	if !errors.As(err, &pluginErr) {
		writeError(w, http.StatusInternalServerError, fallback)
		return
	}
	switch pluginErr.Kind {
	case service.PluginErrorInvalid:
		writeError(w, http.StatusBadRequest, pluginErr.Message)
	case service.PluginErrorNotFound:
		writeError(w, http.StatusNotFound, pluginErr.Message)
	case service.PluginErrorConflict:
		writeError(w, http.StatusConflict, pluginErr.Message)
	case service.PluginErrorForbidden:
		writeError(w, http.StatusForbidden, pluginErr.Message)
	case service.PluginErrorIncompatible:
		writeError(w, http.StatusUnprocessableEntity, pluginErr.Message)
	case service.PluginErrorQuota:
		writeError(w, http.StatusInsufficientStorage, pluginErr.Message)
	default:
		writeError(w, http.StatusBadGateway, pluginErr.Message)
	}
}

// pluginInstallationResponse never carries a secret value. `config` holds only
// non-secret fields by construction (SetConfig routes secrets to their own
// encrypted table), and configured secrets appear as names in
// `configured_secrets`.
type pluginInstallationResponse struct {
	ID                string                      `json:"id"`
	PluginKey         string                      `json:"plugin_key"`
	Name              string                      `json:"name"`
	Description       string                      `json:"description,omitempty"`
	Version           string                      `json:"version"`
	SourceURL         string                      `json:"source_url"`
	Enabled           bool                        `json:"enabled"`
	GrantedScopes     []string                    `json:"granted_scopes"`
	ConfigSchema      []service.PluginConfigField `json:"config_schema"`
	Config            map[string]any              `json:"config"`
	ConfiguredSecrets []string                    `json:"configured_secrets"`
	Surfaces          []plugincontract.Surface    `json:"surfaces"`
	Hooks             []pluginHookResponse        `json:"hooks"`
	Resources         []plugincontract.Resource   `json:"resources"`
	CreatedAt         string                      `json:"created_at"`
	UpdatedAt         string                      `json:"updated_at"`
}

// pluginHookResponse omits input_schema: the settings page lists what a hook is
// and who may call it, and the raw JSON Schema is noise there.
type pluginHookResponse struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
	Events      []string `json:"events,omitempty"`
	Transport   string   `json:"transport"`
}

func (h *Handler) pluginInstallationPayload(ctx context.Context, installation db.PluginInstallation) (pluginInstallationResponse, error) {
	manifest, err := service.ParseInstallationManifest(installation)
	if err != nil {
		return pluginInstallationResponse{}, err
	}
	secrets, err := h.PluginService.ConfiguredSecretKeys(ctx, installation.ID)
	if err != nil {
		return pluginInstallationResponse{}, err
	}

	config := map[string]any{}
	if len(installation.Config) > 0 {
		_ = json.Unmarshal(installation.Config, &config)
	}
	var granted []string
	if len(installation.GrantedScopes) > 0 {
		_ = json.Unmarshal(installation.GrantedScopes, &granted)
	}
	if granted == nil {
		granted = []string{}
	}

	hooks := make([]pluginHookResponse, 0, len(manifest.Contributes.Hooks))
	for _, hook := range manifest.Contributes.Hooks {
		hooks = append(hooks, pluginHookResponse{
			Key:         hook.Key,
			Name:        hook.Name,
			Description: hook.Description,
			Triggers:    hook.Triggers,
			Events:      hook.Events,
			Transport:   hook.Transport.Type,
		})
	}
	surfaces := manifest.Contributes.Surfaces
	if surfaces == nil {
		surfaces = []plugincontract.Surface{}
	}
	resources := manifest.Contributes.Resources
	if resources == nil {
		resources = []plugincontract.Resource{}
	}

	return pluginInstallationResponse{
		ID:                uuidToString(installation.ID),
		PluginKey:         installation.PluginKey,
		Name:              manifest.Name,
		Description:       manifest.Description,
		Version:           installation.Version,
		SourceURL:         installation.SourceUrl,
		Enabled:           installation.Enabled,
		GrantedScopes:     granted,
		ConfigSchema:      service.ConfigFieldsForManifest(manifest),
		Config:            config,
		ConfiguredSecrets: secrets,
		Surfaces:          surfaces,
		Hooks:             hooks,
		Resources:         resources,
		CreatedAt:         installation.CreatedAt.Time.UTC().Format(timeFormatRFC3339),
		UpdatedAt:         installation.UpdatedAt.Time.UTC().Format(timeFormatRFC3339),
	}, nil
}

const timeFormatRFC3339 = "2006-01-02T15:04:05Z07:00"

// ListPlugins — GET /api/workspaces/{id}/plugins
func (h *Handler) ListPlugins(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, workspaceIDFromURL(r, "id"), "workspace_id")
	if !ok {
		return
	}
	installations, err := h.Queries.ListWorkspacePluginInstallations(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list Plugins")
		return
	}
	payloads := make([]pluginInstallationResponse, 0, len(installations))
	for _, installation := range installations {
		payload, err := h.pluginInstallationPayload(r.Context(), installation)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list Plugins")
			return
		}
		payloads = append(payloads, payload)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": payloads})
}

type previewPluginRequest struct {
	SourceURL string `json:"source_url"`
}

// PreviewPlugin — POST /api/workspaces/{id}/plugins/preview
//
// Step one of the two-step install. Nothing is written: the response exists so
// the administrator can read the scope list before consenting.
func (h *Handler) PreviewPlugin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, workspaceIDFromURL(r, "id"), "workspace_id")
	if !ok {
		return
	}
	var req previewPluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	preview, err := h.PluginService.PreviewPlugin(r.Context(), workspaceID, req.SourceURL)
	if err != nil {
		writePluginError(w, err, "failed to read the Plugin manifest")
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

type installPluginRequest struct {
	SourceURL     string   `json:"source_url"`
	GrantedScopes []string `json:"granted_scopes"`
}

// InstallPlugin — POST /api/workspaces/{id}/plugins
func (h *Handler) InstallPlugin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	workspaceIDString := workspaceIDFromURL(r, "id")
	workspaceID, ok := parseUUIDOrBadRequest(w, workspaceIDString, "workspace_id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceIDString)
	if !ok {
		return
	}
	var req installPluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	installation, err := h.PluginService.InstallPlugin(r.Context(), workspaceID, member.UserID, req.SourceURL, req.GrantedScopes)
	if err != nil {
		writePluginError(w, err, "failed to install the Plugin")
		return
	}
	payload, err := h.pluginInstallationPayload(r.Context(), installation)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to install the Plugin")
		return
	}
	writeJSON(w, http.StatusCreated, payload)
}

type configurePluginRequest struct {
	Values map[string]any `json:"values"`
}

// ConfigurePlugin — PUT /api/workspaces/{id}/plugins/{installationId}/config
func (h *Handler) ConfigurePlugin(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.pluginInstallationFromURL(w, r)
	if !ok {
		return
	}
	var req configurePluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.PluginService.SetConfig(r.Context(), installation, req.Values)
	if err != nil {
		writePluginError(w, err, "failed to configure the Plugin")
		return
	}
	payload, err := h.pluginInstallationPayload(r.Context(), updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to configure the Plugin")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// EnablePlugin — POST /api/workspaces/{id}/plugins/{installationId}/enable
func (h *Handler) EnablePlugin(w http.ResponseWriter, r *http.Request) {
	h.setPluginEnabled(w, r, true)
}

// DisablePlugin — POST /api/workspaces/{id}/plugins/{installationId}/disable
func (h *Handler) DisablePlugin(w http.ResponseWriter, r *http.Request) {
	h.setPluginEnabled(w, r, false)
}

func (h *Handler) setPluginEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	installation, ok := h.pluginInstallationFromURL(w, r)
	if !ok {
		return
	}
	updated, err := h.PluginService.SetEnabled(r.Context(), installation, enabled)
	if err != nil {
		writePluginError(w, err, "failed to update the Plugin")
		return
	}
	payload, err := h.pluginInstallationPayload(r.Context(), updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the Plugin")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// UninstallPlugin — DELETE /api/workspaces/{id}/plugins/{installationId}
func (h *Handler) UninstallPlugin(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.pluginInstallationFromURL(w, r)
	if !ok {
		return
	}
	if err := h.PluginService.Uninstall(r.Context(), installation); err != nil {
		writePluginError(w, err, "failed to uninstall the Plugin")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) pluginInstallationFromURL(w http.ResponseWriter, r *http.Request) (db.PluginInstallation, bool) {
	if !h.requirePluginsV1(w, r) {
		return db.PluginInstallation{}, false
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, workspaceIDFromURL(r, "id"), "workspace_id")
	if !ok {
		return db.PluginInstallation{}, false
	}
	installation, err := h.PluginService.InstallationForWorkspace(r.Context(), workspaceID, chi.URLParam(r, "installationId"))
	if err != nil {
		writePluginError(w, err, "failed to load the Plugin")
		return db.PluginInstallation{}, false
	}
	return installation, true
}
