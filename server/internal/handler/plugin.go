package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func (h *Handler) privatePluginsV1Enabled(ctx context.Context) bool {
	return featureflags.PrivatePluginsV1Enabled(ctx, h.FeatureFlags)
}

func (h *Handler) requirePrivatePluginsV1(w http.ResponseWriter, r *http.Request) bool {
	if h.privatePluginsV1Enabled(r.Context()) {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "Private Plugin management is not enabled")
	return false
}

type pluginBindingResponse struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
	Enabled   bool   `json:"enabled"`
	Revision  int64  `json:"revision"`
}

type pluginContributionResponse struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	EntryPath   string `json:"entry_path"`
	EntryDigest string `json:"entry_digest"`
}

type pluginInstallationResponse struct {
	ID                string                       `json:"id"`
	PluginKey         string                       `json:"plugin_key"`
	DisplayName       string                       `json:"display_name"`
	DesiredVersion    string                       `json:"desired_version"`
	ActiveVersion     string                       `json:"active_version,omitempty"`
	Enabled           bool                         `json:"enabled"`
	DesiredGeneration int64                        `json:"desired_generation"`
	ActiveGeneration  int64                        `json:"active_generation"`
	LifecycleStatus   string                       `json:"lifecycle_status"`
	HealthState       string                       `json:"health_state,omitempty"`
	HealthReason      string                       `json:"health_reason,omitempty"`
	Description       string                       `json:"description,omitempty"`
	Publisher         string                       `json:"publisher"`
	PublisherType     string                       `json:"publisher_type"`
	TrustTier         string                       `json:"trust_tier"`
	SourceKind        string                       `json:"source_kind"`
	SourceRef         string                       `json:"source_ref"`
	UploaderID        string                       `json:"uploader_id,omitempty"`
	ManifestDigest    string                       `json:"manifest_digest"`
	ArchiveDigest     string                       `json:"archive_digest"`
	ArtifactDigest    string                       `json:"artifact_digest"`
	SignatureVerified bool                         `json:"signature_verified"`
	RequestedCaps     []string                     `json:"requested_capabilities"`
	AvailableVersions []string                     `json:"available_versions"`
	Contributions     []string                     `json:"contributions"`
	ContributionInfo  []pluginContributionResponse `json:"contribution_details"`
	Bindings          []pluginBindingResponse      `json:"bindings"`
}

func (h *Handler) pluginInstallationResponse(r *http.Request, installation db.PluginInstallation, health *db.PluginHealth) (pluginInstallationResponse, error) {
	return h.pluginInstallationResponseWithReleases(r, installation, health, nil)
}

func (h *Handler) pluginInstallationResponseWithReleases(r *http.Request, installation db.PluginInstallation, health *db.PluginHealth, prefetchedReleases *[]db.PluginRelease) (pluginInstallationResponse, error) {
	identity, err := h.Queries.GetPluginIdentity(r.Context(), installation.PluginID)
	if err != nil {
		return pluginInstallationResponse{}, err
	}
	desired, err := h.Queries.GetPluginRelease(r.Context(), installation.DesiredReleaseID)
	if err != nil {
		return pluginInstallationResponse{}, err
	}
	var manifest plugincontract.Manifest
	if err := json.Unmarshal(desired.Manifest, &manifest); err != nil {
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
		Description:       manifest.Metadata.Description,
		Publisher:         identity.PublisherID,
		PublisherType:     identity.PublisherType,
		TrustTier:         identity.TrustTier,
		SourceKind:        desired.SourceKind,
		SourceRef:         desired.SourceRef,
		UploaderID:        uuidToString(installation.InstalledBy),
		ManifestDigest:    desired.ManifestDigest,
		ArchiveDigest:     desired.ArchiveDigest,
		ArtifactDigest:    desired.ArtifactDigest,
		SignatureVerified: desired.SourceKind == plugincontract.SourceBundled && desired.SignatureKeyID.Valid,
		RequestedCaps:     append([]string(nil), manifest.RequestedCapabilities...),
		AvailableVersions: []string{},
		Contributions:     []string{},
		ContributionInfo:  []pluginContributionResponse{},
		Bindings:          []pluginBindingResponse{},
	}
	var releases []db.PluginRelease
	if prefetchedReleases != nil {
		releases = *prefetchedReleases
	} else {
		releases, err = h.Queries.ListPluginReleasesByPlugin(r.Context(), installation.PluginID)
		if err != nil {
			return pluginInstallationResponse{}, err
		}
	}
	for _, release := range releases {
		response.AvailableVersions = append(response.AvailableVersions, release.Version)
	}
	contributions, err := h.Queries.ListPluginContributionsByRelease(r.Context(), desired.ID)
	if err != nil {
		return pluginInstallationResponse{}, err
	}
	for _, contribution := range contributions {
		response.Contributions = append(response.Contributions, contribution.ContributionKey)
		response.ContributionInfo = append(response.ContributionInfo, pluginContributionResponse{
			Key:         contribution.ContributionKey,
			Type:        contribution.Type,
			Name:        contribution.DisplayName,
			Description: contribution.Description,
			EntryPath:   contribution.EntryPath,
			EntryDigest: contribution.EntryDigest,
		})
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
	h.writePluginInstallations(w, r, workspaceID, installations)
}

func (h *Handler) ListPrivatePlugins(w http.ResponseWriter, r *http.Request) {
	if !h.requirePrivatePluginsV1(w, r) {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	installations, err := h.Queries.ListWorkspacePrivatePluginInstallations(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list Private Plugins")
		return
	}
	h.writePluginInstallations(w, r, workspaceID, installations)
}

func (h *Handler) GetPrivatePluginStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requirePrivatePluginsV1(w, r) {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	installation, err := h.Queries.GetWorkspacePrivatePluginInstallationByRef(r.Context(), db.GetWorkspacePrivatePluginInstallationByRefParams{
		WorkspaceID: workspaceID,
		PluginRef:   chi.URLParam(r, "pluginRef"),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Private Plugin installation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Private Plugin")
		return
	}
	var health *db.PluginHealth
	healthRow, healthErr := h.Queries.GetWorkspacePluginHealthByInstallation(r.Context(), db.GetWorkspacePluginHealthByInstallationParams{
		WorkspaceID:    workspaceID,
		InstallationID: installation.ID,
	})
	if healthErr == nil {
		health = &healthRow
	} else if !errors.Is(healthErr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load Private Plugin")
		return
	}
	response, responseErr := h.pluginInstallationResponse(r, installation, health)
	if responseErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Private Plugin")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) writePluginInstallations(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, installations []db.PluginInstallation) {
	healthRows, _ := h.Queries.ListWorkspacePluginHealth(r.Context(), workspaceID)
	healthByInstallation := make(map[string]db.PluginHealth)
	for _, health := range healthRows {
		key := uuidToString(health.InstallationID)
		if _, exists := healthByInstallation[key]; !exists {
			healthByInstallation[key] = health
		}
	}
	releaseRows, err := h.Queries.ListWorkspacePluginReleases(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Plugin releases")
		return
	}
	releasesByPlugin := make(map[string][]db.PluginRelease)
	for _, release := range releaseRows {
		key := uuidToString(release.PluginID)
		releasesByPlugin[key] = append(releasesByPlugin[key], release)
	}
	responses := make([]pluginInstallationResponse, 0, len(installations))
	for _, installation := range installations {
		health, hasHealth := healthByInstallation[uuidToString(installation.ID)]
		var healthPtr *db.PluginHealth
		if hasHealth {
			healthPtr = &health
		}
		releases := releasesByPlugin[uuidToString(installation.PluginID)]
		response, err := h.pluginInstallationResponseWithReleases(r, installation, healthPtr, &releases)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load Plugin")
			return
		}
		responses = append(responses, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": responses})
}

type pluginCatalogReleaseResponse struct {
	PluginKey              string                       `json:"plugin_key"`
	Name                   string                       `json:"name"`
	Description            string                       `json:"description"`
	Version                string                       `json:"version"`
	Publisher              string                       `json:"publisher"`
	PublisherType          string                       `json:"publisher_type"`
	TrustTier              string                       `json:"trust_tier"`
	SourceKind             string                       `json:"source_kind"`
	SourceRef              string                       `json:"source_ref"`
	RequestedCapabilities  []string                     `json:"requested_capabilities"`
	HostAPI                string                       `json:"host_api"`
	RequiredDaemonFeatures []string                     `json:"required_daemon_features"`
	SignatureKeyID         string                       `json:"signature_key_id"`
	SignatureVerified      bool                         `json:"signature_verified"`
	ManifestDigest         string                       `json:"manifest_digest"`
	ArchiveDigest          string                       `json:"archive_digest"`
	ArtifactDigest         string                       `json:"artifact_digest"`
	Compatible             bool                         `json:"compatible"`
	CompatibilityReason    string                       `json:"compatibility_reason,omitempty"`
	Contributions          []pluginContributionResponse `json:"contributions"`
	Installation           *pluginInstallationResponse  `json:"installation,omitempty"`
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
		Contributions:          make([]pluginContributionResponse, 0, len(entry.Release.Contributions)),
	}
	for _, contribution := range entry.Release.Contributions {
		response.Contributions = append(response.Contributions, pluginContributionResponse{
			Key:         contribution.Key,
			Type:        contribution.Type,
			Name:        contribution.DisplayName,
			Description: contribution.Description,
			EntryPath:   contribution.EntryPath,
			EntryDigest: contribution.EntryDigest,
		})
	}
	identity, err := h.Queries.GetOfficialPluginIdentityByKey(r.Context(), manifest.Metadata.Key)
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

func (h *Handler) InstallPrivatePlugin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePrivatePluginsV1(w, r) {
		return
	}
	workspaceID, actorID, ok := pluginRequestIDs(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, plugincontract.MaxArchiveSize+plugincontract.MaxManifestSize)
	if err := r.ParseMultipartForm(plugincontract.MaxArchiveSize + 1); err != nil {
		writeError(w, http.StatusBadRequest, "Private Plugin package is required and must fit the package limit")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll() //nolint:errcheck
	}
	file, _, err := r.FormFile("artifact")
	if err != nil {
		writeError(w, http.StatusBadRequest, "artifact file is required")
		return
	}
	defer file.Close()
	archive, err := io.ReadAll(io.LimitReader(file, plugincontract.MaxArchiveSize+1))
	if err != nil || len(archive) > plugincontract.MaxArchiveSize {
		writeError(w, http.StatusBadRequest, "Private Plugin package exceeds the package limit")
		return
	}
	installation, err := h.PluginService.InstallPrivateArchive(r.Context(), workspaceID, actorID, archive)
	if err != nil {
		writePluginError(w, r, err)
		return
	}
	response, err := h.pluginInstallationResponse(r, installation, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Private Plugin")
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
	if !h.requirePluginInstallationFeature(w, r, workspaceID, installationID, false) {
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
	if !h.requirePluginInstallationFeature(w, r, workspaceID, installationID, !enabled) {
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
	if !h.requirePluginInstallationFeature(w, r, workspaceID, installationID, false) {
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

func (h *Handler) UninstallPlugin(w http.ResponseWriter, r *http.Request) {
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
	if !h.requirePluginInstallationFeature(w, r, workspaceID, installationID, true) {
		return
	}
	if err := h.PluginService.UninstallPlugin(r.Context(), workspaceID, installationID, actorID); err != nil {
		writePluginError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) requirePluginInstallationFeature(w http.ResponseWriter, r *http.Request, workspaceID, installationID pgtype.UUID, allowPrivateTeardown bool) bool {
	installation, err := h.Queries.GetPluginInstallation(r.Context(), installationID)
	if err != nil || installation.WorkspaceID != workspaceID || installation.UninstalledAt.Valid {
		writeError(w, http.StatusNotFound, "Plugin installation not found")
		return false
	}
	if installation.SourceKind == plugincontract.SourcePrivateDev && !allowPrivateTeardown && !h.requirePrivatePluginsV1(w, r) {
		return false
	}
	return true
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
