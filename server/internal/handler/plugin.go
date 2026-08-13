package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/pluginbundled"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

type pluginBindingResponse struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
	Enabled   bool   `json:"enabled"`
	Revision  int64  `json:"revision"`
}

type pluginInstallationResponse struct {
	ID                string                  `json:"id"`
	PluginKey         string                  `json:"plugin_key"`
	DisplayName       string                  `json:"display_name"`
	DesiredVersion    string                  `json:"desired_version"`
	ActiveVersion     string                  `json:"active_version,omitempty"`
	Enabled           bool                    `json:"enabled"`
	DesiredGeneration int64                   `json:"desired_generation"`
	ActiveGeneration  int64                   `json:"active_generation"`
	LifecycleStatus   string                  `json:"lifecycle_status"`
	HealthState       string                  `json:"health_state,omitempty"`
	HealthReason      string                  `json:"health_reason,omitempty"`
	Contributions     []string                `json:"contributions"`
	Bindings          []pluginBindingResponse `json:"bindings"`
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
		Bindings:          []pluginBindingResponse{},
	}
	contributions, err := h.Queries.ListPluginContributionsByRelease(r.Context(), desired.ID)
	if err != nil {
		return pluginInstallationResponse{}, err
	}
	for _, contribution := range contributions {
		response.Contributions = append(response.Contributions, contribution.ContributionKey)
	}
	bindings, err := h.Queries.ListLatestPluginBindings(r.Context(), installation.ID)
	if err != nil {
		return pluginInstallationResponse{}, err
	}
	for _, binding := range bindings {
		response.Bindings = append(response.Bindings, pluginBindingResponse{
			ScopeType: binding.ScopeType,
			ScopeID:   uuidToString(binding.ScopeID),
			Enabled:   binding.Enabled,
			Revision:  binding.BindingRevision,
		})
	}
	if installation.ActiveReleaseID.Valid {
		active, err := h.Queries.GetPluginRelease(r.Context(), installation.ActiveReleaseID)
		if err != nil {
			return pluginInstallationResponse{}, err
		}
		response.ActiveVersion = active.Version
	}
	if health != nil {
		response.HealthState = health.State
		response.HealthReason = health.ReasonCode
	}
	return response, nil
}

func (h *Handler) ListPlugins(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	installations, err := h.Queries.ListWorkspacePluginInstallations(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list Plugins")
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
			writeError(w, http.StatusInternalServerError, "failed to load Plugin")
			return
		}
		responses = append(responses, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": responses})
}

type pluginCatalogContributionResponse struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	EntryPath   string `json:"entry_path"`
	EntryDigest string `json:"entry_digest"`
}

type pluginCatalogReleaseResponse struct {
	PluginKey              string                              `json:"plugin_key"`
	Name                   string                              `json:"name"`
	Description            string                              `json:"description"`
	Version                string                              `json:"version"`
	Publisher              string                              `json:"publisher"`
	PublisherType          string                              `json:"publisher_type"`
	TrustTier              string                              `json:"trust_tier"`
	SourceKind             string                              `json:"source_kind"`
	SourceRef              string                              `json:"source_ref"`
	RequestedCapabilities  []string                            `json:"requested_capabilities"`
	HostAPI                string                              `json:"host_api"`
	RequiredDaemonFeatures []string                            `json:"required_daemon_features"`
	SignatureKeyID         string                              `json:"signature_key_id"`
	SignatureVerified      bool                                `json:"signature_verified"`
	ManifestDigest         string                              `json:"manifest_digest"`
	ArchiveDigest          string                              `json:"archive_digest"`
	ArtifactDigest         string                              `json:"artifact_digest"`
	Compatible             bool                                `json:"compatible"`
	CompatibilityReason    string                              `json:"compatibility_reason,omitempty"`
	Contributions          []pluginCatalogContributionResponse `json:"contributions"`
	Installation           *pluginInstallationResponse         `json:"installation,omitempty"`
}

func (h *Handler) catalogReleaseResponse(r *http.Request, workspaceID pgtype.UUID, entry pluginbundled.CatalogEntry) (pluginCatalogReleaseResponse, error) {
	manifest := entry.Release.Manifest
	response := pluginCatalogReleaseResponse{
		PluginKey:              manifest.Metadata.Key,
		Name:                   manifest.Metadata.Name,
		Description:            manifest.Metadata.Description,
		Version:                manifest.Metadata.Version,
		Publisher:              manifest.Metadata.Publisher,
		PublisherType:          entry.PublisherType,
		TrustTier:              entry.TrustTier,
		SourceKind:             entry.Release.SourceKind,
		SourceRef:              entry.Release.SourceRef,
		RequestedCapabilities:  append([]string(nil), manifest.RequestedCapabilities...),
		HostAPI:                manifest.Compatibility.HostAPI,
		RequiredDaemonFeatures: append([]string(nil), manifest.Compatibility.RequiredDaemonFeatures...),
		SignatureKeyID:         entry.Release.SignatureKeyID,
		SignatureVerified:      true,
		ManifestDigest:         entry.Release.ManifestDigest,
		ArchiveDigest:          entry.Release.ArchiveDigest,
		ArtifactDigest:         entry.Release.ArtifactDigest,
		Compatible:             entry.Compatible,
		CompatibilityReason:    entry.CompatibilityWhy,
		Contributions:          make([]pluginCatalogContributionResponse, 0, len(entry.Release.Contributions)),
	}
	for _, contribution := range entry.Release.Contributions {
		response.Contributions = append(response.Contributions, pluginCatalogContributionResponse{
			Key:         contribution.Key,
			Type:        contribution.Type,
			Name:        contribution.DisplayName,
			Description: contribution.Description,
			EntryPath:   contribution.EntryPath,
			EntryDigest: contribution.EntryDigest,
		})
	}
	identity, err := h.Queries.GetPluginIdentityByKey(r.Context(), manifest.Metadata.Key)
	if errors.Is(err, pgx.ErrNoRows) {
		return response, nil
	}
	if err != nil {
		return pluginCatalogReleaseResponse{}, err
	}
	installation, err := h.Queries.GetWorkspacePluginInstallation(r.Context(), db.GetWorkspacePluginInstallationParams{
		WorkspaceID: workspaceID,
		PluginID:    identity.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return response, nil
	}
	if err != nil {
		return pluginCatalogReleaseResponse{}, err
	}
	installed, err := h.pluginInstallationResponse(r, installation, nil)
	if err != nil {
		return pluginCatalogReleaseResponse{}, err
	}
	response.Installation = &installed
	return response, nil
}

func (h *Handler) ListPluginCatalog(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	entries := h.PluginService.CatalogEntries()
	responses := make([]pluginCatalogReleaseResponse, 0, len(entries))
	for _, entry := range entries {
		response, err := h.catalogReleaseResponse(r, workspaceID, entry)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load Plugin catalog")
			return
		}
		responses = append(responses, response)
	}
	diagnostics := h.PluginService.CatalogDiagnostics()
	if diagnostics == nil {
		diagnostics = []pluginbundled.Diagnostic{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"releases":    responses,
		"diagnostics": diagnostics,
	})
}

func (h *Handler) GetPluginCatalogRelease(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	entry, found := h.PluginService.FindCatalogRelease(chi.URLParam(r, "pluginKey"), r.URL.Query().Get("version"))
	if !found {
		writeError(w, http.StatusNotFound, "Plugin release not found")
		return
	}
	response, err := h.catalogReleaseResponse(r, workspaceID, entry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Plugin catalog")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type pluginReleaseRequest struct {
	PluginKey string `json:"plugin_key"`
	Version   string `json:"version"`
}

func (h *Handler) InstallPlugin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	workspaceID, actorID, ok := pluginRequestIDs(w, r)
	if !ok {
		return
	}
	var request pluginReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.PluginKey == "" || request.Version == "" {
		writeError(w, http.StatusBadRequest, "plugin_key and version are required")
		return
	}
	installation, err := h.PluginService.InstallCatalogRelease(r.Context(), workspaceID, actorID, request.PluginKey, request.Version)
	if err != nil {
		writePluginError(w, r, err)
		return
	}
	response, err := h.pluginInstallationResponse(r, installation, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Plugin")
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) UpgradePlugin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	workspaceID, actorID, ok := pluginRequestIDs(w, r)
	if !ok {
		return
	}
	installationID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation_id")
	if !ok {
		return
	}
	var request pluginReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.PluginKey == "" || request.Version == "" {
		writeError(w, http.StatusBadRequest, "plugin_key and version are required")
		return
	}
	installation, err := h.PluginService.UpgradeCatalogRelease(r.Context(), workspaceID, installationID, actorID, request.PluginKey, request.Version)
	if err != nil {
		writePluginError(w, r, err)
		return
	}
	response, err := h.pluginInstallationResponse(r, installation, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Plugin")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type pluginBindingRequest struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
}

func (h *Handler) EnablePlugin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	h.setPluginEnabled(w, r, true)
}

func (h *Handler) DisablePlugin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
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
	scopeID, ok := parseUUIDOrBadRequest(w, request.ScopeID, "scope_id")
	if !ok {
		return
	}
	var installation db.PluginInstallation
	var err error
	if enabled {
		installation, err = h.PluginService.EnablePlugin(r.Context(), workspaceID, installationID, actorID, request.ScopeType, scopeID)
	} else {
		installation, err = h.PluginService.DisablePlugin(r.Context(), workspaceID, installationID, actorID, request.ScopeType, scopeID)
	}
	if err != nil {
		writePluginError(w, r, err)
		return
	}
	response, err := h.pluginInstallationResponse(r, installation, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Plugin")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) RollbackPlugin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
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
		writePluginError(w, r, err)
		return
	}
	response, err := h.pluginInstallationResponse(r, installation, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Plugin")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func writePluginError(w http.ResponseWriter, r *http.Request, err error) {
	var pluginErr *service.PluginError
	if !errors.As(err, &pluginErr) {
		slog.Error("Plugin request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "Plugin operation failed")
		return
	}
	status := http.StatusInternalServerError
	switch pluginErr.Kind {
	case service.PluginErrorInvalid:
		status = http.StatusBadRequest
	case service.PluginErrorNotFound:
		status = http.StatusNotFound
	case service.PluginErrorConflict:
		status = http.StatusConflict
	case service.PluginErrorIncompatible:
		status = http.StatusUnprocessableEntity
	default:
		slog.Error("Plugin request returned unknown classified error", "error", err)
		writeError(w, http.StatusInternalServerError, "Plugin operation failed")
		return
	}
	writeError(w, status, pluginErr.Message)
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
