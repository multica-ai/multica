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
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	PlatformExtensionMaxImportBytes     = 5 * 1024 * 1024
	platformAgentRuntimeContextSchema   = "platform-agent.runtime-context/v1"
	platformExtensionProvider           = "platform-agent-cli"
	platformExtensionRuntimeUnavailable = "PLATFORM_RUNTIME_UNAVAILABLE"
	platformExtensionVersionImmutable   = "EXTENSION_VERSION_IMMUTABLE"
	platformExtensionImportFailed       = "EXTENSION_IMPORT_FAILED"
	platformExtensionNotFound           = "EXTENSION_NOT_FOUND"
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
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PlatformExtensionAgentMappingResponse struct {
	SourceKey string `json:"source_key"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Leader    bool   `json:"leader"`
}

type PlatformExtensionSkillMappingResponse struct {
	SourceKey string `json:"source_key"`
	ID        string `json:"id"`
	Name      string `json:"name"`
}

type PlatformExtensionMappingResponse struct {
	Release PlatformExtensionReleaseResponse        `json:"release"`
	Runtime PlatformExtensionRuntimeResponse        `json:"runtime"`
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
	Manifest json.RawMessage `json:"manifest"`
}

type platformExtensionResources struct {
	Runtime PlatformExtensionRuntimeResponse        `json:"runtime"`
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
		ids := make([]pgtype.UUID, len(authorized))
		for i, runtime := range authorized {
			ids[i] = runtime.ID
		}
		return ids, false, nil
	}
	alive, ok := h.LivenessStore.IsAliveBatch(ctx, runtimeIDs)
	if !ok {
		ids := make([]pgtype.UUID, len(authorized))
		for i, runtime := range authorized {
			ids[i] = runtime.ID
		}
		return ids, false, nil
	}

	ids := make([]pgtype.UUID, 0, len(authorized))
	for _, runtime := range authorized {
		if alive[uuidToString(runtime.ID)] {
			ids = append(ids, runtime.ID)
		}
	}
	return ids, true, nil
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
			writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_INVALID", "extension exceeds the 5 MiB limit")
			return
		}
		writePlatformExtensionError(w, http.StatusBadRequest, "EXTENSION_INVALID", "failed to read extension")
		return
	}

	bundle, manifest, err := decodePlatformExtensionImport(body, h.PlatformExtensionPolicy)
	if err != nil {
		writePlatformExtensionContractError(w, err)
		return
	}

	eligibleRuntimeIDs, useRedisLiveness, err := h.eligiblePlatformExtensionRuntimeIDs(r.Context(), member, workspaceUUID)
	if err != nil {
		slog.Error("platform extension: list runtime candidates failed", "error", err, "workspace_id", workspaceID)
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
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
		if existing.Digest != bundle.Digest {
			writePlatformExtensionError(w, http.StatusConflict, platformExtensionVersionImmutable, "extension version is immutable")
			return
		}
		mapping, mapErr := platformExtensionMappingFromRelease(existing)
		if mapErr != nil {
			slog.Error("platform extension: decode idempotent resource mapping failed", "error", mapErr, "release_id", uuidToString(existing.ID))
			writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
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

	runtime, err := qtx.LockIdlePlatformExtensionRuntime(r.Context(), db.LockIdlePlatformExtensionRuntimeParams{
		WorkspaceID:      workspaceUUID,
		EligibleIds:      eligibleRuntimeIDs,
		UseRedisLiveness: useRedisLiveness,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writePlatformExtensionError(w, http.StatusConflict, platformExtensionRuntimeUnavailable, "no online idle Platform Agent CLI runtime is available")
		return
	}
	if err != nil {
		slog.Error("platform extension: lock runtime failed", "error", err, "workspace_id", workspaceID)
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to import extension")
		return
	}

	mapping, err := createPlatformExtensionNativeResources(r.Context(), qtx, release, runtime, member.UserID, bundle)
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
	completed, err := qtx.CompletePlatformExtensionRelease(r.Context(), db.CompletePlatformExtensionReleaseParams{
		RuntimeID:   runtime.ID,
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
		mapping, err := platformExtensionMappingFromRelease(release)
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
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
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
	mapping, err := platformExtensionMappingFromRelease(release)
	if err != nil {
		slog.Error("platform extension: decode detail resource mapping failed", "error", err, "release_id", uuidToString(release.ID))
		writePlatformExtensionError(w, http.StatusInternalServerError, platformExtensionImportFailed, "failed to load extension")
		return
	}
	writeJSON(w, http.StatusOK, PlatformExtensionDetailResponse{
		PlatformExtensionMappingResponse: mapping,
		Manifest:                         json.RawMessage(release.Manifest),
	})
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
	return bundle, manifest, nil
}

func createPlatformExtensionNativeResources(
	ctx context.Context,
	qtx *db.Queries,
	release db.PlatformExtensionRelease,
	runtime db.AgentRuntime,
	creatorID pgtype.UUID,
	bundle PlatformExtensionBundle,
) (PlatformExtensionMappingResponse, error) {
	mapping := PlatformExtensionMappingResponse{
		Release: newPlatformExtensionReleaseResponse(release),
		Runtime: PlatformExtensionRuntimeResponse{
			ID: uuidToString(runtime.ID), Provider: runtime.Provider, Name: runtime.Name,
		},
		Agents: make([]PlatformExtensionAgentMappingResponse, 0, len(bundle.Agents)),
		Skills: make([]PlatformExtensionSkillMappingResponse, 0, len(bundle.Skills)),
	}

	skillIDs := make([]pgtype.UUID, 0, len(bundle.Skills))
	for _, sourceSkill := range bundle.Skills {
		rootContent := ""
		files := make([]CreateSkillFileRequest, 0, len(sourceSkill.Files)-1)
		for _, file := range sourceSkill.Files {
			if file.Path == "SKILL.md" {
				rootContent = file.Content
				continue
			}
			files = append(files, CreateSkillFileRequest{Path: file.Path, Content: file.Content})
		}
		created, err := createSkillWithFilesInTx(ctx, qtx, skillCreateInput{
			WorkspaceID: release.WorkspaceID,
			CreatorID:   creatorID,
			Name:        platformExtensionNativeResourceName(bundle.Extension, sourceSkill.Name),
			Description: sourceSkill.Description,
			Content:     rootContent,
			Config: map[string]any{"origin": map[string]any{
				"type": "platform_extension", "release_id": uuidToString(release.ID), "source_key": sourceSkill.Key,
			}},
			Files: files,
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

	agentIDs := make(map[string]pgtype.UUID, len(bundle.Agents))
	runtimeCommands := append([]PlatformExtensionCommand(nil), bundle.RuntimeCommands...)
	if runtimeCommands == nil {
		runtimeCommands = []PlatformExtensionCommand{}
	}
	for _, sourceAgent := range bundle.Agents {
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
		agent, err := qtx.CreateAgent(ctx, db.CreateAgentParams{
			WorkspaceID:        release.WorkspaceID,
			Name:               platformExtensionNativeResourceName(bundle.Extension, sourceAgent.Name),
			Description:        sourceAgent.Description,
			RuntimeMode:        "local",
			RuntimeConfig:      runtimeConfig,
			RuntimeID:          runtime.ID,
			Visibility:         "private",
			PermissionMode:     "private",
			MaxConcurrentTasks: 1,
			OwnerID:            creatorID,
			Instructions:       sourceAgent.Prompt,
			CustomEnv:          []byte(`{}`),
			CustomArgs:         []byte(`[]`),
		})
		if err != nil {
			return PlatformExtensionMappingResponse{}, fmt.Errorf("create agent %q: %w", sourceAgent.Key, err)
		}
		agentIDs[sourceAgent.Key] = agent.ID
		mapping.Agents = append(mapping.Agents, PlatformExtensionAgentMappingResponse{
			SourceKey: sourceAgent.Key,
			ID:        uuidToString(agent.ID),
			Name:      agent.Name,
			Leader:    sourceAgent.Key == bundle.Leader,
		})
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
		Name:        platformExtensionSquadName(bundle.Extension),
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
	if !release.RuntimeID.Valid || !release.SquadID.Valid {
		return PlatformExtensionMappingResponse{}, errors.New("platform extension release is incomplete")
	}
	var resources platformExtensionResources
	decoder := json.NewDecoder(bytes.NewReader(release.Resources))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&resources); err != nil {
		return PlatformExtensionMappingResponse{}, fmt.Errorf("decode release resources: %w", err)
	}
	if resources.Agents == nil || resources.Skills == nil || resources.Runtime.ID == "" || resources.Squad.ID == "" {
		return PlatformExtensionMappingResponse{}, errors.New("platform extension release resources are incomplete")
	}
	return PlatformExtensionMappingResponse{
		Release: newPlatformExtensionReleaseResponse(release),
		Runtime: resources.Runtime,
		Squad:   resources.Squad,
		Agents:  resources.Agents,
		Skills:  resources.Skills,
	}, nil
}

func newPlatformExtensionReleaseResponse(release db.PlatformExtensionRelease) PlatformExtensionReleaseResponse {
	return PlatformExtensionReleaseResponse{
		ID: uuidToString(release.ID), ExtensionKey: release.ExtensionKey, Version: release.Version, Digest: release.Digest,
	}
}

func platformExtensionNativeResourceName(extension PlatformExtension, resourceName string) string {
	return fmt.Sprintf("%s v%s / %s", extension.Name, extension.Version, resourceName)
}

func platformExtensionSquadName(extension PlatformExtension) string {
	return fmt.Sprintf("%s v%s", extension.Name, extension.Version)
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
