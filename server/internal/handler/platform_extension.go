package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	PlatformExtensionMaxImportBytes     = 16 * 1024 * 1024
	platformAgentRuntimeContextSchema   = "platform-agent.runtime-context/v1"
	platformExtensionProvider           = "platform-agent-cli"
	platformExtensionRuntimeUnavailable = "PLATFORM_RUNTIME_UNAVAILABLE"
	platformExtensionVersionImmutable   = "EXTENSION_VERSION_IMMUTABLE"
	platformExtensionVersionArchived    = "EXTENSION_VERSION_ARCHIVED"
	platformExtensionImportFailed       = "EXTENSION_IMPORT_FAILED"
	platformExtensionNotFound           = "EXTENSION_NOT_FOUND"
	platformExtensionImportConfigHeader = "X-Multica-Extension-Import-Config"
)

type PlatformExtensionReleaseResponse struct {
	ID           string `json:"id"`
	ExtensionKey string `json:"extension_key"`
	Version      string `json:"version"`
	Digest       string `json:"digest"`
}

type PlatformExtensionRuntimeResponse struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

type PlatformExtensionSquadResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
}

type PlatformExtensionAgentMappingResponse struct {
	SourceKey string                            `json:"source_key"`
	ID        string                            `json:"id"`
	Name      string                            `json:"name"`
	Leader    bool                              `json:"leader"`
	Runtime   *PlatformExtensionRuntimeResponse `json:"runtime"`
}

type PlatformExtensionSkillMappingResponse struct {
	SourceKey string `json:"source_key"`
	ID        string `json:"id"`
	Name      string `json:"name"`
}

type PlatformExtensionMappingResponse struct {
	Release PlatformExtensionReleaseResponse        `json:"release"`
	Runtime *PlatformExtensionRuntimeResponse       `json:"runtime"`
	Squad   PlatformExtensionSquadResponse          `json:"squad"`
	Agents  []PlatformExtensionAgentMappingResponse `json:"agents"`
	Skills  []PlatformExtensionSkillMappingResponse `json:"skills"`
}

type PlatformExtensionImportResponse struct {
	PlatformExtensionMappingResponse
	Idempotent bool `json:"idempotent"`
}

type PlatformExtensionDetailResponse struct {
	PlatformExtensionMappingResponse
	Manifest          json.RawMessage                    `json:"manifest"`
	AvailableRuntimes []PlatformExtensionRuntimeResponse `json:"available_runtimes"`
}

// PlatformExtensionPreviewAgentResponse is a pending internal Agent mapping.
// It exposes the default fixed Runtime but does not create any database rows.
type PlatformExtensionPreviewAgentResponse struct {
	SourceKey string `json:"source_key"`
	Name      string `json:"name"`
	Leader    bool   `json:"leader"`
	// RuntimeID is empty when no compatible local runtime is available yet.
	// The extension is still importable; its internal Agent remains unbound.
	RuntimeID string `json:"runtime_id"`
}

type PlatformExtensionPreviewResponse struct {
	Release       PlatformExtensionReleaseResponse        `json:"release"`
	SquadBaseName string                                  `json:"squad_base_name"`
	Agents        []PlatformExtensionPreviewAgentResponse `json:"agents"`
	Runtimes      []PlatformExtensionRuntimeResponse      `json:"runtimes"`
	Manifest      json.RawMessage                         `json:"manifest"`
}

type platformExtensionImportConfiguration struct {
	SquadBaseName   string            `json:"squad_base_name"`
	AgentRuntimeIDs map[string]string `json:"agent_runtime_ids"`
}

type platformExtensionResources struct {
	Runtime *PlatformExtensionRuntimeResponse       `json:"runtime"`
	Squad   PlatformExtensionSquadResponse          `json:"squad"`
	Agents  []PlatformExtensionAgentMappingResponse `json:"agents"`
	Skills  []PlatformExtensionSkillMappingResponse `json:"skills"`
}

type platformAgentRuntimeConfig struct {
	PlatformAgent platformAgentRuntimeContext `json:"platform_agent"`
}

type platformAgentRuntimeContext struct {
	SchemaVersion string                         `json:"schema_version"`
	Extension     platformAgentExtensionIdentity `json:"extension"`
	Agent         platformAgentIdentity          `json:"agent"`
	Commands      []PlatformExtensionCommand     `json:"commands"`
}

type platformAgentExtensionIdentity struct {
	Key       string `json:"key"`
	Version   string `json:"version"`
	ReleaseID string `json:"release_id"`
	Digest    string `json:"digest"`
}

type platformAgentIdentity struct {
	SourceKey string `json:"source_key"`
}

type platformExtensionHTTPError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// eligiblePlatformExtensionRuntimeIDs performs the checks that cannot live in
// SQL: the existing runtime visibility/ownership predicate and Redis-backed
// liveness. The returned boolean tells the transaction query whether Redis was
// authoritative; false means it must apply the 150-second DB heartbeat window.
func (h *Handler) eligiblePlatformExtensionRuntimeIDs(
	ctx context.Context,
	member db.Member,
	workspaceID pgtype.UUID,
) ([]pgtype.UUID, bool, error) {
	runtimes, redisAuthoritative, err := h.eligiblePlatformExtensionRuntimes(ctx, member, workspaceID)
	if err != nil {
		return nil, false, err
	}
	ids := make([]pgtype.UUID, len(runtimes))
	for i, runtime := range runtimes {
		ids[i] = runtime.ID
	}
	return ids, redisAuthoritative, nil
}

func (h *Handler) eligiblePlatformExtensionRuntimes(
	ctx context.Context,
	member db.Member,
	workspaceID pgtype.UUID,
) ([]db.AgentRuntime, bool, error) {
	candidates, err := h.Queries.ListPlatformExtensionRuntimeCandidates(ctx, workspaceID)
	if err != nil {
		return nil, false, err
	}

	authorized := make([]db.AgentRuntime, 0, len(candidates))
	runtimeIDs := make([]string, 0, len(candidates))
	for _, runtime := range candidates {
		if !canUseRuntimeForAgent(member, runtime) {
			continue
		}
		authorized = append(authorized, runtime)
		runtimeIDs = append(runtimeIDs, uuidToString(runtime.ID))
	}

	if h.LivenessStore == nil {
		sortPlatformExtensionRuntimeCandidates(authorized, member.UserID)
		return authorized, false, nil
	}
	alive, ok := h.LivenessStore.IsAliveBatch(ctx, runtimeIDs)
	if !ok {
		sortPlatformExtensionRuntimeCandidates(authorized, member.UserID)
		return authorized, false, nil
	}

	eligible := make([]db.AgentRuntime, 0, len(authorized))
	for _, runtime := range authorized {
		if alive[uuidToString(runtime.ID)] {
			eligible = append(eligible, runtime)
		}
	}
	sortPlatformExtensionRuntimeCandidates(eligible, member.UserID)
	return eligible, true, nil
}

func sortPlatformExtensionRuntimeCandidates(runtimes []db.AgentRuntime, currentUserID pgtype.UUID) {
	sort.SliceStable(runtimes, func(i, j int) bool {
		leftMine := runtimes[i].OwnerID == currentUserID
		rightMine := runtimes[j].OwnerID == currentUserID
		if leftMine != rightMine {
			return leftMine
		}
		return uuidToString(runtimes[i].ID) < uuidToString(runtimes[j].ID)
	})
}

// PreviewPlatformExtension validates a package and returns its editable
// versioned-Squad mapping without reserving or materializing a release.
func (h *Handler) PreviewPlatformExtension(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	limited := http.MaxBytesReader(w, r.Body, PlatformExtensionMaxImportBytes)
	defer limited.Close()
	body, err := io.ReadAll(limited)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_INVALID", "extension package exceeds the 16 MiB limit")
			return
		}
		writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_INVALID", "failed to read extension")
		return
	}
	bundle, manifest, err := decodePlatformExtensionImportRequest(r.Header.Get("Content-Type"), body, h.PlatformExtensionPolicy)
	if err != nil {
		writePlatformExtensionContractError(w, err)
		return
	}
	baseName, err := platformExtensionDefaultSquadBaseName(bundle)
	if err != nil {
		writePlatformExtensionContractError(w, err)
		return
	}
	runtimes, _, err := h.eligiblePlatformExtensionRuntimes(r.Context(), member, workspaceUUID)
	if err != nil {
		slog.Error("platform extension: list preview runtimes failed", "error", err, "workspace_id", workspaceID)
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to preview extension")
		return
	}
	response := PlatformExtensionPreviewResponse{
		Release: PlatformExtensionReleaseResponse{
			ExtensionKey: bundle.Extension.Key,
			Version:      bundle.Extension.Version,
			Digest:       bundle.Digest,
		},
		SquadBaseName: baseName,
		Agents:        make([]PlatformExtensionPreviewAgentResponse, 0, len(bundle.Agents)),
		Runtimes:      make([]PlatformExtensionRuntimeResponse, 0, len(runtimes)),
		Manifest:      json.RawMessage(manifest),
	}
	defaultRuntimeID := ""
	if len(runtimes) > 0 {
		defaultRuntimeID = uuidToString(runtimes[0].ID)
	}
	for _, runtime := range runtimes {
		response.Runtimes = append(response.Runtimes, PlatformExtensionRuntimeResponse{
			ID: uuidToString(runtime.ID), Provider: runtime.Provider, Name: runtime.Name,
		})
	}
	for _, agent := range bundle.Agents {
		response.Agents = append(response.Agents, PlatformExtensionPreviewAgentResponse{
			SourceKey: agent.Key,
			Name:      agent.Name,
			Leader:    agent.Key == bundle.Leader,
			RuntimeID: defaultRuntimeID,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

// ImportPlatformExtension compiles or validates an immutable extension bundle,
// reserves its identity, selects one idle platform runtime, and materializes
// every native resource in a single PostgreSQL transaction.
func (h *Handler) ImportPlatformExtension(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	limited := http.MaxBytesReader(w, r.Body, PlatformExtensionMaxImportBytes)
	defer limited.Close()
	body, err := io.ReadAll(limited)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_INVALID", "extension package exceeds the 16 MiB limit")
			return
		}
		writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_INVALID", "failed to read extension")
		return
	}

	bundle, manifest, err := decodePlatformExtensionImportRequest(r.Header.Get("Content-Type"), body, h.PlatformExtensionPolicy)
	if err != nil {
		writePlatformExtensionContractError(w, err)
		return
	}

	eligibleRuntimes, _, err := h.eligiblePlatformExtensionRuntimes(r.Context(), member, workspaceUUID)
	if err != nil {
		slog.Error("platform extension: list runtime candidates failed", "error", err, "workspace_id", workspaceID)
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
		return
	}
	configuration, err := platformExtensionImportConfigurationForBundle(
		bundle,
		eligibleRuntimes,
		r.Header.Get(platformExtensionImportConfigHeader),
	)
	if err != nil {
		writePlatformExtensionContractError(w, err)
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	release, err := qtx.CreatePlatformExtensionReleaseReservation(r.Context(), db.CreatePlatformExtensionReleaseReservationParams{
		WorkspaceID:  workspaceUUID,
		ExtensionKey: bundle.Extension.Key,
		Name:         bundle.Extension.Name,
		Version:      bundle.Extension.Version,
		Digest:       bundle.Digest,
		Manifest:     manifest,
		CreatedBy:    member.UserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := qtx.GetPlatformExtensionReleaseByIdentity(r.Context(), db.GetPlatformExtensionReleaseByIdentityParams{
			WorkspaceID: workspaceUUID, ExtensionKey: bundle.Extension.Key, Version: bundle.Extension.Version,
		})
		if getErr != nil {
			slog.Error("platform extension: load reservation winner failed", "error", getErr, "workspace_id", workspaceID)
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
			return
		}
		mapping, mapErr := platformExtensionMappingWithLiveSquad(r.Context(), qtx, existing)
		if mapErr != nil {
			slog.Error("platform extension: load idempotent resource mapping failed", "error", mapErr, "release_id", uuidToString(existing.ID))
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
			return
		}
		if mapping.Squad.Archived {
			writePlatformExtensionError(w, http.StatusConflict, platformExtensionVersionArchived, "extension version is archived")
			return
		}
		if existing.Digest != bundle.Digest {
			writePlatformExtensionError(w, http.StatusConflict, platformExtensionVersionImmutable, "extension version is immutable")
			return
		}
		writeJSON(w, http.StatusOK, PlatformExtensionImportResponse{
			PlatformExtensionMappingResponse: mapping,
			Idempotent:                       true,
		})
		return
	}
	if err != nil {
		slog.Error("platform extension: reserve release failed", "error", err, "workspace_id", workspaceID)
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
		return
	}

	lockedRuntimes := make(map[string]db.AgentRuntime, len(configuration.AgentRuntimeIDs))
	for _, runtimeID := range configuration.AgentRuntimeIDs {
		if runtimeID == "" {
			continue
		}
		if _, alreadyLocked := lockedRuntimes[runtimeID]; alreadyLocked {
			continue
		}
		candidate, lockErr := qtx.LockAgentRuntime(r.Context(), parseUUID(runtimeID))
		if errors.Is(lockErr, pgx.ErrNoRows) {
			writePlatformExtensionError(w, http.StatusConflict, platformExtensionRuntimeUnavailable, "the selected runtime is no longer available")
			return
		}
		if lockErr != nil {
			slog.Error("platform extension: lock selected runtime failed", "error", lockErr, "workspace_id", workspaceID)
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
			return
		}
		if candidate.WorkspaceID != workspaceUUID || candidate.Provider != platformExtensionProvider || candidate.Status != "online" || !canUseRuntimeForAgent(member, candidate) {
			writePlatformExtensionError(w, http.StatusConflict, platformExtensionRuntimeUnavailable, "the selected runtime is no longer available")
			return
		}
		lockedRuntimes[runtimeID] = candidate
	}

	mapping, err := createPlatformExtensionNativeResources(r.Context(), tx, qtx, release, lockedRuntimes, configuration, member.UserID, bundle)
	if err != nil {
		slog.Error("platform extension: create native resources failed", "error", err, "release_id", uuidToString(release.ID))
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
		return
	}
	resources, err := json.Marshal(platformExtensionResources{
		Runtime: mapping.Runtime,
		Squad:   mapping.Squad,
		Agents:  mapping.Agents,
		Skills:  mapping.Skills,
	})
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
		return
	}
	releaseRuntimeID := pgtype.UUID{}
	if mapping.Runtime != nil {
		releaseRuntimeID = parseUUID(mapping.Runtime.ID)
	}
	completed, err := qtx.CompletePlatformExtensionRelease(r.Context(), db.CompletePlatformExtensionReleaseParams{
		RuntimeID:   releaseRuntimeID,
		SquadID:     parseUUID(mapping.Squad.ID),
		Resources:   resources,
		ID:          release.ID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		slog.Error("platform extension: complete release failed", "error", err, "release_id", uuidToString(release.ID))
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("platform extension: commit import failed", "error", err, "release_id", uuidToString(release.ID))
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
		return
	}

	mapping.Release = newPlatformExtensionReleaseResponse(completed)
	writeJSON(w, http.StatusCreated, PlatformExtensionImportResponse{
		PlatformExtensionMappingResponse: mapping,
		Idempotent:                       false,
	})
}

func decodePlatformExtensionImportRequest(contentType string, body []byte, policy PlatformExtensionPolicy) (PlatformExtensionBundle, []byte, error) {
	if strings.HasPrefix(strings.ToLower(contentType), "application/zip") {
		return decodePlatformExtensionArchiveImport(body, policy)
	}
	return decodePlatformExtensionImport(body, policy)
}

func platformExtensionImportConfigurationForBundle(
	bundle PlatformExtensionBundle,
	runtimes []db.AgentRuntime,
	raw string,
) (platformExtensionImportConfiguration, error) {
	baseName, err := platformExtensionDefaultSquadBaseName(bundle)
	if err != nil {
		return platformExtensionImportConfiguration{}, err
	}
	configuration := platformExtensionImportConfiguration{
		SquadBaseName:   baseName,
		AgentRuntimeIDs: make(map[string]string, len(bundle.Agents)),
	}
	if strings.TrimSpace(raw) != "" {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&configuration); err != nil {
			return platformExtensionImportConfiguration{}, platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "invalid import configuration")
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return platformExtensionImportConfiguration{}, platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "invalid import configuration")
		}
	}
	configuration.SquadBaseName = strings.TrimSpace(configuration.SquadBaseName)
	if configuration.SquadBaseName == "" {
		return platformExtensionImportConfiguration{}, platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "squad_base_name is required")
	}

	available := make(map[string]struct{}, len(runtimes))
	for _, runtime := range runtimes {
		available[uuidToString(runtime.ID)] = struct{}{}
	}
	defaultRuntimeID := ""
	if len(runtimes) > 0 {
		defaultRuntimeID = uuidToString(runtimes[0].ID)
	}
	for sourceKey := range configuration.AgentRuntimeIDs {
		found := false
		for _, agent := range bundle.Agents {
			if agent.Key == sourceKey {
				found = true
				break
			}
		}
		if !found {
			return platformExtensionImportConfiguration{}, platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "unknown Agent runtime selection")
		}
	}
	for _, agent := range bundle.Agents {
		runtimeID, selected := configuration.AgentRuntimeIDs[agent.Key]
		runtimeID = strings.TrimSpace(runtimeID)
		if !selected {
			runtimeID = defaultRuntimeID
		}
		if runtimeID == "" {
			configuration.AgentRuntimeIDs[agent.Key] = ""
			continue
		}
		if _, err := util.ParseUUID(runtimeID); err != nil {
			return platformExtensionImportConfiguration{}, platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "agent runtime_id must be a UUID")
		}
		if _, ok := available[runtimeID]; !ok {
			return platformExtensionImportConfiguration{}, platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "selected runtime is not compatible")
		}
		configuration.AgentRuntimeIDs[agent.Key] = runtimeID
	}
	return configuration, nil
}

func (h *Handler) ListPlatformExtensions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	releases, err := h.Queries.ListPlatformExtensionReleasesInWorkspace(r.Context(), workspaceUUID)
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to list extensions")
		return
	}
	response := make([]PlatformExtensionMappingResponse, 0, len(releases))
	for _, release := range releases {
		mapping, err := platformExtensionMappingWithLiveSquad(r.Context(), h.Queries, release)
		if err != nil {
			slog.Error("platform extension: decode list resource mapping failed", "error", err, "release_id", uuidToString(release.ID))
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to list extensions")
			return
		}
		response = append(response, mapping)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) GetPlatformExtension(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	releaseID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "extension id")
	if !ok {
		return
	}
	release, err := h.Queries.GetPlatformExtensionReleaseInWorkspace(r.Context(), db.GetPlatformExtensionReleaseInWorkspaceParams{
		ID: releaseID, WorkspaceID: workspaceUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writePlatformExtensionError(w, http.StatusNotFound, platformExtensionNotFound, "extension not found")
		return
	}
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to load extension")
		return
	}
	mapping, err := platformExtensionMappingWithLiveSquad(r.Context(), h.Queries, release)
	if err != nil {
		slog.Error("platform extension: decode detail resource mapping failed", "error", err, "release_id", uuidToString(release.ID))
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to load extension")
		return
	}
	runtimes, _, err := h.eligiblePlatformExtensionRuntimes(r.Context(), member, workspaceUUID)
	if err != nil {
		slog.Error("platform extension: list editable runtimes failed", "error", err, "release_id", uuidToString(release.ID))
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to load extension")
		return
	}
	writeJSON(w, http.StatusOK, PlatformExtensionDetailResponse{
		PlatformExtensionMappingResponse: mapping,
		Manifest:                         json.RawMessage(release.Manifest),
		AvailableRuntimes:                platformExtensionRuntimeResponses(runtimes),
	})
}

// UpdatePlatformExtension persists the only editable portions of an imported
// release: its Squad base name and each internal Agent's fixed Runtime. The
// extension version, generated resources, and command/skill mappings remain
// immutable audit data.
func (h *Handler) UpdatePlatformExtension(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	releaseID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "extension id")
	if !ok {
		return
	}
	var configuration platformExtensionImportConfiguration
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&configuration); err != nil {
		writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_IMPORT_CONFIG_INVALID", "invalid extension configuration")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_IMPORT_CONFIG_INVALID", "invalid extension configuration")
		return
	}

	eligible, _, err := h.eligiblePlatformExtensionRuntimes(r.Context(), member, workspaceUUID)
	if err != nil {
		slog.Error("platform extension: list editable runtimes failed", "error", err, "workspace_id", workspaceID)
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
		return
	}
	available := make(map[string]struct{}, len(eligible))
	for _, runtime := range eligible {
		available[uuidToString(runtime.ID)] = struct{}{}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	release, err := qtx.LockPlatformExtensionReleaseInWorkspace(r.Context(), db.LockPlatformExtensionReleaseInWorkspaceParams{ID: releaseID, WorkspaceID: workspaceUUID})
	if errors.Is(err, pgx.ErrNoRows) {
		writePlatformExtensionError(w, http.StatusNotFound, platformExtensionNotFound, "extension not found")
		return
	}
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
		return
	}
	currentMapping, err := platformExtensionMappingWithLiveSquad(r.Context(), qtx, release)
	if err != nil {
		slog.Error("platform extension: load editable resource mapping failed", "error", err, "release_id", uuidToString(release.ID))
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
		return
	}
	if currentMapping.Squad.Archived {
		writePlatformExtensionError(w, http.StatusConflict, platformExtensionVersionArchived, "extension version is archived")
		return
	}
	resources, err := platformExtensionResourcesFromRelease(release)
	if err != nil {
		writePlatformExtensionError(w, http.StatusConflict, "EXTENSION_RESOURCE_MAPPING_INVALID", "extension resources cannot be updated")
		return
	}
	if err := validatePlatformExtensionUpdateConfiguration(&configuration, resources, available); err != nil {
		writePlatformExtensionContractError(w, err)
		return
	}

	runtimeIDs := make([]string, 0, len(configuration.AgentRuntimeIDs))
	seenRuntimeIDs := make(map[string]struct{}, len(configuration.AgentRuntimeIDs))
	for _, runtimeID := range configuration.AgentRuntimeIDs {
		if runtimeID == "" {
			continue
		}
		if _, seen := seenRuntimeIDs[runtimeID]; !seen {
			seenRuntimeIDs[runtimeID] = struct{}{}
			runtimeIDs = append(runtimeIDs, runtimeID)
		}
	}
	sort.Strings(runtimeIDs)
	lockedRuntimes := make(map[string]db.AgentRuntime, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		runtime, lockErr := qtx.LockAgentRuntime(r.Context(), parseUUID(runtimeID))
		if errors.Is(lockErr, pgx.ErrNoRows) {
			writePlatformExtensionError(w, http.StatusConflict, platformExtensionRuntimeUnavailable, "the selected runtime is no longer available")
			return
		}
		if lockErr != nil {
			slog.Error("platform extension: lock editable runtime failed", "error", lockErr, "release_id", uuidToString(release.ID))
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
			return
		}
		if runtime.WorkspaceID != workspaceUUID || runtime.Provider != platformExtensionProvider || runtime.Status != "online" || !canUseRuntimeForAgent(member, runtime) {
			writePlatformExtensionError(w, http.StatusConflict, platformExtensionRuntimeUnavailable, "the selected runtime is no longer available")
			return
		}
		lockedRuntimes[runtimeID] = runtime
	}

	squadName := platformExtensionSquadName(configuration.SquadBaseName, release.Version)
	if _, err := qtx.UpdatePlatformExtensionSquadName(r.Context(), db.UpdatePlatformExtensionSquadNameParams{SquadID: parseUUID(resources.Squad.ID), WorkspaceID: workspaceUUID, Name: squadName}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writePlatformExtensionError(w, http.StatusConflict, "EXTENSION_RESOURCE_MAPPING_INVALID", "extension resources cannot be updated")
			return
		}
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
		return
	}
	resources.Squad.Name = squadName
	resources.Runtime = nil
	for index := range resources.Agents {
		agent := &resources.Agents[index]
		runtimeID := configuration.AgentRuntimeIDs[agent.SourceKey]
		var runtimeResponse *PlatformExtensionRuntimeResponse
		if runtimeID != "" {
			runtime := lockedRuntimes[runtimeID]
			runtimeResponse = &PlatformExtensionRuntimeResponse{ID: uuidToString(runtime.ID), Provider: runtime.Provider, Name: runtime.Name}
		}
		runtimeUUID := pgtype.UUID{}
		if runtimeID != "" {
			runtimeUUID = parseUUID(runtimeID)
		}
		if _, err := qtx.UpdatePlatformExtensionInternalAgentRuntime(r.Context(), db.UpdatePlatformExtensionInternalAgentRuntimeParams{
			RuntimeID:   runtimeUUID,
			ID:          parseUUID(agent.ID),
			WorkspaceID: workspaceUUID,
			SystemKey:   pgtype.Text{String: "platform_extension:" + uuidToString(release.ID) + ":" + agent.SourceKey, Valid: true},
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writePlatformExtensionError(w, http.StatusConflict, "EXTENSION_RESOURCE_MAPPING_INVALID", "extension resources cannot be updated")
				return
			}
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
			return
		}
		agent.Runtime = runtimeResponse
		if agent.Leader {
			resources.Runtime = runtimeResponse
		}
	}
	serializedResources, err := json.Marshal(resources)
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
		return
	}
	releaseRuntimeID := pgtype.UUID{}
	if resources.Runtime != nil {
		releaseRuntimeID = parseUUID(resources.Runtime.ID)
	}
	updatedRelease, err := qtx.UpdatePlatformExtensionReleaseMapping(r.Context(), db.UpdatePlatformExtensionReleaseMappingParams{
		RuntimeID: releaseRuntimeID, Resources: serializedResources, ID: release.ID, WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
		return
	}
	mapping, err := platformExtensionMappingFromRelease(updatedRelease)
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to update extension")
		return
	}
	writeJSON(w, http.StatusOK, mapping)
}

// ArchivePlatformExtension archives only the Squad materialized for this
// immutable Extension version. Internal Agents and Skills remain as historical
// version resources, and other imported versions are untouched.
func (h *Handler) ArchivePlatformExtension(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	releaseID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "extension id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to archive extension")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	release, err := qtx.LockPlatformExtensionReleaseInWorkspace(r.Context(), db.LockPlatformExtensionReleaseInWorkspaceParams{ID: releaseID, WorkspaceID: workspaceUUID})
	if errors.Is(err, pgx.ErrNoRows) {
		writePlatformExtensionError(w, http.StatusNotFound, platformExtensionNotFound, "extension not found")
		return
	}
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to archive extension")
		return
	}
	resources, err := platformExtensionResourcesFromRelease(release)
	if err != nil {
		writePlatformExtensionError(w, http.StatusConflict, "EXTENSION_RESOURCE_MAPPING_INVALID", "extension resources cannot be archived")
		return
	}
	if !resources.Squad.Archived {
		if _, err := qtx.ArchivePlatformExtensionSquad(r.Context(), db.ArchivePlatformExtensionSquadParams{
			SquadID: parseUUID(resources.Squad.ID), WorkspaceID: workspaceUUID, ArchivedBy: member.UserID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writePlatformExtensionError(w, http.StatusConflict, "EXTENSION_RESOURCE_MAPPING_INVALID", "extension resources cannot be archived")
				return
			}
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to archive extension")
			return
		}
		resources.Squad.Archived = true
		serializedResources, err := json.Marshal(resources)
		if err != nil {
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to archive extension")
			return
		}
		if _, err := qtx.UpdatePlatformExtensionReleaseMapping(r.Context(), db.UpdatePlatformExtensionReleaseMappingParams{
			RuntimeID: release.RuntimeID, Resources: serializedResources, ID: release.ID, WorkspaceID: workspaceUUID,
		}); err != nil {
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to archive extension")
			return
		}
		release.Resources = serializedResources
	}
	if err := tx.Commit(r.Context()); err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to archive extension")
		return
	}
	mapping, err := platformExtensionMappingFromRelease(release)
	if err != nil {
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to archive extension")
		return
	}
	writeJSON(w, http.StatusOK, mapping)
}

func validatePlatformExtensionUpdateConfiguration(configuration *platformExtensionImportConfiguration, resources platformExtensionResources, available map[string]struct{}) error {
	configuration.SquadBaseName = strings.TrimSpace(configuration.SquadBaseName)
	if configuration.SquadBaseName == "" {
		return platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "squad_base_name is required")
	}
	if len(configuration.AgentRuntimeIDs) != len(resources.Agents) {
		return platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "a runtime selection is required for every internal Agent")
	}
	for _, agent := range resources.Agents {
		runtimeID, found := configuration.AgentRuntimeIDs[agent.SourceKey]
		if !found {
			return platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "a runtime selection is required for every internal Agent")
		}
		runtimeID = strings.TrimSpace(runtimeID)
		configuration.AgentRuntimeIDs[agent.SourceKey] = runtimeID
		if runtimeID == "" {
			continue
		}
		if _, err := util.ParseUUID(runtimeID); err != nil {
			return platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "agent runtime_id must be a UUID")
		}
		if _, ok := available[runtimeID]; !ok {
			return platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "selected runtime is not compatible")
		}
	}
	for sourceKey := range configuration.AgentRuntimeIDs {
		found := false
		for _, agent := range resources.Agents {
			if agent.SourceKey == sourceKey {
				found = true
				break
			}
		}
		if !found {
			return platformExtensionCode("EXTENSION_IMPORT_CONFIG_INVALID", "unknown Agent runtime selection")
		}
	}
	return nil
}

func platformExtensionRuntimeResponses(runtimes []db.AgentRuntime) []PlatformExtensionRuntimeResponse {
	responses := make([]PlatformExtensionRuntimeResponse, 0, len(runtimes))
	for _, runtime := range runtimes {
		responses = append(responses, PlatformExtensionRuntimeResponse{ID: uuidToString(runtime.ID), Provider: runtime.Provider, Name: runtime.Name})
	}
	return responses
}

func decodePlatformExtensionImport(data []byte, policy PlatformExtensionPolicy) (PlatformExtensionBundle, []byte, error) {
	if err := rejectPlatformExtensionDuplicateObjectKeys(data); err != nil {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_INVALID", err.Error())
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_INVALID", err.Error())
	}

	var bundle PlatformExtensionBundle
	switch envelope.SchemaVersion {
	case PlatformExtensionSourceSchemaVersion:
		source, err := DecodePlatformExtensionSource(data)
		if err != nil {
			return PlatformExtensionBundle{}, nil, err
		}
		bundle, err = CompilePlatformExtensionWithPolicy(source, policy)
		if err != nil {
			return PlatformExtensionBundle{}, nil, err
		}
	case PlatformExtensionBundleSchemaVersion:
		decoded, err := DecodePlatformExtensionBundle(data)
		if err != nil {
			return PlatformExtensionBundle{}, nil, err
		}
		if err := ValidatePlatformExtensionBundleWithPolicy(decoded, policy); err != nil {
			return PlatformExtensionBundle{}, nil, err
		}
		canonical, err := canonicalizePlatformExtensionBundleMetadata(decoded)
		if err != nil {
			return PlatformExtensionBundle{}, nil, err
		}
		bundle = canonical
	default:
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_INVALID", "unsupported schema_version")
	}

	manifest, err := CanonicalPlatformExtensionBundleJSON(bundle)
	if err != nil {
		return PlatformExtensionBundle{}, nil, platformExtensionCode("EXTENSION_INVALID", err.Error())
	}
	if err := validatePlatformExtensionImportE2ECommand(bundle); err != nil {
		return PlatformExtensionBundle{}, nil, err
	}
	return bundle, manifest, nil
}

func createPlatformExtensionNativeResources(
	ctx context.Context,
	tx pgx.Tx,
	qtx *db.Queries,
	release db.PlatformExtensionRelease,
	lockedRuntimes map[string]db.AgentRuntime,
	configuration platformExtensionImportConfiguration,
	creatorID pgtype.UUID,
	bundle PlatformExtensionBundle,
) (PlatformExtensionMappingResponse, error) {
	mapping := PlatformExtensionMappingResponse{
		Release: newPlatformExtensionReleaseResponse(release),
		Agents:  make([]PlatformExtensionAgentMappingResponse, 0, len(bundle.Agents)),
		Skills:  make([]PlatformExtensionSkillMappingResponse, 0, len(bundle.Skills)),
	}

	skillIDs := make([]pgtype.UUID, 0, len(bundle.Skills))
	for _, sourceSkill := range bundle.Skills {
		rootContent := ""
		files := make([]CreateSkillFileRequest, 0, len(sourceSkill.Files)-1)
		fileEncodings := make(map[string]string)
		for _, file := range sourceSkill.Files {
			if file.Path == "SKILL.md" {
				rootContent = file.Content
				continue
			}
			files = append(files, CreateSkillFileRequest{Path: file.Path, Content: file.Content})
			if file.Encoding == "base64" {
				fileEncodings[file.Path] = file.Encoding
			}
		}
		config := map[string]any{"origin": map[string]any{
			"type": "platform_extension", "scope": "squad_internal", "release_id": uuidToString(release.ID), "source_key": sourceSkill.Key,
		}}
		if len(fileEncodings) > 0 {
			config[platformExtensionSkillFileEncodingsConfigKey] = fileEncodings
		}
		created, err := createSkillWithFilesInTx(ctx, qtx, skillCreateInput{
			WorkspaceID: release.WorkspaceID,
			CreatorID:   creatorID,
			Name:        platformExtensionNativeResourceName(bundle.Extension, sourceSkill.Name),
			Description: sourceSkill.Description,
			Content:     rootContent,
			Config:      config,
			Files:       files,
		})
		if err != nil {
			return PlatformExtensionMappingResponse{}, fmt.Errorf("create skill %q: %w", sourceSkill.Key, err)
		}
		skillID := parseUUID(created.ID)
		skillIDs = append(skillIDs, skillID)
		mapping.Skills = append(mapping.Skills, PlatformExtensionSkillMappingResponse{
			SourceKey: sourceSkill.Key, ID: created.ID, Name: created.Name,
		})
	}
	// Commands that are not Flow Commands are native Skills as well as
	// runtime-context entries. Keeping the runtime-context entry preserves
	// compatibility with older Platform Agent CLI versions, while the Skill
	// gives the imported command a first-class Multica resource and makes the
	// mapping visible in the workspace UI.
	for _, command := range bundle.RuntimeCommands {
		var commandMetadata any = map[string]any{}
		if len(command.Metadata) > 0 {
			if err := json.Unmarshal(command.Metadata, &commandMetadata); err != nil {
				return PlatformExtensionMappingResponse{}, fmt.Errorf("decode generated skill metadata %q: %w", command.Name, err)
			}
		}
		created, err := createSkillWithFilesInTx(ctx, qtx, skillCreateInput{
			WorkspaceID: release.WorkspaceID,
			CreatorID:   creatorID,
			Name:        platformExtensionNativeResourceName(bundle.Extension, command.Name),
			Description: command.Description,
			Content:     command.Content,
			Config: map[string]any{
				"origin": map[string]any{
					"type": "platform_extension_command", "scope": "squad_internal",
					"release_id": uuidToString(release.ID),
					"source_key": command.Name,
				},
				"metadata": commandMetadata,
			},
		})
		if err != nil {
			return PlatformExtensionMappingResponse{}, fmt.Errorf("create generated skill %q: %w", command.Name, err)
		}
		skillID := parseUUID(created.ID)
		skillIDs = append(skillIDs, skillID)
		mapping.Skills = append(mapping.Skills, PlatformExtensionSkillMappingResponse{
			SourceKey: "command:" + command.Name,
			ID:        created.ID,
			Name:      created.Name,
		})
	}

	agentIDs := make(map[string]pgtype.UUID, len(bundle.Agents))
	runtimeCommands := append([]PlatformExtensionCommand(nil), bundle.RuntimeCommands...)
	if runtimeCommands == nil {
		runtimeCommands = []PlatformExtensionCommand{}
	}
	for _, sourceAgent := range bundle.Agents {
		runtimeID := configuration.AgentRuntimeIDs[sourceAgent.Key]
		var runtime *db.AgentRuntime
		if runtimeID != "" {
			lockedRuntime, ok := lockedRuntimes[runtimeID]
			if !ok {
				return PlatformExtensionMappingResponse{}, fmt.Errorf("selected runtime missing for agent %q", sourceAgent.Key)
			}
			runtime = &lockedRuntime
		}
		runtimeConfig, err := json.Marshal(platformAgentRuntimeConfig{PlatformAgent: platformAgentRuntimeContext{
			SchemaVersion: platformAgentRuntimeContextSchema,
			Extension: platformAgentExtensionIdentity{
				Key: bundle.Extension.Key, Version: bundle.Extension.Version, ReleaseID: uuidToString(release.ID), Digest: bundle.Digest,
			},
			Agent:    platformAgentIdentity{SourceKey: sourceAgent.Key},
			Commands: runtimeCommands,
		}})
		if err != nil {
			return PlatformExtensionMappingResponse{}, fmt.Errorf("marshal runtime config for agent %q: %w", sourceAgent.Key, err)
		}
		agentRuntimeID := pgtype.UUID{}
		if runtime != nil {
			agentRuntimeID = runtime.ID
		}
		agent, err := qtx.CreateAgent(ctx, db.CreateAgentParams{
			WorkspaceID:         release.WorkspaceID,
			Name:                platformExtensionNativeResourceName(bundle.Extension, sourceAgent.Name),
			Description:         sourceAgent.Description,
			RuntimeMode:         "local",
			RuntimeConfig:       runtimeConfig,
			RuntimeID:           agentRuntimeID,
			Visibility:          "private",
			PermissionMode:      "private",
			MaxConcurrentTasks:  1,
			OwnerID:             creatorID,
			Instructions:        sourceAgent.Prompt,
			CustomEnv:           []byte(`{}`),
			CustomArgs:          []byte(`[]`),
			RuntimeBindingMode:  "fixed",
			RuntimeRequirements: []byte(`{}`),
		})
		if err != nil {
			return PlatformExtensionMappingResponse{}, fmt.Errorf("create agent %q: %w", sourceAgent.Key, err)
		}
		if _, err := tx.Exec(ctx, `UPDATE agent SET kind = 'system', system_key = $2 WHERE id = $1`, agent.ID, "platform_extension:"+uuidToString(release.ID)+":"+sourceAgent.Key); err != nil {
			return PlatformExtensionMappingResponse{}, fmt.Errorf("mark agent %q as internal: %w", sourceAgent.Key, err)
		}
		agentIDs[sourceAgent.Key] = agent.ID
		var runtimeResponse *PlatformExtensionRuntimeResponse
		if runtime != nil {
			runtimeResponse = &PlatformExtensionRuntimeResponse{ID: uuidToString(runtime.ID), Provider: runtime.Provider, Name: runtime.Name}
		}
		mapping.Agents = append(mapping.Agents, PlatformExtensionAgentMappingResponse{
			SourceKey: sourceAgent.Key,
			ID:        uuidToString(agent.ID),
			Name:      agent.Name,
			Leader:    sourceAgent.Key == bundle.Leader,
			Runtime:   runtimeResponse,
		})
		if sourceAgent.Key == bundle.Leader && runtimeResponse != nil {
			mapping.Runtime = runtimeResponse
		}
		for _, skillID := range skillIDs {
			if err := qtx.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: agent.ID, SkillID: skillID}); err != nil {
				return PlatformExtensionMappingResponse{}, fmt.Errorf("bind agent %q to skill: %w", sourceAgent.Key, err)
			}
		}
	}

	leaderID, exists := agentIDs[bundle.Leader]
	if !exists {
		return PlatformExtensionMappingResponse{}, errors.New("compiled bundle leader has no native agent")
	}
	squad, err := qtx.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: release.WorkspaceID,
		Name:        platformExtensionSquadName(configuration.SquadBaseName, bundle.Extension.Version),
		Description: bundle.Extension.Description,
		LeaderID:    leaderID,
		CreatorID:   creatorID,
	})
	if err != nil {
		return PlatformExtensionMappingResponse{}, fmt.Errorf("create squad: %w", err)
	}
	squad, err = qtx.UpdateSquad(ctx, db.UpdateSquadParams{
		ID:           squad.ID,
		Instructions: pgtype.Text{String: bundle.SquadInstructions, Valid: true},
	})
	if err != nil {
		return PlatformExtensionMappingResponse{}, fmt.Errorf("set squad instructions: %w", err)
	}
	for _, sourceAgent := range bundle.Agents {
		role := "member"
		if sourceAgent.Key == bundle.Leader {
			role = "leader"
		}
		if _, err := qtx.AddSquadMember(ctx, db.AddSquadMemberParams{
			SquadID: squad.ID, MemberType: "agent", MemberID: agentIDs[sourceAgent.Key], Role: role,
		}); err != nil {
			return PlatformExtensionMappingResponse{}, fmt.Errorf("add squad member %q: %w", sourceAgent.Key, err)
		}
	}
	mapping.Squad = PlatformExtensionSquadResponse{ID: uuidToString(squad.ID), Name: squad.Name}
	return mapping, nil
}

func platformExtensionMappingFromRelease(release db.PlatformExtensionRelease) (PlatformExtensionMappingResponse, error) {
	resources, err := platformExtensionResourcesFromRelease(release)
	if err != nil {
		return PlatformExtensionMappingResponse{}, err
	}
	// Releases imported before per-Agent runtime selection stored one runtime
	// at the release level. Preserve their audit detail while presenting the
	// current per-Agent response contract.
	for index := range resources.Agents {
		if resources.Agents[index].Runtime == nil && resources.Runtime != nil {
			resources.Agents[index].Runtime = resources.Runtime
		}
	}
	return PlatformExtensionMappingResponse{
		Release: newPlatformExtensionReleaseResponse(release),
		Runtime: resources.Runtime,
		Squad:   resources.Squad,
		Agents:  resources.Agents,
		Skills:  resources.Skills,
	}, nil
}

// platformExtensionMappingWithLiveSquad keeps immutable release resources as
// the audit record while reflecting the Squad's current lifecycle state. A
// Squad can be archived from the normal Squad UI, so storing archived=true in
// the release JSON would otherwise leave Extensions showing a stale status.
func platformExtensionMappingWithLiveSquad(ctx context.Context, queries *db.Queries, release db.PlatformExtensionRelease) (PlatformExtensionMappingResponse, error) {
	mapping, err := platformExtensionMappingFromRelease(release)
	if err != nil {
		return PlatformExtensionMappingResponse{}, err
	}
	squad, err := queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          parseUUID(mapping.Squad.ID),
		WorkspaceID: release.WorkspaceID,
	})
	if err != nil {
		return PlatformExtensionMappingResponse{}, fmt.Errorf("load release squad: %w", err)
	}
	mapping.Squad.Name = squad.Name
	mapping.Squad.Archived = squad.ArchivedAt.Valid
	return mapping, nil
}

func platformExtensionResourcesFromRelease(release db.PlatformExtensionRelease) (platformExtensionResources, error) {
	if !release.SquadID.Valid {
		return platformExtensionResources{}, errors.New("platform extension release is incomplete")
	}
	var resources platformExtensionResources
	decoder := json.NewDecoder(bytes.NewReader(release.Resources))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&resources); err != nil {
		return platformExtensionResources{}, fmt.Errorf("decode release resources: %w", err)
	}
	if resources.Agents == nil || resources.Skills == nil || resources.Squad.ID == "" {
		return platformExtensionResources{}, errors.New("platform extension release resources are incomplete")
	}
	return resources, nil
}

func newPlatformExtensionReleaseResponse(release db.PlatformExtensionRelease) PlatformExtensionReleaseResponse {
	return PlatformExtensionReleaseResponse{
		ID: uuidToString(release.ID), ExtensionKey: release.ExtensionKey, Version: release.Version, Digest: release.Digest,
	}
}

func platformExtensionNativeResourceName(extension PlatformExtension, resourceName string) string {
	return fmt.Sprintf("%s v%s / %s", extension.Name, extension.Version, resourceName)
}

func platformExtensionSquadName(baseName, version string) string {
	return fmt.Sprintf("%s · v%s", baseName, version)
}

// isPlatformExtensionInternalSkill keeps extension-generated Skills scoped to
// their Squad while leaving the normal agent_skill execution lookup intact.
func isPlatformExtensionInternalSkill(config []byte) bool {
	var decoded struct {
		Origin struct {
			Scope string `json:"scope"`
		} `json:"origin"`
	}
	return json.Unmarshal(config, &decoded) == nil && decoded.Origin.Scope == "squad_internal"
}

func writePlatformExtensionError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, platformExtensionHTTPError{Error: message, Code: code})
}

func writePlatformExtensionContractError(w http.ResponseWriter, err error) {
	var contractErr *PlatformExtensionContractError
	if !errors.As(err, &contractErr) {
		writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_INVALID", "extension is invalid")
		return
	}
	switch contractErr.Code {
	case "COMMAND_SUFFIX_POLICY_INVALID":
		writePlatformExtensionError(w, http.StatusInternalServerError, contractErr.Code, "extension command policy is invalid")
	case "COMMAND_SUFFIX_POLICY_MISMATCH":
		writePlatformExtensionError(w, http.StatusBadRequest, contractErr.Code, "extension command policy does not match the trusted policy")
	case "TOOL_COMMAND_UNSUPPORTED", "COMMAND_CONFLICT":
		writePlatformExtensionError(w, http.StatusUnprocessableEntity, contractErr.Code, contractErr.Message)
	case "BUNDLE_DIGEST_INVALID":
		writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_DIGEST_MISMATCH", "extension bundle digest does not match")
	default:
		message := strings.TrimSpace(contractErr.Message)
		if message == "" {
			message = "extension is invalid"
		}
		writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_INVALID", message)
	}
}
