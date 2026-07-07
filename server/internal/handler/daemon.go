package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/daemonws"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// ---------------------------------------------------------------------------
// Daemon workspace ownership helpers
// ---------------------------------------------------------------------------

// requireDaemonWorkspaceAccess verifies the caller has access to the given workspace.
// For daemon tokens (mdt_), compares the token's workspace ID directly.
// For PAT/JWT fallback, verifies user membership in the workspace.
func (h *Handler) requireDaemonWorkspaceAccess(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	if workspaceID == "" {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}

	// Daemon token: workspace must match.
	if daemonWsID := middleware.DaemonWorkspaceIDFromContext(r.Context()); daemonWsID != "" {
		if daemonWsID != workspaceID {
			writeError(w, http.StatusNotFound, "not found")
			return false
		}
		return true
	}

	// PAT/JWT fallback: check membership cache before hitting DB.
	userID := requestUserID(r)
	if userID != "" {
		if h.MembershipCache.Get(r.Context(), userID, workspaceID) {
			return true
		}
	}

	_, ok := h.requireWorkspaceMember(w, r, workspaceID, "not found")
	if ok && userID != "" {
		h.MembershipCache.Set(r.Context(), userID, workspaceID)
	}
	return ok
}

// requireDaemonRuntimeAccess looks up a runtime and verifies the caller can
// operate that exact runtime. Workspace membership alone is not enough:
// daemon tokens must match runtime.daemon_id, while user-backed PAT/JWT/cloud
// PAT requests must come from runtime.owner_id.
//
// Only pgx.ErrNoRows is treated as a real "runtime gone" 404 — the daemon uses
// that response to drop the stale runtime from its in-memory map and re-register,
// so collapsing transient DB errors into the same 404 would force the daemon to
// self-cleanup on a hiccup. Other DB errors become 500.
func (h *Handler) requireDaemonRuntimeAccess(w http.ResponseWriter, r *http.Request, runtimeID string) (db.AgentRuntime, bool) {
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return db.AgentRuntime{}, false
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "runtime not found")
			return db.AgentRuntime{}, false
		}
		slog.Warn("get agent runtime failed", "runtime_id", runtimeID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load runtime")
		return db.AgentRuntime{}, false
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(rt.WorkspaceID)) {
		return db.AgentRuntime{}, false
	}
	if !canAccessDaemonRuntime(r, rt) {
		writeError(w, http.StatusNotFound, "runtime not found")
		return db.AgentRuntime{}, false
	}
	return rt, true
}

func canAccessDaemonRuntime(r *http.Request, rt db.AgentRuntime) bool {
	if daemonID := middleware.DaemonIDFromContext(r.Context()); daemonID != "" {
		return rt.DaemonID.Valid && rt.DaemonID.String == daemonID
	}
	userID := requestUserID(r)
	return userID != "" && rt.OwnerID.Valid && uuidToString(rt.OwnerID) == userID
}

// requireDaemonTaskAccess looks up a task and verifies the caller owns its workspace.
func (h *Handler) requireDaemonTaskAccess(w http.ResponseWriter, r *http.Request, taskID string) (db.AgentTaskQueue, bool) {
	task, _, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	return task, ok
}

// requireDaemonTaskAccessWithWorkspace is the workspace-aware variant of
// requireDaemonTaskAccess. It returns the resolved workspace ID alongside
// the task row so callers that need to forward workspace_id into
// taskToResponse (powering RelativeWorkDir) don't have to repeat the
// ResolveTaskWorkspaceID lookup. The two helpers share their entire
// implementation; the simpler one is preserved for ergonomic call sites
// that genuinely don't need workspace_id.
func (h *Handler) requireDaemonTaskAccessWithWorkspace(w http.ResponseWriter, r *http.Request, taskID string) (db.AgentTaskQueue, string, bool) {
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
	if !ok {
		return db.AgentTaskQueue{}, "", false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		// Only treat pgx.ErrNoRows as a real "task gone" signal — daemon
		// uses this 404 to interrupt the running agent, so a transient DB
		// error must not be reported as a deletion.
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "task not found")
			return db.AgentTaskQueue{}, "", false
		}
		slog.Warn("get agent task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return db.AgentTaskQueue{}, "", false
	}

	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" {
		writeError(w, http.StatusNotFound, "task not found")
		return db.AgentTaskQueue{}, "", false
	}

	if !h.requireDaemonWorkspaceAccess(w, r, wsID) {
		return db.AgentTaskQueue{}, "", false
	}
	if task.RuntimeID.Valid {
		rt, err := h.Queries.GetAgentRuntime(r.Context(), task.RuntimeID)
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, "task not found")
				return db.AgentTaskQueue{}, "", false
			}
			slog.Warn("get task runtime failed", "task_id", taskID, "runtime_id", uuidToString(task.RuntimeID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to load task runtime")
			return db.AgentTaskQueue{}, "", false
		}
		if !canAccessDaemonRuntime(r, rt) {
			writeError(w, http.StatusNotFound, "task not found")
			return db.AgentTaskQueue{}, "", false
		}
	}
	return task, wsID, true
}

// verifyDaemonWorkspaceAccess checks workspace access without writing an HTTP error.
// Used in loops where individual items may be skipped silently.
func (h *Handler) verifyDaemonWorkspaceAccess(r *http.Request, workspaceID string) bool {
	if workspaceID == "" {
		return false
	}
	if daemonWsID := middleware.DaemonWorkspaceIDFromContext(r.Context()); daemonWsID != "" {
		return daemonWsID == workspaceID
	}
	userID := requestUserID(r)
	if userID == "" {
		return false
	}
	if h.MembershipCache.Get(r.Context(), userID, workspaceID) {
		return true
	}
	_, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		return false
	}
	h.MembershipCache.Set(r.Context(), userID, workspaceID)
	return true
}

// ---------------------------------------------------------------------------
// Daemon Registration & Heartbeat
// ---------------------------------------------------------------------------

type DaemonRegisterRequest struct {
	WorkspaceID string `json:"workspace_id"`
	DaemonID    string `json:"daemon_id"`
	// LegacyDaemonIDs lists prior hostname-derived daemon_ids this machine
	// may have registered under before switching to a persistent UUID. The
	// handler merges any matching runtime rows into the new row so agents
	// and tasks keep working without manual intervention.
	LegacyDaemonIDs []string `json:"legacy_daemon_ids"`
	DeviceName      string   `json:"device_name"`
	CLIVersion      string   `json:"cli_version"` // multica CLI version
	LaunchedBy      string   `json:"launched_by"` // "desktop" when spawned by the Electron app
	Runtimes        []struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Version string `json:"version"` // agent CLI version (claude/codex)
		Status  string `json:"status"`
		// ProfileID, when non-empty, marks this as an instance of a custom
		// runtime_profile (MUL-3284). Empty = built-in runtime (legacy path).
		// Type carries the protocol family for both built-in and custom rows
		// so task routing (agent.New) is unchanged.
		ProfileID string `json:"profile_id"`
	} `json:"runtimes"`
}

type daemonWorkspaceReposResponse struct {
	WorkspaceID  string          `json:"workspace_id"`
	Repos        []RepoData      `json:"repos"`
	ReposVersion string          `json:"repos_version"`
	Settings     json.RawMessage `json:"settings,omitempty"`
}

func normalizeWorkspaceRepos(repos []RepoData) []RepoData {
	if len(repos) == 0 {
		return []RepoData{}
	}

	normalized := make([]RepoData, 0, len(repos))
	seen := make(map[string]struct{}, len(repos))
	for _, repo := range repos {
		url := strings.TrimSpace(repo.URL)
		if url == "" {
			continue
		}
		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		normalized = append(normalized, RepoData{URL: url, Description: repo.Description})
	}
	return normalized
}

func workspaceReposVersion(repos []RepoData) string {
	urls := make([]string, 0, len(repos))
	for _, repo := range repos {
		if repo.URL == "" {
			continue
		}
		urls = append(urls, repo.URL)
	}
	sort.Strings(urls)
	sum := sha256.Sum256([]byte(strings.Join(urls, "\n")))
	return hex.EncodeToString(sum[:])
}

func parseWorkspaceRepos(raw []byte) []RepoData {
	if len(raw) == 0 {
		return []RepoData{}
	}

	var repos []RepoData
	if err := json.Unmarshal(raw, &repos); err != nil {
		return []RepoData{}
	}
	return normalizeWorkspaceRepos(repos)
}

func workspaceReposResponse(workspaceID string, raw []byte, settingsRaw []byte) daemonWorkspaceReposResponse {
	repos := parseWorkspaceRepos(raw)
	resp := daemonWorkspaceReposResponse{
		WorkspaceID:  workspaceID,
		Repos:        repos,
		ReposVersion: workspaceReposVersion(repos),
	}
	if len(settingsRaw) > 0 {
		resp.Settings = json.RawMessage(settingsRaw)
	}
	return resp
}

// normalizeProvider canonicalizes a provider string for storage: trimmed and
// lowercased so client-side pricing lookups tolerate case drift. Returns "" for
// a blank input.
func normalizeProvider(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (h *Handler) DaemonRegister(w http.ResponseWriter, r *http.Request) {
	var req DaemonRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.DaemonID = strings.TrimSpace(req.DaemonID)
	req.DeviceName = strings.TrimSpace(req.DeviceName)

	if req.DaemonID == "" {
		writeError(w, http.StatusBadRequest, "daemon_id is required")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if len(req.Runtimes) == 0 {
		writeError(w, http.StatusBadRequest, "at least one runtime is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)

	// Verify workspace access and resolve owner.
	// Daemon tokens (mdt_) prove workspace access directly; OwnerID will be zero
	// (the SQL COALESCE preserves any existing owner on upsert).
	// PAT/JWT tokens require a membership check and set OwnerID from the member.
	var ownerID pgtype.UUID
	if daemonWsID := middleware.DaemonWorkspaceIDFromContext(r.Context()); daemonWsID != "" {
		if daemonWsID != req.WorkspaceID {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		// ownerID stays zero — COALESCE keeps the existing owner on upsert.
	} else {
		member, ok := h.requireWorkspaceMember(w, r, req.WorkspaceID, "workspace not found")
		if !ok {
			return
		}
		ownerID = member.UserID
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	resp := make([]AgentRuntimeResponse, 0, len(req.Runtimes))
	for _, runtime := range req.Runtimes {
		provider := normalizeProvider(runtime.Type)
		if provider == "" {
			provider = "unknown"
		}
		name := strings.TrimSpace(runtime.Name)
		if name == "" {
			name = provider
			if req.DeviceName != "" {
				name = fmt.Sprintf("%s (%s)", provider, req.DeviceName)
			}
		}
		deviceInfo := strings.TrimSpace(req.DeviceName)
		if runtime.Version != "" && deviceInfo != "" {
			deviceInfo = fmt.Sprintf("%s · %s", deviceInfo, runtime.Version)
		} else if runtime.Version != "" {
			deviceInfo = runtime.Version
		}
		status := "online"
		if runtime.Status == "offline" {
			status = "offline"
		}
		metadata, _ := json.Marshal(map[string]any{
			"version":     runtime.Version,
			"cli_version": req.CLIVersion,
			"launched_by": req.LaunchedBy,
		})

		var registered db.AgentRuntime
		var inserted bool
		isCustom := strings.TrimSpace(runtime.ProfileID) != ""

		if isCustom {
			profileUUID, pok := parseUUIDOrBadRequest(w, strings.TrimSpace(runtime.ProfileID), "profile_id")
			if !pok {
				return
			}
			// The profile must exist in this workspace and be enabled. Trust
			// the profile's stored protocol_family over the daemon-sent type so
			// the provider used for task routing cannot drift from the profile.
			profile, perr := h.Queries.GetRuntimeProfileForWorkspace(r.Context(), db.GetRuntimeProfileForWorkspaceParams{
				ID:          profileUUID,
				WorkspaceID: wsUUID,
			})
			if perr != nil {
				writeError(w, http.StatusBadRequest, "unknown runtime profile: "+runtime.ProfileID)
				return
			}
			if !profile.Enabled {
				writeError(w, http.StatusConflict, "runtime profile is disabled: "+runtime.ProfileID)
				return
			}
			provider = profile.ProtocolFamily

			prow, err := h.Queries.UpsertAgentRuntimeWithProfile(r.Context(), db.UpsertAgentRuntimeWithProfileParams{
				WorkspaceID: wsUUID,
				DaemonID:    strToText(req.DaemonID),
				Name:        name,
				RuntimeMode: "local",
				Provider:    provider,
				Status:      status,
				DeviceInfo:  deviceInfo,
				Metadata:    metadata,
				OwnerID:     ownerID,
				ProfileID:   profileUUID,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeError(w, http.StatusNotFound, "runtime not found")
					return
				}
				obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeFailed(
					uuidToString(ownerID),
					req.WorkspaceID,
					req.DaemonID,
					provider,
					"registration_failed",
					"db_error",
					true,
				))
				writeError(w, http.StatusInternalServerError, "failed to register runtime: "+err.Error())
				return
			}
			inserted = prow.Inserted
			registered = db.AgentRuntime{
				ID:             prow.ID,
				WorkspaceID:    prow.WorkspaceID,
				DaemonID:       prow.DaemonID,
				Name:           prow.Name,
				RuntimeMode:    prow.RuntimeMode,
				Provider:       prow.Provider,
				Status:         prow.Status,
				DeviceInfo:     prow.DeviceInfo,
				Metadata:       prow.Metadata,
				LastSeenAt:     prow.LastSeenAt,
				CreatedAt:      prow.CreatedAt,
				UpdatedAt:      prow.UpdatedAt,
				OwnerID:        prow.OwnerID,
				LegacyDaemonID: prow.LegacyDaemonID,
				Visibility:     prow.Visibility,
				ProfileID:      prow.ProfileID,
			}
		} else {
			row, err := h.Queries.UpsertAgentRuntime(r.Context(), db.UpsertAgentRuntimeParams{
				WorkspaceID: wsUUID,
				DaemonID:    strToText(req.DaemonID),
				Name:        name,
				RuntimeMode: "local",
				Provider:    provider,
				Status:      status,
				DeviceInfo:  deviceInfo,
				Metadata:    metadata,
				OwnerID:     ownerID,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeError(w, http.StatusNotFound, "runtime not found")
					return
				}
				obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeFailed(
					uuidToString(ownerID),
					req.WorkspaceID,
					req.DaemonID,
					provider,
					"registration_failed",
					"db_error",
					true,
				))
				writeError(w, http.StatusInternalServerError, "failed to register runtime: "+err.Error())
				return
			}
			inserted = row.Inserted
			registered = db.AgentRuntime{
				ID:             row.ID,
				WorkspaceID:    row.WorkspaceID,
				DaemonID:       row.DaemonID,
				Name:           row.Name,
				RuntimeMode:    row.RuntimeMode,
				Provider:       row.Provider,
				Status:         row.Status,
				DeviceInfo:     row.DeviceInfo,
				Metadata:       row.Metadata,
				LastSeenAt:     row.LastSeenAt,
				CreatedAt:      row.CreatedAt,
				UpdatedAt:      row.UpdatedAt,
				OwnerID:        row.OwnerID,
				LegacyDaemonID: row.LegacyDaemonID,
				Visibility:     row.Visibility,
				ProfileID:      row.ProfileID,
			}
		}

		// Inserted is false for normal daemon reconnects/upserts, so
		// runtime_ready is a first-ready-per-runtime-row signal.
		if inserted {
			obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeRegistered(
				uuidToString(ownerID),
				req.WorkspaceID,
				uuidToString(registered.ID),
				req.DaemonID,
				provider,
				runtime.Version,
				req.CLIVersion,
			))
			if registered.Status == "online" {
				obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeReady(
					uuidToString(ownerID),
					req.WorkspaceID,
					uuidToString(registered.ID),
					req.DaemonID,
					provider,
					0,
				))
			}
		}

		// Seamless migration from the previous hostname-derived identity. The
		// daemon sends every legacy daemon_id it may have registered under
		// (e.g. "host.local", "host", "host-staging"); for each match we
		// reassign agents + tasks onto the new UUID-keyed row, then delete
		// the stale row so there's only ever one runtime per machine.
		//
		// Only built-in runtimes participate: legacy rows predate custom
		// profiles, so a profile-keyed instance never has a hostname-derived
		// ancestor to merge, and mergeLegacyRuntimes scopes by provider alone
		// (no profile_id), which could otherwise fold a built-in row into a
		// custom one of the same provider.
		if !isCustom {
			h.mergeLegacyRuntimes(r, registered, provider, req.LegacyDaemonIDs)
		}

		resp = append(resp, runtimeToResponse(registered))
	}

	slog.Info("daemon registered", "workspace_id", req.WorkspaceID, "daemon_id", req.DaemonID, "runtimes_count", len(resp))

	h.publish(protocol.EventDaemonRegister, req.WorkspaceID, "system", "", map[string]any{
		"action": "register",
	})

	repoResp := workspaceReposResponse(req.WorkspaceID, ws.Repos, ws.Settings)

	writeJSON(w, http.StatusOK, map[string]any{
		"runtimes":      resp,
		"repos":         repoResp.Repos,
		"repos_version": repoResp.ReposVersion,
		"settings":      repoResp.Settings,
	})
}

// mergeLegacyRuntimes folds every runtime row keyed on a prior hostname-derived
// daemon_id into the newly registered UUID-keyed row. For each legacy id the
// lookup is case-insensitive and returns *all* matching rows — case-only drift
// may have already minted duplicates historically (e.g. `Foo.local` AND
// `foo.local` coexisting), and we need to consolidate every one of them, not
// just the first. Per match we reassign agents and tasks, record the legacy
// id on the new row for audit, then delete the stale row.
//
// Scoping by (workspace_id, provider, owner_id) is required: legacy daemon ids
// were hostname-derived, so two users can collide on a host name. A registering
// runtime may only fold legacy rows owned by the same runtime owner. We also
// dedupe across legacy ids so overlapping candidates (e.g. `foo` and `foo.local`
// both resolving to the same stored row) don't double-process.
func (h *Handler) mergeLegacyRuntimes(r *http.Request, registered db.AgentRuntime, provider string, legacyIDs []string) {
	newID := uuidToString(registered.ID)
	merged := make(map[string]struct{})

	for _, legacyID := range legacyIDs {
		legacyID = strings.TrimSpace(legacyID)
		if legacyID == "" {
			continue
		}

		matches, err := h.Queries.FindLegacyRuntimesByDaemonID(r.Context(), db.FindLegacyRuntimesByDaemonIDParams{
			WorkspaceID: registered.WorkspaceID,
			Provider:    provider,
			OwnerID:     registered.OwnerID,
			DaemonID:    legacyID,
		})
		if err != nil {
			slog.Warn("legacy runtime merge: lookup failed", "legacy_daemon_id", legacyID, "error", err)
			continue
		}
		for _, old := range matches {
			oldID := uuidToString(old.ID)
			if oldID == newID {
				continue
			}
			if !registered.OwnerID.Valid || !old.OwnerID.Valid || old.OwnerID != registered.OwnerID {
				slog.Warn("legacy runtime merge: owner mismatch",
					"legacy_daemon_id", legacyID,
					"old_runtime_id", oldID,
					"new_runtime_id", newID,
				)
				continue
			}
			if _, seen := merged[oldID]; seen {
				continue
			}
			merged[oldID] = struct{}{}

			agents, err := h.Queries.ReassignAgentsToRuntime(r.Context(), db.ReassignAgentsToRuntimeParams{
				NewRuntimeID: registered.ID,
				OldRuntimeID: old.ID,
			})
			if err != nil {
				slog.Warn("legacy runtime merge: reassign agents failed", "legacy_daemon_id", legacyID, "old_runtime_id", oldID, "new_runtime_id", newID, "error", err)
				continue
			}
			tasks, err := h.Queries.ReassignTasksToRuntime(r.Context(), db.ReassignTasksToRuntimeParams{
				NewRuntimeID: registered.ID,
				OldRuntimeID: old.ID,
			})
			if err != nil {
				slog.Warn("legacy runtime merge: reassign tasks failed", "legacy_daemon_id", legacyID, "old_runtime_id", oldID, "new_runtime_id", newID, "error", err)
				continue
			}
			if err := h.Queries.RecordRuntimeLegacyDaemonID(r.Context(), db.RecordRuntimeLegacyDaemonIDParams{
				ID:             registered.ID,
				LegacyDaemonID: strToText(legacyID),
			}); err != nil {
				slog.Warn("legacy runtime merge: record legacy daemon_id failed", "legacy_daemon_id", legacyID, "error", err)
			}
			if err := h.Queries.DeleteAgentRuntime(r.Context(), old.ID); err != nil {
				slog.Warn("legacy runtime merge: delete old runtime failed", "old_runtime_id", oldID, "error", err)
				continue
			}

			slog.Info("legacy runtime merged",
				"legacy_daemon_id", legacyID,
				"old_runtime_id", oldID,
				"new_runtime_id", newID,
				"provider", provider,
				"agents_reassigned", agents,
				"tasks_reassigned", tasks,
			)
		}
	}
}

func (h *Handler) GetDaemonWorkspaceRepos(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	writeJSON(w, http.StatusOK, workspaceReposResponse(workspaceID, ws.Repos, ws.Settings))
}

// DaemonDeregister marks runtimes as offline when the daemon shuts down.
func (h *Handler) DaemonDeregister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RuntimeIDs []string `json:"runtime_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.RuntimeIDs) == 0 {
		writeError(w, http.StatusBadRequest, "runtime_ids is required")
		return
	}
	runtimeUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.RuntimeIDs, "runtime_ids")
	if !ok {
		return
	}

	// Track affected workspaces for WS notifications.
	affectedWorkspaces := make(map[string]bool)

	for i, rid := range req.RuntimeIDs {
		// Look up the runtime and verify ownership.
		rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUIDs[i])
		if err != nil {
			slog.Warn("deregister: runtime not found", "runtime_id", rid, "error", err)
			continue
		}

		wsID := uuidToString(rt.WorkspaceID)
		if !h.verifyDaemonWorkspaceAccess(r, wsID) {
			slog.Warn("deregister: workspace mismatch", "runtime_id", rid)
			continue
		}
		if !canAccessDaemonRuntime(r, rt) {
			slog.Warn("deregister: runtime access denied", "runtime_id", rid)
			continue
		}

		if err := h.Queries.SetAgentRuntimeOffline(r.Context(), rt.ID); err != nil {
			slog.Warn("deregister: failed to set offline", "runtime_id", rid, "error", err)
			continue
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.RuntimeOffline(
			uuidToString(rt.OwnerID),
			wsID,
			uuidToString(rt.ID),
			rt.DaemonID.String,
			rt.Provider,
		))

		affectedWorkspaces[wsID] = true
	}

	// Notify frontend clients so they re-fetch runtime list.
	for wsID := range affectedWorkspaces {
		h.publish(protocol.EventDaemonRegister, wsID, "system", "", map[string]any{
			"action": "deregister",
		})
	}

	slog.Info("daemon deregistered", "runtime_ids", req.RuntimeIDs)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type DaemonHeartbeatRequest struct {
	RuntimeID           string `json:"runtime_id"`
	SupportsBatchImport bool   `json:"supports_batch_import,omitempty"`
}

// heartbeatHasPendingTimeout bounds the cheap HasPending probe on the
// heartbeat hot path. Probes are read-only (ZCARD in Redis) so a timeout is
// ack-safe: the worst case is "we didn't find out if anything was queued this
// tick" and the next heartbeat (default 15s later) will try again.
//
// PopPending is deliberately NOT bounded this way — its Redis implementation
// runs a Lua claim script whose ZREM + SET-running side effects cannot be
// cleanly un-run from the client side if the context expires mid-script. We
// therefore only invoke PopPending after HasPending confirms there is work
// to claim, so we never start a claim we might have to abort.
const heartbeatHasPendingTimeout = 1 * time.Second

// maxLocalSkillImportBatch is how many pending import requests the heartbeat
// handler pops per cycle. Higher values let the daemon process more imports
// in parallel but increase per-heartbeat latency.
//
// Timeout invariant: IMPORT_CONCURRENCY (views/.../runtime-local-skill-import-panel.tsx)
// × heartbeat period (~15s) must stay within runtimeLocalSkillPendingTimeout
// (runtime_local_skills.go), and IMPORT_POLL_TIMEOUT_MS (core/runtimes/local-skills.ts)
// must exceed pendingTimeout + runningTimeout.
const maxLocalSkillImportBatch = 10

// runtimeLivenessTTL is how long a Redis liveness record stays valid before
// expiring. The daemon refreshes it every heartbeat (~15s), so this just
// needs to be a few heartbeats long — the value (90s) tolerates ~6 missed
// beats before Redis declares the runtime dead.
//
// It is intentionally shorter than the sweeper's stale threshold (150s in
// cmd/server/runtime_sweeper.go). That ordering is safe and desirable:
// Redis can declare a runtime dead before the DB stale window opens, and
// the sweeper will simply ignore it until the DB column also crosses the
// threshold. The unsafe direction would be the opposite (Redis claiming
// "alive" past the DB stale window, masking a truly dead runtime when the
// sweeper consults Redis as the source of truth) — that cannot happen here.
const runtimeLivenessTTL = 90 * time.Second

// runtimeHeartbeatDBFlushInterval is the maximum staleness we tolerate on
// agent_runtime.last_seen_at while Redis is the active liveness source. When
// last_seen_at gets older than this, the heartbeat path schedules a DB write
// so (a) the UI's "last seen" display stays bounded and (b) the sweeper's
// DB-only fallback path (used when an IsAliveBatch call to Redis errors) does
// not false-positive on alive-but-Redis-only runtimes.
//
// Load-bearing invariant: this must be strictly less than the sweeper's
// stale threshold (150s in cmd/server/runtime_sweeper.go) MINUS one daemon
// heartbeat cycle (~15s) MINUS the BatchedHeartbeatScheduler tick interval
// (~30s). Worst-case DB age for an alive runtime is therefore bounded by
// flush + heartbeat + batchTick = 60 + 15 + 30 = 105s, leaving a 45s buffer
// below the 150s stale window. If you tune any of these constants, recompute
// the chain and keep at least a one-tick buffer.
//
// We intentionally keep the per-runtime flush throttle at 60s (rather than
// pushing it higher) so a crashed runtime is detected within ~150s instead
// of ~10 minutes. The bulk of the DB-pressure win comes from batched
// coalescing in HeartbeatScheduler — at 70 online runtimes that collapses
// ~17 single-row UPDATE/s into ~0.03 bulk UPDATE/s (one per batch tick),
// independent of how the per-runtime throttle is tuned.
const runtimeHeartbeatDBFlushInterval = 60 * time.Second

func (h *Handler) DaemonHeartbeat(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	authPath := middleware.DaemonAuthPathFromContext(r.Context())
	var (
		outcome                                                                                            = "unauth"
		runtimeID                                                                                          string
		decodeMs, runtimeLookupMs, workspaceCheckMs                                                        int64
		authMs, updateMs, probeModelMs, popModelMs, probeSkillsMs, popSkillsMs, probeImportMs, popImportMs int64
		probeModelTimedOut, probeSkillsTimedOut, probeImportTimedOut                                       bool
	)
	defer func() {
		logHeartbeatEndpointSlow(runtimeID, outcome, authPath, start, decodeMs, runtimeLookupMs, workspaceCheckMs, authMs, updateMs, probeModelMs, popModelMs, probeSkillsMs, popSkillsMs, probeImportMs, popImportMs, probeModelTimedOut, probeSkillsTimedOut, probeImportTimedOut)
	}()

	decodeStart := time.Now()
	var req DaemonHeartbeatRequest
	decodeErr := json.NewDecoder(r.Body).Decode(&req)
	decodeMs = time.Since(decodeStart).Milliseconds()
	if decodeErr != nil {
		outcome = "bad_body"
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RuntimeID == "" {
		outcome = "missing_runtime_id"
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	runtimeID = req.RuntimeID

	// Inlined and instrumented version of requireDaemonRuntimeAccess so we
	// can attribute the runtime-lookup and workspace-check sub-stages
	// independently in slow-logs. Together with the auth_path label set by
	// DaemonAuth middleware, this lets us tell whether prod heartbeat tail
	// latency is in pgx pool acquisition (runtime_lookup_ms), in the PAT
	// fallback workspace-membership query (workspace_check_ms), or upstream.
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		outcome = "bad_runtime_id"
		return
	}
	lookupStart := time.Now()
	rt, lookupErr := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	runtimeLookupMs = time.Since(lookupStart).Milliseconds()
	if lookupErr != nil {
		// Only pgx.ErrNoRows means the runtime row is gone. Daemon reads this
		// 404 as a signal to drop the stale runtime locally; treating a
		// transient DB error the same way would force daemons to self-cleanup
		// on a hiccup.
		if isNotFound(lookupErr) {
			outcome = "runtime_not_found"
			writeError(w, http.StatusNotFound, "runtime not found")
			return
		}
		outcome = "runtime_lookup_error"
		slog.Warn("get agent runtime failed", "runtime_id", req.RuntimeID, "error", lookupErr)
		writeError(w, http.StatusInternalServerError, "failed to load runtime")
		return
	}
	wsCheckStart := time.Now()
	wsOK := h.requireDaemonWorkspaceAccess(w, r, uuidToString(rt.WorkspaceID))
	workspaceCheckMs = time.Since(wsCheckStart).Milliseconds()
	if !wsOK {
		outcome = "workspace_denied"
		return
	}
	if !canAccessDaemonRuntime(r, rt) {
		outcome = "runtime_denied"
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	authMs = time.Since(start).Milliseconds()

	ack, m, err := h.processHeartbeat(r.Context(), rt, req.SupportsBatchImport)
	updateMs = m.UpdateMs
	probeModelMs = m.ProbeModelMs
	popModelMs = m.PopModelMs
	probeSkillsMs = m.ProbeSkillsMs
	popSkillsMs = m.PopSkillsMs
	probeImportMs = m.ProbeImportMs
	popImportMs = m.PopImportMs
	probeModelTimedOut = m.ProbeModelTimedOut
	probeSkillsTimedOut = m.ProbeSkillsTimedOut
	probeImportTimedOut = m.ProbeImportTimedOut
	if err != nil {
		outcome = "error_update"
		writeError(w, http.StatusInternalServerError, "heartbeat failed")
		return
	}

	outcome = "ok"
	// Preserve the existing HTTP response shape: the runtime_id field is new
	// in the WS path and would be redundant noise on the HTTP path where the
	// caller already knows which runtime it asked about.
	resp := map[string]any{"status": ack.Status}
	if ack.PendingUpdate != nil {
		resp["pending_update"] = ack.PendingUpdate
	}
	if ack.PendingModelList != nil {
		resp["pending_model_list"] = ack.PendingModelList
	}
	if ack.PendingLocalSkills != nil {
		resp["pending_local_skills"] = ack.PendingLocalSkills
	}
	if ack.PendingLocalSkillImport != nil {
		resp["pending_local_skill_import"] = ack.PendingLocalSkillImport
	}
	if len(ack.PendingLocalSkillImports) > 0 {
		resp["pending_local_skill_imports"] = ack.PendingLocalSkillImports
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleDaemonWSHeartbeat is the daemonws.HeartbeatHandler entry point: it
// resolves the runtime, verifies the connection's workspace owns it, and
// returns the ack payload. It is the WebSocket-side mirror of DaemonHeartbeat.
//
// Workspace authorization is re-checked on every heartbeat instead of trusted
// from the upgrade-time check because runtime ownership can change (e.g. a
// runtime is reassigned to another workspace mid-connection).
//
// When the runtime row is missing (pgx.ErrNoRows), the function returns a
// successful ack with Status=HeartbeatStatusRuntimeGone and RuntimeGone=true
// instead of an error. That keeps the hub from logging every beat at Warn,
// and tells the daemon to drop the stale runtime and re-register. Other DB
// errors still propagate as errors so they keep their existing Warn logging
// and the daemon does not mistake a hiccup for a deletion.
func (h *Handler) HandleDaemonWSHeartbeat(ctx context.Context, identity daemonws.ClientIdentity, runtimeID string, supportsBatchImport bool) (*protocol.DaemonHeartbeatAckPayload, error) {
	runtimeUUID, err := util.ParseUUID(runtimeID)
	if err != nil {
		return nil, fmt.Errorf("invalid runtime_id: %w", err)
	}
	rt, err := h.Queries.GetAgentRuntime(ctx, runtimeUUID)
	if err != nil {
		if isNotFound(err) {
			return &protocol.DaemonHeartbeatAckPayload{
				RuntimeID:   runtimeID,
				Status:      protocol.HeartbeatStatusRuntimeGone,
				RuntimeGone: true,
			}, nil
		}
		return nil, fmt.Errorf("get agent runtime: %w", err)
	}
	if !identity.AllowsWorkspace(uuidToString(rt.WorkspaceID)) {
		return nil, fmt.Errorf("runtime not in connection workspace")
	}
	if !daemonIdentityCanAccessRuntime(identity, rt) {
		return nil, fmt.Errorf("runtime not in connection identity")
	}
	ack, _, err := h.processHeartbeat(ctx, rt, supportsBatchImport)
	return ack, err
}

func daemonIdentityCanAccessRuntime(identity daemonws.ClientIdentity, rt db.AgentRuntime) bool {
	if identity.DaemonID != "" {
		return rt.DaemonID.Valid && rt.DaemonID.String == identity.DaemonID
	}
	if identity.UserID != "" {
		return rt.OwnerID.Valid && uuidToString(rt.OwnerID) == identity.UserID
	}
	return false
}

// recordHeartbeat marks the runtime as alive. When LivenessStore is available
// (Redis configured and reachable) it writes a TTL'd liveness key and skips
// the DB row write on most beats — the DB is only updated on the
// offline→online transition or once per runtimeHeartbeatDBFlushInterval to
// keep last_seen_at fresh enough for the UI and the DB-fallback sweeper.
//
// When LivenessStore is unavailable (no Redis configured) or any Touch call
// errors, recordHeartbeat falls back to writing the DB on every beat — that
// is the original behavior and keeps the sweeper's DB-only path correct.
//
// The actual DB write is delegated to h.HeartbeatScheduler so production can
// coalesce many runtimes' bumps into one bulk UPDATE per tick. See
// heartbeat_scheduler.go for the two implementations.
func (h *Handler) recordHeartbeat(ctx context.Context, rt db.AgentRuntime) error {
	now := time.Now()

	// Decide whether the DB row needs a write *before* touching Redis, so a
	// Touch failure can simply force needDBWrite=true without re-evaluating
	// the structural reasons.
	needDBWrite := !h.LivenessStore.Available() ||
		rt.Status != "online" ||
		!rt.LastSeenAt.Valid ||
		now.Sub(rt.LastSeenAt.Time) >= runtimeHeartbeatDBFlushInterval

	if h.LivenessStore.Available() {
		if err := h.LivenessStore.Touch(ctx, uuidToString(rt.ID), runtimeLivenessTTL); err != nil {
			// Redis hiccup: degrade transparently to the DB-only path for
			// this beat. The sweeper falls back to its DB threshold the
			// same way when IsAliveBatch fails, so end-to-end correctness
			// is preserved.
			slog.Warn("liveness touch failed; falling back to DB heartbeat",
				"runtime_id", uuidToString(rt.ID), "error", err)
			needDBWrite = true
		}
	}

	if !needDBWrite {
		return nil
	}

	// Either bumps last_seen_at on an already-online row (Touch + race
	// fallback) or flips status from offline to online. The scheduler
	// chooses sync vs batched per case; see HeartbeatScheduler doc.
	return h.HeartbeatScheduler.Schedule(ctx, rt)
}

// heartbeatMetrics carries per-stage timings out of processHeartbeat so the
// HTTP slow-log can stay structured. The WS path discards them.
type heartbeatMetrics struct {
	UpdateMs, ProbeModelMs, PopModelMs, ProbeSkillsMs, PopSkillsMs, ProbeImportMs, PopImportMs int64
	ProbeModelTimedOut, ProbeSkillsTimedOut, ProbeImportTimedOut                               bool
}

// processHeartbeat does the work shared by HTTP POST /api/daemon/heartbeat and
// the WebSocket daemon:heartbeat path: records liveness and pulls any pending
// actions queued for the runtime. Auth and request decoding live in the
// caller because they differ between transports.
func (h *Handler) processHeartbeat(ctx context.Context, rt db.AgentRuntime, supportsBatchImport bool) (*protocol.DaemonHeartbeatAckPayload, heartbeatMetrics, error) {
	var m heartbeatMetrics
	runtimeID := uuidToString(rt.ID)

	updateStart := time.Now()
	if err := h.recordHeartbeat(ctx, rt); err != nil {
		m.UpdateMs = time.Since(updateStart).Milliseconds()
		return nil, m, err
	}
	m.UpdateMs = time.Since(updateStart).Milliseconds()

	slog.Debug("daemon heartbeat", "runtime_id", runtimeID)

	ack := &protocol.DaemonHeartbeatAckPayload{
		RuntimeID: runtimeID,
		Status:    "ok",
	}

	probeUpdateCtx, cancelProbeUpdate := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasUpdate, probeUpdateErr := h.UpdateStore.HasPending(probeUpdateCtx, runtimeID)
	cancelProbeUpdate()
	switch {
	case probeUpdateErr == nil && hasUpdate:
		pending, popUpdateErr := h.UpdateStore.PopPending(ctx, runtimeID)
		if popUpdateErr != nil {
			slog.Warn("update PopPending failed", "error", popUpdateErr, "runtime_id", runtimeID)
		} else if pending != nil {
			ack.PendingUpdate = &protocol.DaemonHeartbeatPendingUpdate{
				ID:            pending.ID,
				TargetVersion: pending.TargetVersion,
			}
		}
	case probeUpdateErr != nil:
		if errors.Is(probeUpdateErr, context.DeadlineExceeded) || errors.Is(probeUpdateErr, context.Canceled) {
			slog.Warn("update HasPending timed out", "runtime_id", runtimeID)
		} else {
			slog.Warn("update HasPending failed", "error", probeUpdateErr, "runtime_id", runtimeID)
		}
	}

	// Probe then claim the model list queue. Same pattern as the local-skill
	// queues below — a slow shared store cannot stall the heartbeat on
	// empty-queue ticks, but the claim itself runs unbounded because its
	// Lua side effects cannot be safely aborted mid-script.
	probeModelStart := time.Now()
	probeModelCtx, cancelProbeModel := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasModel, probeModelErr := h.ModelListStore.HasPending(probeModelCtx, runtimeID)
	cancelProbeModel()
	m.ProbeModelMs = time.Since(probeModelStart).Milliseconds()
	switch {
	case probeModelErr == nil && hasModel:
		popStart := time.Now()
		pendingModel, popErr := h.ModelListStore.PopPending(ctx, runtimeID)
		m.PopModelMs = time.Since(popStart).Milliseconds()
		if popErr != nil {
			slog.Warn("model list PopPending failed", "error", popErr, "runtime_id", runtimeID)
		} else if pendingModel != nil {
			ack.PendingModelList = &protocol.DaemonHeartbeatPendingModelList{ID: pendingModel.ID}
		}
	case probeModelErr != nil:
		if errors.Is(probeModelErr, context.DeadlineExceeded) || errors.Is(probeModelErr, context.Canceled) {
			m.ProbeModelTimedOut = true
			slog.Warn("model list HasPending timed out", "runtime_id", runtimeID, "elapsed_ms", m.ProbeModelMs)
		} else {
			slog.Warn("model list HasPending failed", "error", probeModelErr, "runtime_id", runtimeID)
		}
	}

	// Probe then claim the local-skill list queue. The probe is bounded so a
	// slow shared store cannot stall the heartbeat on empty-queue ticks; the
	// claim runs unbounded (it inherits only ctx) because its Lua side
	// effects cannot be safely aborted mid-script.
	probeSkillsStart := time.Now()
	probeSkillsCtx, cancelProbeSkills := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasSkills, probeErr := h.LocalSkillListStore.HasPending(probeSkillsCtx, runtimeID)
	cancelProbeSkills()
	m.ProbeSkillsMs = time.Since(probeSkillsStart).Milliseconds()
	switch {
	case probeErr == nil && hasSkills:
		popStart := time.Now()
		pendingSkills, popErr := h.LocalSkillListStore.PopPending(ctx, runtimeID)
		m.PopSkillsMs = time.Since(popStart).Milliseconds()
		if popErr != nil {
			slog.Warn("local skill list PopPending failed", "error", popErr, "runtime_id", runtimeID)
		} else if pendingSkills != nil {
			ack.PendingLocalSkills = &protocol.DaemonHeartbeatPendingLocalSkills{ID: pendingSkills.ID}
		}
	case probeErr != nil:
		if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(probeErr, context.Canceled) {
			m.ProbeSkillsTimedOut = true
			slog.Warn("local skill list HasPending timed out", "runtime_id", runtimeID, "elapsed_ms", m.ProbeSkillsMs)
		} else {
			slog.Warn("local skill list HasPending failed", "error", probeErr, "runtime_id", runtimeID)
		}
	}

	probeImportStart := time.Now()
	probeImportCtx, cancelProbeImport := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasImport, probeErr := h.LocalSkillImportStore.HasPending(probeImportCtx, runtimeID)
	cancelProbeImport()
	m.ProbeImportMs = time.Since(probeImportStart).Milliseconds()
	switch {
	case probeErr == nil && hasImport:
		popStart := time.Now()
		if supportsBatchImport {
			pendingImports, popErr := h.LocalSkillImportStore.PopPendingBatch(ctx, runtimeID, maxLocalSkillImportBatch)
			m.PopImportMs = time.Since(popStart).Milliseconds()
			if popErr != nil {
				slog.Warn("local skill import PopPendingBatch failed", "error", popErr, "runtime_id", runtimeID, "claimed", len(pendingImports))
			}
			// Always dispatch whatever was claimed — even on partial
			// failure the claimed requests have already transitioned to
			// running in the store. Dropping them here would leave them
			// stranded until the running timeout.
			if len(pendingImports) > 0 {
				// Backwards compat: singular field carries the first item so
				// old daemons that don't know the plural field still get one.
				ack.PendingLocalSkillImport = &protocol.DaemonHeartbeatPendingLocalSkillImport{
					ID:       pendingImports[0].ID,
					SkillKey: pendingImports[0].SkillKey,
				}
				batch := make([]protocol.DaemonHeartbeatPendingLocalSkillImport, 0, len(pendingImports))
				for _, p := range pendingImports {
					batch = append(batch, protocol.DaemonHeartbeatPendingLocalSkillImport{
						ID:       p.ID,
						SkillKey: p.SkillKey,
					})
				}
				ack.PendingLocalSkillImports = batch
			}
		} else {
			pendingImport, popErr := h.LocalSkillImportStore.PopPending(ctx, runtimeID)
			m.PopImportMs = time.Since(popStart).Milliseconds()
			if popErr != nil {
				slog.Warn("local skill import PopPending failed", "error", popErr, "runtime_id", runtimeID)
			} else if pendingImport != nil {
				ack.PendingLocalSkillImport = &protocol.DaemonHeartbeatPendingLocalSkillImport{
					ID:       pendingImport.ID,
					SkillKey: pendingImport.SkillKey,
				}
			}
		}
	case probeErr != nil:
		if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(probeErr, context.Canceled) {
			m.ProbeImportTimedOut = true
			slog.Warn("local skill import HasPending timed out", "runtime_id", runtimeID, "elapsed_ms", m.ProbeImportMs)
		} else {
			slog.Warn("local skill import HasPending failed", "error", probeErr, "runtime_id", runtimeID)
		}
	}

	return ack, m, nil
}

// logHeartbeatEndpointSlow emits one structured log when /api/daemon/heartbeat
// exceeds 500ms, splitting auth / update / probe / pop phases for both queues
// so the prod tail can be attributed without flooding logs at normal rates.
// auth_ms is further decomposed into decode_ms, runtime_lookup_ms, and
// workspace_check_ms; auth_path labels which token kind authenticated the
// request ("daemon_token", "pat", or "jwt"). Mirrors logClaimEndpointSlow.
func logHeartbeatEndpointSlow(runtimeID, outcome, authPath string, start time.Time, decodeMs, runtimeLookupMs, workspaceCheckMs, authMs, updateMs, probeModelMs, popModelMs, probeSkillsMs, popSkillsMs, probeImportMs, popImportMs int64, probeModelTimedOut, probeSkillsTimedOut, probeImportTimedOut bool) {
	totalMs := time.Since(start).Milliseconds()
	if totalMs < 500 && !probeModelTimedOut && !probeSkillsTimedOut && !probeImportTimedOut {
		return
	}
	slog.Info("heartbeat_endpoint slow",
		"runtime_id", runtimeID,
		"outcome", outcome,
		"auth_path", authPath,
		"total_ms", totalMs,
		"auth_ms", authMs,
		"decode_ms", decodeMs,
		"runtime_lookup_ms", runtimeLookupMs,
		"workspace_check_ms", workspaceCheckMs,
		"update_ms", updateMs,
		"probe_model_ms", probeModelMs,
		"pop_model_ms", popModelMs,
		"probe_skills_ms", probeSkillsMs,
		"pop_skills_ms", popSkillsMs,
		"probe_import_ms", probeImportMs,
		"pop_import_ms", popImportMs,
		"probe_model_timed_out", probeModelTimedOut,
		"probe_skills_timed_out", probeSkillsTimedOut,
		"probe_import_timed_out", probeImportTimedOut,
	)
}

// logClaimEndpointSlow emits one structured log when the /tasks/claim endpoint
// exceeds 500ms, splitting auth / claim / response-build phases so the prod
// tail can be diagnosed without flooding logs at normal poll rates.
func logClaimEndpointSlow(runtimeID, outcome string, start time.Time, authMs, claimMs, buildMs int64) {
	totalMs := time.Since(start).Milliseconds()
	if totalMs < 500 {
		return
	}
	slog.Info("claim_endpoint slow",
		"runtime_id", runtimeID,
		"outcome", outcome,
		"total_ms", totalMs,
		"auth_ms", authMs,
		"claim_ms", claimMs,
		"build_ms", buildMs,
	)
}

// filterProjectResourcesForRuntime wraps listProjectResourcesForProject. The
// current runtime-decoupled design treats local_directory.daemon_id as
// metadata, not as a routing filter: runtime resolution has already selected
// which daemon is allowed to claim the task, and the daemon should receive the
// project context the user attached.
func (h *Handler) filterProjectResourcesForRuntime(ctx context.Context, projectID pgtype.UUID, runtime db.AgentRuntime) []db.ProjectResource {
	rows := h.listProjectResourcesForProject(ctx, projectID)
	if len(rows) == 0 {
		return nil
	}
	return rows
}

// ClaimTaskByRuntime atomically claims the next queued task for a runtime.
// The response includes the agent's name and skills, fetched fresh from the DB.
func (h *Handler) ClaimTaskByRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	start := time.Now()

	var (
		outcome                  = "unauth"
		authMs, claimMs, buildMs int64
		buildStart               time.Time
	)
	defer func() {
		// Emit at function exit so error / unauth paths also carry timing.
		// build_ms is computed from buildStart only when we entered the
		// response-build phase (otherwise stays 0).
		if !buildStart.IsZero() {
			buildMs = time.Since(buildStart).Milliseconds()
		}
		logClaimEndpointSlow(runtimeID, outcome, start, authMs, claimMs, buildMs)
	}()

	// Verify the caller owns this runtime's workspace. The runtime's
	// workspace_id is the authoritative value a claimed task must match
	// below — a task whose resolved workspace doesn't equal this runtime's
	// workspace is rejected even if it was enqueued against this
	// runtime_id (defense-in-depth against upstream routing bugs).
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	runtimeWorkspaceID := uuidToString(runtime.WorkspaceID)
	authMs = time.Since(start).Milliseconds()

	claimStart := time.Now()
	task, err := h.TaskService.ClaimTaskForRuntime(r.Context(), parseUUID(runtimeID))
	claimMs = time.Since(claimStart).Milliseconds()
	if err != nil {
		outcome = "error_claim"
		writeError(w, http.StatusInternalServerError, "failed to claim task: "+err.Error())
		return
	}

	if task == nil {
		slog.Debug("no task to claim", "runtime_id", runtimeID)
		writeJSON(w, http.StatusOK, map[string]any{"task": nil})
		outcome = "no_task"
		return
	}

	outcome = "claimed"
	buildStart = time.Now()

	// Build response with fresh agent data (name + skills + custom_env + custom_args).
	resp := taskToResponse(*task, runtimeWorkspaceID)
	if agent, err := h.Queries.GetAgent(r.Context(), task.AgentID); err == nil {
		// Workspace-bound skills first, then platform built-in skills. Built-in
		// names carry a "multica-" prefix so their on-disk slugs never collide
		// with a user-authored workspace skill (see writeSkillFiles).
		skills := h.TaskService.LoadAgentSkills(r.Context(), task.AgentID)
		skills = append(skills, h.TaskService.BuiltinSkills()...)
		var customEnv map[string]string
		if agent.CustomEnv != nil {
			if err := json.Unmarshal(agent.CustomEnv, &customEnv); err != nil {
				slog.Warn("failed to unmarshal agent custom_env", "agent_id", uuidToString(agent.ID), "error", err)
			}
		}
		var customArgs []string
		if agent.CustomArgs != nil {
			if err := json.Unmarshal(agent.CustomArgs, &customArgs); err != nil {
				slog.Warn("failed to unmarshal agent custom_args", "agent_id", uuidToString(agent.ID), "error", err)
			}
		}
		var mcpConfig json.RawMessage
		if agent.McpConfig != nil {
			mcpConfig = json.RawMessage(agent.McpConfig)
		}
		// runtime_config is stored as JSONB and may legitimately be the
		// empty object `{}` for agents that haven't opted into any
		// provider-specific tuning. Forward only non-empty payloads so the
		// daemon's per-provider decoders treat absent-or-empty identically.
		var runtimeConfig json.RawMessage
		if rc := bytes.TrimSpace(agent.RuntimeConfig); len(rc) > 0 && !bytes.Equal(rc, []byte("{}")) && !bytes.Equal(rc, []byte("null")) {
			runtimeConfig = json.RawMessage(agent.RuntimeConfig)
		}
		resp.Agent = &TaskAgentData{
			ID:            uuidToString(agent.ID),
			Name:          agent.Name,
			Instructions:  agent.Instructions,
			Skills:        skills,
			CustomEnv:     customEnv,
			CustomArgs:    customArgs,
			McpConfig:     mcpConfig,
			Model:         agent.Model.String,
			ThinkingLevel: agent.ThinkingLevel.String,
			RuntimeConfig: runtimeConfig,
		}
	}

	// Resolve the runtime owner's profile description so the daemon can
	// inject "## Requesting User" into the brief. Empty fields short-circuit
	// the heading entirely on the daemon side; cloud / system runtimes with
	// no owner stay anonymous. Failure here must not block claim — the agent
	// can still run without the user-context section.
	if runtime.OwnerID.Valid {
		if owner, err := h.Queries.GetUser(r.Context(), runtime.OwnerID); err == nil {
			resp.RequestingUserName = owner.Name
			resp.RequestingUserProfileDescription = owner.ProfileDescription
		} else {
			slog.Debug("failed to load runtime owner for brief injection",
				"runtime_id", runtimeID,
				"owner_id", uuidToString(runtime.OwnerID),
				"error", err,
			)
		}
	}

	// Stored task initiator: chat tasks persist the real message sender at
	// enqueue time (web: request user; Lark: inbound sender — NOT the chat
	// session creator, which for Lark groups is the installer). When set, it is
	// the authoritative initiator for this run; resolve the live name/email so
	// the daemon can render `## Task Initiator`. Comment-triggered tasks instead
	// resolve their initiator from the triggering comment's author below; the
	// two paths are mutually exclusive (a task is either chat or issue-bound).
	// See MUL-2645.
	if task.InitiatorUserID.Valid {
		resp.InitiatorType = "member"
		resp.InitiatorID = uuidToString(task.InitiatorUserID)
		if u, err := h.Queries.GetUser(r.Context(), task.InitiatorUserID); err == nil {
			resp.InitiatorName = u.Name
			resp.InitiatorEmail = u.Email
		}
	}

	// Include workspace ID and repos so the daemon can set up worktrees.
	//
	// Repo precedence: project-bound github_repo resources override workspace
	// repos when present. Mixing both would just confuse the agent — if a
	// project explicitly attached its repos, those are the authoritative set
	// for issues inside that project. When the project has no github_repo
	// resources (or no project at all), we fall back to the workspace repos.
	var issueWorkspaceID pgtype.UUID
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(r.Context(), task.IssueID); err == nil {
			issueWorkspaceID = issue.WorkspaceID
			resp.WorkspaceID = uuidToString(issue.WorkspaceID)
			resp.ThreadName = issue.Title

			// Squad-leader briefing injection: when the issue is assigned
			// to a squad and the claiming agent is that squad's current
			// leader, append a full briefing (Operating Protocol + Roster
			// + user Instructions) to the agent's own Instructions. We
			// append (not replace) so per-agent instructions remain
			// authoritative for general behavior; the squad briefing
			// stacks on top as task-specific squad context.
			if resp.Agent != nil && issue.AssigneeType.Valid && issue.AssigneeType.String == "squad" && issue.AssigneeID.Valid {
				if squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
					ID:          issue.AssigneeID,
					WorkspaceID: issue.WorkspaceID,
				}); err == nil && uuidToString(squad.LeaderID) == resp.Agent.ID {
					briefing := buildSquadLeaderBriefing(r.Context(), h.Queries, squad)
					if strings.TrimSpace(resp.Agent.Instructions) == "" {
						resp.Agent.Instructions = briefing
					} else {
						resp.Agent.Instructions = resp.Agent.Instructions + "\n\n" + briefing
					}
					slog.Debug("injected squad leader briefing",
						"squad_id", uuidToString(squad.ID),
						"squad_name", squad.Name,
						"leader_agent_id", resp.Agent.ID,
					)
				}
			}

			var projectRepos []RepoData
			if issue.ProjectID.Valid {
				resp.ProjectID = uuidToString(issue.ProjectID)
				if proj, err := h.Queries.GetProject(r.Context(), issue.ProjectID); err == nil {
					resp.ProjectTitle = proj.Title
				}
				if rows := h.filterProjectResourcesForRuntime(r.Context(), issue.ProjectID, runtime); len(rows) > 0 {
					out := make([]ProjectResourceData, 0, len(rows))
					for _, row := range rows {
						label := ""
						if row.Label.Valid {
							label = row.Label.String
						}
						ref := json.RawMessage(row.ResourceRef)
						if len(ref) == 0 {
							ref = json.RawMessage("{}")
						}
						out = append(out, ProjectResourceData{
							ID:           uuidToString(row.ID),
							ResourceType: row.ResourceType,
							ResourceRef:  ref,
							Label:        label,
						})
						// Lift github_repo resources into the daemon's repo list
						// so `multica repo checkout` and the meta-skill render
						// them as the issue's repos.
						if row.ResourceType == "github_repo" {
							var payload struct {
								URL string `json:"url"`
							}
							if json.Unmarshal(row.ResourceRef, &payload) == nil && payload.URL != "" {
								projectRepos = append(projectRepos, RepoData{URL: payload.URL})
							}
						}
					}
					resp.ProjectResources = out
				}
			}

			if len(projectRepos) > 0 {
				resp.Repos = projectRepos
			} else if ws, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID); err == nil && ws.Repos != nil {
				var repos []RepoData
				if json.Unmarshal(ws.Repos, &repos) == nil && len(repos) > 0 {
					resp.Repos = repos
				}
			}
		}

		// Fetch the triggering comment content so the daemon can embed it
		// directly in the agent prompt (prevents the agent from ignoring comments
		// when stale output files exist in a reused workdir). Also surface the
		// comment author's kind and display name so the agent knows whether it
		// was triggered by a human or by another agent — a signal used by the
		// harness instructions to avoid mention loops between agents.
		if task.TriggerCommentID.Valid {
			if comment, err := h.Queries.GetComment(r.Context(), task.TriggerCommentID); err == nil {
				resp.TriggerCommentContent = comment.Content
				resp.TriggerThreadID = uuidToString(comment.ID)
				if comment.ParentID.Valid {
					resp.TriggerThreadID = uuidToString(comment.ParentID)
				}
				resp.TriggerAuthorType = comment.AuthorType
				// The triggering comment's author is the task initiator — the
				// real requester behind this run. Surface it (type + id + name,
				// plus email for members) so a workspace-visible agent can
				// attribute the request to the right person instead of to the
				// runtime owner. Same lookups as the display name above; we just
				// also capture the id and email. See MUL-2645.
				resp.InitiatorType = comment.AuthorType
				if comment.AuthorID.Valid {
					resp.InitiatorID = uuidToString(comment.AuthorID)
				}
				switch comment.AuthorType {
				case "agent":
					if comment.AuthorID.Valid {
						if a, err := h.Queries.GetAgent(r.Context(), comment.AuthorID); err == nil {
							resp.TriggerAuthorName = a.Name
							resp.InitiatorName = a.Name
						}
					}
				case "member":
					// For member-authored comments, AuthorID is a user UUID
					// (see handler.resolveActor) — look up the user's display name.
					if comment.AuthorID.Valid {
						if u, err := h.Queries.GetUser(r.Context(), comment.AuthorID); err == nil {
							resp.TriggerAuthorName = u.Name
							resp.InitiatorName = u.Name
							resp.InitiatorEmail = u.Email
						}
					}
				}
				// Count comments that arrived issue-wide since this agent's last
				// run, so the daemon can tell it the full catch-up volume up front
				// (the prompt then steers it to read the triggering thread first).
				// Anchor = the prior task's started_at (never completed_at: a long
				// run would miss comments posted while it ran). Cold start (no prior
				// task) → no anchor → no hint. Excludes the agent's own comments and
				// the triggering comment itself because that body is already
				// injected into the prompt. Best-effort: any DB error or zero count
				// leaves the hint suppressed.
				if startedAt, err := h.Queries.GetLastTaskStartedAtForIssueAndAgent(r.Context(), db.GetLastTaskStartedAtForIssueAndAgentParams{
					AgentID: task.AgentID,
					IssueID: comment.IssueID,
				}); err == nil && startedAt.Valid {
					if cnt, err := h.Queries.CountNewCommentsSince(r.Context(), db.CountNewCommentsSinceParams{
						AnchorID:    task.TriggerCommentID,
						IssueID:     comment.IssueID,
						WorkspaceID: comment.WorkspaceID,
						Since:       startedAt,
						AuthorID:    task.AgentID,
					}); err == nil && cnt > 0 {
						resp.NewCommentCount = int(cnt)
						resp.NewCommentsSince = startedAt.Time.UTC().Format(time.RFC3339)
					}
				}
			}
		}

		// Look up the prior session for this (agent, issue) pair so the daemon
		// can resume the Claude Code conversation context.
		//
		// Skip all prior state when the task was flagged as a manual rerun:
		// the user just judged the prior output bad, so the daemon must start a
		// fresh agent session in a fresh workdir instead of resuming anything
		// from the same conversation that produced that output.
		if !task.ForceFreshSession {
			if prior, err := h.Queries.GetLastTaskSession(r.Context(), db.GetLastTaskSessionParams{
				AgentID: task.AgentID,
				IssueID: task.IssueID,
			}); err == nil && prior.SessionID.Valid {
				// Resume the prior session when it ran on the same runtime —
				// including comment-triggered follow-ups, so the agent keeps the
				// issue's conversation context across turns. The "Focus on THIS
				// comment" guard in prompt.go defends against inheriting the prior
				// turn's "Done." marker, and GetLastTaskSession already excludes
				// poisoned sessions.
				if prior.RuntimeID == task.RuntimeID {
					resp.PriorSessionID = prior.SessionID.String
				}
				if prior.WorkDir.Valid {
					resp.PriorWorkDir = prior.WorkDir.String
				}
			}
			if resp.PriorWorkDir == "" && issueWorkspaceID.Valid {
				if issuePrior, err := h.Queries.GetLastGithubRepoIssueWorkDir(r.Context(), db.GetLastGithubRepoIssueWorkDirParams{
					IssueID:     task.IssueID,
					WorkspaceID: issueWorkspaceID,
					RuntimeID:   task.RuntimeID,
				}); err == nil && issuePrior.WorkDir.Valid {
					resp.PriorWorkDir = issuePrior.WorkDir.String
				}
			}
		}
	}

	// Chat task: populate workspace/session info from the chat_session table.
	if task.ChatSessionID.Valid {
		if cs, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID); err == nil {
			resp.WorkspaceID = uuidToString(cs.WorkspaceID)
			resp.ChatSessionID = uuidToString(cs.ID)
			resp.ThreadName = cs.Title
			if ws, err := h.Queries.GetWorkspace(r.Context(), cs.WorkspaceID); err == nil && ws.Repos != nil {
				var repos []RepoData
				if json.Unmarshal(ws.Repos, &repos) == nil && len(repos) > 0 {
					resp.Repos = repos
				}
			}
			if !task.ForceFreshSession {
				// Resume chat sessions only when the stored pointer was produced
				// by the same runtime as the claiming task. When the chat_session
				// pointer is missing (legacy NULL runtime_id), stale (last task
				// failed before reporting completion), or runtime-mismatched, fall
				// back to the most recent task row that recorded a session_id —
				// otherwise a single failed turn would silently drop the entire
				// conversation memory on the next message. The fallback also
				// requires runtime to match.
				if cs.SessionID.Valid && cs.RuntimeID.Valid && cs.RuntimeID == task.RuntimeID {
					resp.PriorSessionID = cs.SessionID.String
				}
				if cs.WorkDir.Valid {
					resp.PriorWorkDir = cs.WorkDir.String
				}
				if prior, err := h.Queries.GetLastChatTaskSession(r.Context(), cs.ID); err == nil && prior.SessionID.Valid {
					if resp.PriorSessionID == "" && prior.RuntimeID == task.RuntimeID {
						resp.PriorSessionID = prior.SessionID.String
					}
					if prior.WorkDir.Valid && resp.PriorWorkDir == "" {
						resp.PriorWorkDir = prior.WorkDir.String
					}
				}
			}
			// Build the chat prompt from EVERY user message that has arrived
			// since the agent's last reply — not just the most recent one. A
			// short-window debounce (MUL-2968) can land several user messages
			// before a single run fires; the agent resumes its prior session
			// and only learns of new input through resp.ChatMessage, so
			// delivering just the latest message would silently drop the
			// earlier ones (e.g. "看上海天气" then "还有青岛" → only Qingdao
			// answered). The unanswered set is the trailing run of user
			// messages after the last assistant message (every completed or
			// failed run writes an assistant row, so that anchor advances each
			// turn). Attachments are collected from each included message so
			// the agent can `multica attachment download <id>` — the markdown
			// URL alone is signed and 30-min expiring on the private CDN.
			if msgs, err := h.Queries.ListChatMessages(r.Context(), cs.ID); err == nil && len(msgs) > 0 {
				unanswered := trailingUserMessages(msgs)
				parts := make([]string, 0, len(unanswered))
				for _, m := range unanswered {
					if strings.TrimSpace(m.Content) != "" {
						parts = append(parts, m.Content)
					}
					if atts, attErr := h.Queries.ListAttachmentsByChatMessage(r.Context(), db.ListAttachmentsByChatMessageParams{
						ChatMessageID: m.ID,
						WorkspaceID:   parseUUID(resp.WorkspaceID),
					}); attErr == nil && len(atts) > 0 {
						for _, a := range atts {
							resp.ChatMessageAttachments = append(resp.ChatMessageAttachments, ChatAttachmentMeta{
								ID:          uuidToString(a.ID),
								Filename:    a.Filename,
								ContentType: a.ContentType,
							})
						}
					}
				}
				resp.ChatMessage = strings.Join(parts, "\n\n")
				if strings.TrimSpace(resp.ThreadName) == "" {
					resp.ThreadName = resp.ChatMessage
				}
			}

			// If the chat session is linked to an issue, load its project
			// resources (local_directory, repos, etc.) so the daemon can
			// write code in the correct local directory.
			if cs.IssueID.Valid {
				if issue, err := h.Queries.GetIssue(r.Context(), cs.IssueID); err == nil && issue.ProjectID.Valid {
					resp.ProjectID = uuidToString(issue.ProjectID)
					if proj, err := h.Queries.GetProject(r.Context(), issue.ProjectID); err == nil {
						resp.ProjectTitle = proj.Title
					}
					if rows := h.filterProjectResourcesForRuntime(r.Context(), issue.ProjectID, runtime); len(rows) > 0 {
						out := make([]ProjectResourceData, 0, len(rows))
						for _, row := range rows {
							label := ""
							if row.Label.Valid {
								label = row.Label.String
							}
							ref := json.RawMessage(row.ResourceRef)
							if len(ref) == 0 {
								ref = json.RawMessage("{}")
							}
							out = append(out, ProjectResourceData{
								ID:           uuidToString(row.ID),
								ResourceType: row.ResourceType,
								ResourceRef:  ref,
								Label:        label,
							})
							if row.ResourceType == "github_repo" {
								var payload struct {
									URL string `json:"url"`
								}
								if json.Unmarshal(row.ResourceRef, &payload) == nil && payload.URL != "" {
									resp.Repos = append(resp.Repos, RepoData{URL: payload.URL})
								}
							}
						}
						resp.ProjectResources = out
					}
				}
			}
		}

	}

	// Autopilot run_only task: resolve workspace from autopilot_run →
	// autopilot, and include the autopilot instructions because there is no
	// issue for the agent to fetch.
	if task.AutopilotRunID.Valid {
		if run, err := h.Queries.GetAutopilotRun(r.Context(), task.AutopilotRunID); err == nil {
			resp.AutopilotID = uuidToString(run.AutopilotID)
			resp.AutopilotSource = run.Source
			if run.TriggerPayload != nil {
				resp.AutopilotTriggerPayload = json.RawMessage(run.TriggerPayload)
			}
			if ap, err := h.Queries.GetAutopilot(r.Context(), run.AutopilotID); err == nil {
				resp.AutopilotTitle = ap.Title
				resp.ThreadName = ap.Title
				if ap.Description.Valid {
					resp.AutopilotDescription = ap.Description.String
				}
				if resp.WorkspaceID == "" {
					resp.WorkspaceID = uuidToString(ap.WorkspaceID)
				}
				if len(resp.Repos) == 0 {
					if ws, err := h.Queries.GetWorkspace(r.Context(), ap.WorkspaceID); err == nil && ws.Repos != nil {
						var repos []RepoData
						if json.Unmarshal(ws.Repos, &repos) == nil && len(repos) > 0 {
							resp.Repos = repos
						}
					}
				}

				// If the autopilot run is linked to an issue, load its project
				// resources (local_directory, repos, etc.) so the daemon can
				// write code in the correct local directory.
				if run.IssueID.Valid {
					if issue, err := h.Queries.GetIssue(r.Context(), run.IssueID); err == nil && issue.ProjectID.Valid {
						resp.ProjectID = uuidToString(issue.ProjectID)
						if proj, err := h.Queries.GetProject(r.Context(), issue.ProjectID); err == nil {
							resp.ProjectTitle = proj.Title
						}
						if rows := h.filterProjectResourcesForRuntime(r.Context(), issue.ProjectID, runtime); len(rows) > 0 {
							out := make([]ProjectResourceData, 0, len(rows))
							for _, row := range rows {
								label := ""
								if row.Label.Valid {
									label = row.Label.String
								}
								ref := json.RawMessage(row.ResourceRef)
								if len(ref) == 0 {
									ref = json.RawMessage("{}")
								}
								out = append(out, ProjectResourceData{
									ID:           uuidToString(row.ID),
									ResourceType: row.ResourceType,
									ResourceRef:  ref,
									Label:        label,
								})
								if row.ResourceType == "github_repo" {
									var payload struct {
										URL string `json:"url"`
									}
									if json.Unmarshal(row.ResourceRef, &payload) == nil && payload.URL != "" {
										resp.Repos = append(resp.Repos, RepoData{URL: payload.URL})
									}
								}
							}
							resp.ProjectResources = out
						}
					}
				}
			}
		}
	}

	// Quick-create task: no issue / chat / autopilot link — workspace and
	// prompt come from the task's context JSONB. Resolve workspace from
	// there so the isolation check below has something to compare.
	hasQuickCreate := false
	if task.Context != nil && !task.IssueID.Valid && !task.ChatSessionID.Valid && !task.AutopilotRunID.Valid {
		var qc service.QuickCreateContext
		if json.Unmarshal(task.Context, &qc) == nil && qc.Type == service.QuickCreateContextType {
			hasQuickCreate = true
			resp.QuickCreatePrompt = qc.Prompt
			resp.QuickCreateAttachmentIDs = append([]string(nil), qc.AttachmentIDs...)
			resp.ThreadName = qc.Prompt
			resp.WorkspaceID = qc.WorkspaceID

			// When the user picked a project in the modal, surface its title
			// and resources to the daemon so the agent has the same context
			// it would for an issue-bound task: the prompt template can name
			// the project, and `multica repo checkout` sees the project's
			// github_repo resources instead of the workspace fallback.
			var projectRepos []RepoData
			if qc.ProjectID != "" {
				projectUUID, err := util.ParseUUID(qc.ProjectID)
				if err == nil {
					resp.ProjectID = qc.ProjectID
					if proj, err := h.Queries.GetProject(r.Context(), projectUUID); err == nil {
						resp.ProjectTitle = proj.Title
					}
					if rows := h.filterProjectResourcesForRuntime(r.Context(), projectUUID, runtime); len(rows) > 0 {
						out := make([]ProjectResourceData, 0, len(rows))
						for _, row := range rows {
							label := ""
							if row.Label.Valid {
								label = row.Label.String
							}
							ref := json.RawMessage(row.ResourceRef)
							if len(ref) == 0 {
								ref = json.RawMessage("{}")
							}
							out = append(out, ProjectResourceData{
								ID:           uuidToString(row.ID),
								ResourceType: row.ResourceType,
								ResourceRef:  ref,
								Label:        label,
							})
							if row.ResourceType == "github_repo" {
								var payload struct {
									URL string `json:"url"`
								}
								if json.Unmarshal(row.ResourceRef, &payload) == nil && payload.URL != "" {
									projectRepos = append(projectRepos, RepoData{URL: payload.URL})
								}
							}
						}
						resp.ProjectResources = out
					}
				}
			}

			if len(projectRepos) > 0 {
				resp.Repos = projectRepos
			} else if ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(qc.WorkspaceID)); err == nil && ws.Repos != nil {
				var repos []RepoData
				if json.Unmarshal(ws.Repos, &repos) == nil && len(repos) > 0 {
					resp.Repos = repos
				}
			}

			// Parent-issue resolution for quick-create tasks opened from
			// "Add sub issue". The handler already verified workspace
			// membership at submit time; here we re-fetch to pull the
			// human-readable identifier (e.g. MUL-123) the agent will
			// reference in the prompt. If the parent was deleted between
			// submit and claim we surface the UUID anyway — the agent
			// still passes `--parent <uuid>` and the server-side create
			// will fail loud, which is a better outcome than silently
			// dropping the sub-issue intent.
			if qc.ParentIssueID != "" {
				resp.ParentIssueID = qc.ParentIssueID
				if parentUUID, err := util.ParseUUID(qc.ParentIssueID); err == nil {
					if wsUUID, wsErr := util.ParseUUID(qc.WorkspaceID); wsErr == nil {
						parent, perr := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
							ID:          parentUUID,
							WorkspaceID: wsUUID,
						})
						if perr == nil && parent.ID.Valid {
							if ws, werr := h.Queries.GetWorkspace(r.Context(), wsUUID); werr == nil {
								resp.ParentIssueIdentifier = ws.IssuePrefix + "-" + strconv.Itoa(int(parent.Number))
							}
						}
					}
				}
			}

			// Squad-leader briefing injection for quick-create tasks. When
			// the user picked a squad in the modal, the task runs on the
			// squad's leader agent (resolved by the handler). Surface the
			// same Operating Protocol + Roster + user Instructions that
			// issue-bound squad tasks see, so the leader can decide to
			// delegate before opening the issue.
			if resp.Agent != nil && qc.SquadID != "" {
				wsUUID, wsErr := util.ParseUUID(qc.WorkspaceID)
				squadUUID, sqErr := util.ParseUUID(qc.SquadID)
				if wsErr == nil && sqErr == nil {
					if squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
						ID:          squadUUID,
						WorkspaceID: wsUUID,
					}); err == nil && uuidToString(squad.LeaderID) == resp.Agent.ID {
						briefing := buildSquadLeaderBriefing(r.Context(), h.Queries, squad)
						if strings.TrimSpace(resp.Agent.Instructions) == "" {
							resp.Agent.Instructions = briefing
						} else {
							resp.Agent.Instructions = resp.Agent.Instructions + "\n\n" + briefing
						}
						// Surface the squad identity to the daemon so the
						// quick-create prompt defaults the new issue's
						// assignee to the squad, not the leader agent.
						resp.SquadID = uuidToString(squad.ID)
						resp.SquadName = squad.Name
						slog.Debug("injected squad leader briefing for quick-create",
							"squad_id", uuidToString(squad.ID),
							"squad_name", squad.Name,
							"leader_agent_id", resp.Agent.ID,
						)
					}
				}
			}
		}
	}

	// Workspace isolation check: the daemon uses this response's workspace_id
	// as the only authority for MULTICA_WORKSPACE_ID in the agent env. An
	// empty value would make the CLI silently fall back to the user-global
	// config and talk to whatever workspace the user happened to last
	// configure; a value that doesn't match the runtime's workspace means
	// upstream routed a foreign-workspace task here. Both cases must hard-
	// fail AND cancel the just-dispatched task so the queue / agent status
	// don't sit stuck until the stale-task sweeper fires minutes later.
	if resp.WorkspaceID == "" || resp.WorkspaceID != runtimeWorkspaceID {
		outcome = "error_workspace"
		slog.Error("task claim: workspace isolation check failed, cancelling task",
			"task_id", uuidToString(task.ID),
			"runtime_id", runtimeID,
			"runtime_workspace", runtimeWorkspaceID,
			"resolved_workspace", resp.WorkspaceID,
			"has_issue", task.IssueID.Valid,
			"has_chat", task.ChatSessionID.Valid,
			"has_autopilot_run", task.AutopilotRunID.Valid,
			"has_quick_create", hasQuickCreate,
		)
		if _, cerr := h.TaskService.CancelTask(r.Context(), task.ID); cerr != nil {
			slog.Error("task claim: cancel after workspace check failed",
				"task_id", uuidToString(task.ID), "error", cerr)
		}
		writeError(w, http.StatusInternalServerError, "task workspace isolation check failed")
		return
	}

	// Workspace-level Context (workspace.context DB column) — the per-workspace
	// system prompt that workspace owners set in Settings → General. Inject it
	// into the brief regardless of task kind (issue / chat / autopilot /
	// quick-create) so every agent running in the workspace sees the same
	// shared context. Empty string when the owner hasn't set one; the daemon
	// skips rendering the heading in that case.
	if ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(resp.WorkspaceID)); err == nil {
		if ws.Context.Valid {
			resp.WorkspaceContext = ws.Context.String
		}
	} else {
		slog.Warn("task claim: failed to load workspace for context injection",
			"task_id", uuidToString(task.ID),
			"workspace_id", resp.WorkspaceID,
			"error", err,
		)
	}

	// Mint a task-scoped `mat_` token bound to (agent, task, workspace,
	// owner). The daemon will inject this as MULTICA_TOKEN into the agent
	// process instead of its own credential, so any API call the agent
	// makes — even one that strips X-Agent-ID / X-Task-ID headers — is
	// recognized server-side as actor=agent, closing the lateral-movement
	// path on owner-only endpoints (e.g. `/api/agents/{id}/env`). Runtime
	// owner is required because task tokens are still bound to an owning user;
	// without one, fail the claim explicitly instead of letting the daemon
	// fall back to a member/owner credential. MUL-3292.
	// Token expires after the queue/runtime upper bound (24h) so it survives
	// long-running tasks but cannot outlive a forgotten one.
	if !runtime.OwnerID.Valid {
		outcome = "error_token"
		slog.Error("task claim: runtime owner missing; cancelling task to avoid unscoped agent credentials",
			"task_id", uuidToString(task.ID),
			"runtime_id", runtimeID,
			"workspace_id", runtimeWorkspaceID,
		)
		if _, cerr := h.TaskService.CancelTask(r.Context(), task.ID); cerr != nil {
			slog.Error("task claim: cancel after missing runtime owner failed",
				"task_id", uuidToString(task.ID), "error", cerr)
		}
		writeError(w, http.StatusInternalServerError, "runtime owner required to mint task token")
		return
	}
	tokenStr, terr := auth.GenerateAgentTaskToken()
	if terr != nil {
		outcome = "error_token"
		slog.Error("task claim: failed to generate agent task token",
			"task_id", uuidToString(task.ID), "error", terr)
		writeError(w, http.StatusInternalServerError, "failed to mint task token")
		return
	}
	if _, terr := h.Queries.CreateTaskToken(r.Context(), db.CreateTaskTokenParams{
		TokenHash:   auth.HashToken(tokenStr),
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		WorkspaceID: parseUUID(resp.WorkspaceID),
		UserID:      runtime.OwnerID,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}); terr != nil {
		outcome = "error_token"
		slog.Error("task claim: failed to persist agent task token",
			"task_id", uuidToString(task.ID), "error", terr)
		writeError(w, http.StatusInternalServerError, "failed to persist task token")
		return
	}
	resp.AuthToken = tokenStr

	slog.Info("task claimed by runtime", "task_id", uuidToString(task.ID), "runtime_id", runtimeID, "agent_id", uuidToString(task.AgentID), "prior_session", resp.PriorSessionID)
	writeJSON(w, http.StatusOK, map[string]any{"task": resp})
}

// trailingUserMessages returns the run of user messages after the last
// assistant message in a chronologically-ordered chat history — the set the
// agent has NOT yet replied to. The agent resumes its prior session and only
// learns of new input through the claim response's chat_message, so a single
// run that covers a debounced burst (MUL-2968) must deliver every one of
// these, not just the latest. Every completed or failed run writes an
// assistant row, so the anchor advances one turn at a time; the result is the
// whole slice on the first turn and exactly the new message(s) thereafter.
func trailingUserMessages(msgs []db.ChatMessage) []db.ChatMessage {
	start := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			start = i + 1
			break
		}
	}
	return msgs[start:]
}

// ListPendingTasksByRuntime returns queued/dispatched tasks for a runtime.
func (h *Handler) ListPendingTasksByRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	// Verify the caller owns this runtime's workspace.
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	workspaceID := uuidToString(runtime.WorkspaceID)

	tasks, err := h.Queries.ListPendingTasksByRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending tasks")
		return
	}

	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t, workspaceID)
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Task Lifecycle (called by daemon)
// ---------------------------------------------------------------------------

// StartTask marks a dispatched task as running.
func (h *Handler) StartTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	// Verify the caller owns this task's workspace.
	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	task, err := h.TaskService.StartTask(r.Context(), parseUUID(taskID))
	if err != nil {
		slog.Warn("start task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("task started", "task_id", taskID, "agent_id", uuidToString(task.AgentID))
	writeJSON(w, http.StatusOK, taskToResponse(*task, workspaceID))
}

// TaskWaitLocalDirectoryRequest is the body the daemon POSTs when it parks
// a freshly-dispatched task on a busy local_directory path.
type TaskWaitLocalDirectoryRequest struct {
	// Reason is a short hint surfaced by the UI alongside the status —
	// typically "<path>" or "<path> (holder: <task short id>)". Small
	// enough to fit on the issue card. Empty is accepted; the column is
	// nullable on the server.
	Reason string `json:"reason"`
}

// MarkTaskWaitingLocalDirectory transitions a dispatched task to
// waiting_local_directory. Called by the daemon when, after claiming a task
// whose project carries a local_directory resource, it discovers another
// in-flight task already holds the path's mutex.
func (h *Handler) MarkTaskWaitingLocalDirectory(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	var req TaskWaitLocalDirectoryRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	task, err := h.TaskService.MarkTaskWaitingLocalDirectory(r.Context(), parseUUID(taskID), req.Reason)
	if err != nil {
		slog.Warn("mark task waiting_local_directory failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(*task, workspaceID))
}

// ReportTaskProgress broadcasts a progress update.
type TaskProgressRequest struct {
	Summary string `json:"summary"`
	Step    int    `json:"step"`
	Total   int    `json:"total"`
}

func (h *Handler) ReportTaskProgress(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	var req TaskProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Verify ownership and resolve workspace ID.
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	workspaceID := ""
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(r.Context(), task.IssueID); err == nil {
			workspaceID = uuidToString(issue.WorkspaceID)
		}
	}

	h.TaskService.ReportProgress(r.Context(), taskID, workspaceID, req.Summary, req.Step, req.Total)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CompleteTask marks a running task as completed.
type TaskCompleteRequest struct {
	PRURL     string `json:"pr_url"`
	Output    string `json:"output"`
	SessionID string `json:"session_id"` // Claude session ID for future resumption
	WorkDir   string `json:"work_dir"`   // working directory used during execution
}

func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	// Verify the caller owns this task's workspace.
	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	var req TaskCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, _ := json.Marshal(req)
	task, err := h.TaskService.CompleteTask(r.Context(), parseUUID(taskID), result, req.SessionID, req.WorkDir)
	if err != nil {
		slog.Warn("complete task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.emitIssueExecutedOnFirstCompletion(r, task)

	// Best-effort revoke of any agent task token minted at claim time.
	// The token would naturally expire at the 24h watermark and is also
	// cascaded on agent_task deletion, but eagerly deleting it on
	// completion shrinks the window where a compromised agent process
	// can keep making API calls after its task finishes. Failure here is
	// non-fatal; the expiry / cascade are the durable guards.
	if err := h.Queries.DeleteTaskTokensByTask(r.Context(), task.ID); err != nil {
		slog.Warn("complete task: failed to revoke task tokens", "task_id", uuidToString(task.ID), "error", err)
	}

	slog.Info("task completed", "task_id", taskID, "agent_id", uuidToString(task.AgentID))
	writeJSON(w, http.StatusOK, taskToResponse(*task, workspaceID))
}

// emitIssueExecutedOnFirstCompletion atomically flips issue.first_executed_at
// and fires the issue_executed analytics event iff this is the first task on
// the issue to reach terminal done. Retries / re-assignments / comment-
// triggered follow-ups hit the WHERE first_executed_at IS NULL clause and
// no-op, so the funnel counts unique issues, not tasks.
func (h *Handler) emitIssueExecutedOnFirstCompletion(r *http.Request, task *db.AgentTaskQueue) {
	if task == nil {
		return
	}
	marked, err := h.Queries.MarkIssueFirstExecuted(r.Context(), task.IssueID)
	if err != nil {
		if !isNotFound(err) {
			slog.Warn("analytics: mark issue first-executed failed", "issue_id", uuidToString(task.IssueID), "error", err)
		}
		return
	}
	var durationMS int64
	if task.StartedAt.Valid && task.CompletedAt.Valid {
		durationMS = task.CompletedAt.Time.Sub(task.StartedAt.Time).Milliseconds()
	}
	taskContext := h.TaskService.AnalyticsContextForTask(r.Context(), *task)
	// distinct_id prefers the human creator so agent-driven events flow into
	// the issue-author's person profile (same place signup and
	// workspace_created land). Agent-created issues keep the agent id with a
	// prefix so PostHog doesn't merge them into a user by accident.
	distinct := uuidToString(marked.CreatorID)
	if marked.CreatorType == "agent" {
		distinct = "agent:" + distinct
	}
	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueExecuted(
		distinct,
		uuidToString(marked.WorkspaceID),
		uuidToString(marked.ID),
		uuidToString(task.ID),
		uuidToString(task.AgentID),
		taskContext.Source,
		taskContext.RuntimeMode,
		taskContext.Provider,
		durationMS,
	))
}

// ReportTaskUsage stores per-task token usage. Called independently of
// complete/fail so usage is captured even when tasks fail or are blocked.
type TaskUsagePayload struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

func (h *Handler) ReportTaskUsage(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	// Verify the caller owns this task's workspace.
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	var req struct {
		Usage []TaskUsagePayload `json:"usage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Provider is lowercased on write so client-side pricing lookups tolerate
	// case drift. An empty provider (an older daemon that omits the field) is
	// stamped from the task's runtime, so generic model ids like `auto` still
	// resolve to a provider instead of landing as '' and pricing $0.
	var runtimeProvider string
	runtimeProviderLoaded := false
	for _, u := range req.Usage {
		provider := normalizeProvider(u.Provider)
		if provider == "" {
			if !runtimeProviderLoaded {
				if rt, err := h.Queries.GetAgentRuntime(r.Context(), task.RuntimeID); err == nil {
					runtimeProvider = normalizeProvider(rt.Provider)
				} else {
					slog.Warn("load runtime provider for usage backfill failed",
						"task_id", taskID, "runtime_id", uuidToString(task.RuntimeID), "error", err)
				}
				runtimeProviderLoaded = true
			}
			provider = runtimeProvider
		}
		if err := h.Queries.UpsertTaskUsage(r.Context(), db.UpsertTaskUsageParams{
			TaskID:           parseUUID(taskID),
			Provider:         provider,
			Model:            u.Model,
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
		}); err != nil {
			slog.Warn("upsert task usage failed", "task_id", taskID, "model", u.Model, "error", err)
			continue
		}
		h.TaskService.CaptureTaskUsage(r.Context(), task, provider, u.Model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetTaskStatus returns the current status of a task.
// Used by the daemon to detect terminal/interruption signals (cancelled,
// failed, completed) while a task is executing mid-flight.
func (h *Handler) GetTaskStatus(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	// Verify the caller owns this task's workspace.
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": task.Status})
}

// FailTask marks a running task as failed.
type TaskFailRequest struct {
	Error         string `json:"error"`
	SessionID     string `json:"session_id,omitempty"`
	WorkDir       string `json:"work_dir,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

func (h *Handler) FailTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	// Verify the caller owns this task's workspace.
	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	var req TaskFailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	task, err := h.TaskService.FailTask(r.Context(), parseUUID(taskID), req.Error, req.SessionID, req.WorkDir, req.FailureReason)
	if err != nil {
		slog.Warn("fail task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Best-effort revoke of the mat_ task token minted at claim. Same
	// rationale as CompleteTask — eager deletion shrinks the post-
	// terminal window. The 24h expiry / cascade are the durable guards.
	if err := h.Queries.DeleteTaskTokensByTask(r.Context(), task.ID); err != nil {
		slog.Warn("fail task: failed to revoke task tokens", "task_id", uuidToString(task.ID), "error", err)
	}

	slog.Info("task failed", "task_id", taskID, "agent_id", uuidToString(task.AgentID), "task_error", req.Error, "failure_reason", req.FailureReason)
	writeJSON(w, http.StatusOK, taskToResponse(*task, workspaceID))
}

// ---------------------------------------------------------------------------
// Task Messages (live agent output)
// ---------------------------------------------------------------------------

type TaskMessageRequest struct {
	Seq     int            `json:"seq"`
	Type    string         `json:"type"`
	Tool    string         `json:"tool,omitempty"`
	Content string         `json:"content,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Output  string         `json:"output,omitempty"`
}

type TaskMessageBatchRequest struct {
	Messages []TaskMessageRequest `json:"messages"`
}

// ReportTaskMessages receives a batch of agent execution messages from the daemon.
func (h *Handler) ReportTaskMessages(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	var req TaskMessageBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Verify the caller owns this task's workspace.
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	workspaceID := ""
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(r.Context(), task.IssueID); err == nil {
			workspaceID = uuidToString(issue.WorkspaceID)
		}
	}
	if workspaceID == "" && task.ChatSessionID.Valid {
		if cs, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID); err == nil {
			workspaceID = uuidToString(cs.WorkspaceID)
		}
	}

	for _, msg := range req.Messages {
		// Redact sensitive information before persisting or broadcasting.
		msg.Content = redact.Text(msg.Content)
		msg.Output = redact.Text(msg.Output)
		msg.Input = redact.InputMap(msg.Input)

		var inputJSON []byte
		if msg.Input != nil {
			inputJSON, _ = json.Marshal(msg.Input)
		}
		created, createErr := h.Queries.CreateTaskMessage(r.Context(), db.CreateTaskMessageParams{
			TaskID:  parseUUID(taskID),
			Seq:     int32(msg.Seq),
			Type:    msg.Type,
			Tool:    pgtype.Text{String: msg.Tool, Valid: msg.Tool != ""},
			Content: pgtype.Text{String: msg.Content, Valid: msg.Content != ""},
			Input:   inputJSON,
			Output:  pgtype.Text{String: msg.Output, Valid: msg.Output != ""},
		})
		if createErr != nil {
			slog.Error("failed to create task message", "task_id", taskID, "seq", msg.Seq, "error", createErr)
			writeError(w, http.StatusInternalServerError, "failed to persist task message")
			return
		}

		if workspaceID != "" {
			h.publishTask(protocol.EventTaskMessage, workspaceID, "system", "", taskID,
				taskMessageToPayload(created, taskID, uuidToString(task.IssueID)))
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func taskMessageToPayload(m db.TaskMessage, taskID, issueID string) protocol.TaskMessagePayload {
	var input map[string]any
	if m.Input != nil {
		json.Unmarshal(m.Input, &input)
		input = sanitizeTaskMessageInput(input)
	}
	createdAt := ""
	if m.CreatedAt.Valid {
		createdAt = m.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return protocol.TaskMessagePayload{
		TaskID:    taskID,
		IssueID:   issueID,
		Seq:       int(m.Seq),
		Type:      m.Type,
		Tool:      m.Tool.String,
		Content:   m.Content.String,
		Input:     input,
		Output:    m.Output.String,
		CreatedAt: createdAt,
	}
}

var taskMessageSensitiveInputKeys = map[string]struct{}{
	"connectioncredentials": {},
	"daemonoperation":       {},
	"daemonoperationparams": {},
	"runtimecallurl":        {},
	"runtimeconnection":     {},
	"runtimecredentials":    {},
	"runtimedetailurl":      {},
	"runtimedetails":        {},
	"runtimeid":             {},
	"runtimeselectoroption": {},
	"workdir":               {},
	"priorworkdir":          {},
}

func sanitizeTaskMessageInput(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cleaned := make(map[string]any, len(input))
	for key, value := range input {
		if _, sensitive := taskMessageSensitiveInputKeys[normalizeTaskAuditKey(key)]; sensitive {
			continue
		}
		cleaned[key] = sanitizeTaskMessageValue(value)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func sanitizeTaskMessageValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return sanitizeTaskMessageInput(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = sanitizeTaskMessageValue(item)
		}
		return out
	default:
		return value
	}
}

// ListTaskMessages returns the persisted messages for a task (for catch-up after reconnect).
func (h *Handler) ListTaskMessages(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	// Verify the caller owns this task's workspace.
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	var (
		messages []db.TaskMessage
		err      error
	)
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		sinceSeq, parseErr := strconv.Atoi(sinceStr)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter")
			return
		}
		messages, err = h.Queries.ListTaskMessagesSince(r.Context(), db.ListTaskMessagesSinceParams{
			TaskID: parseUUID(taskID),
			Seq:    int32(sinceSeq),
		})
	} else {
		messages, err = h.Queries.ListTaskMessages(r.Context(), parseUUID(taskID))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list task messages")
		return
	}

	issueID := uuidToString(task.IssueID)

	resp := make([]protocol.TaskMessagePayload, len(messages))
	for i, m := range messages {
		resp[i] = taskMessageToPayload(m, taskID, issueID)
	}

	writeJSON(w, http.StatusOK, resp)
}

// GetActiveTaskForIssue returns all currently active tasks for an issue.
// Returns { tasks: [...] } array (may be empty).
func (h *Handler) GetActiveTaskForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	tasks, err := h.Queries.ListActiveTasksByIssue(r.Context(), issue.ID)
	if err != nil {
		tasks = nil
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToAuditResponse(t, workspaceID)
	}

	writeJSON(w, http.StatusOK, map[string]any{"tasks": resp})
}

// CancelTask cancels a running or queued task by ID.
// Verifies both that the URL-parameter issue belongs to the caller's workspace
// and that the task belongs to that same issue — a task UUID from a different
// issue (in any workspace) must not be cancellable through this route.
func (h *Handler) CancelTask(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	taskID := chi.URLParam(r, "taskId")
	existing, err := h.Queries.GetAgentTask(r.Context(), parseUUID(taskID))
	if err != nil || uuidToString(existing.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	task, err := h.TaskService.CancelTask(r.Context(), existing.ID)
	if err != nil {
		slog.Warn("cancel task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("task cancelled by user", "task_id", taskID, "issue_id", uuidToString(task.IssueID))
	writeJSON(w, http.StatusOK, taskToAuditResponse(*task, uuidToString(issue.WorkspaceID)))
}

// ListTasksByIssue returns all tasks (any status) for an issue — used for execution history.
func (h *Handler) ListTasksByIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	tasks, err := h.Queries.ListTasksByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToAuditResponse(t, workspaceID)
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListTaskMessagesByUser returns task messages for a task.
// Used by the frontend under regular user auth (not daemon auth).
// Verifies the task belongs to the caller's workspace.
func (h *Handler) ListTaskMessagesByUser(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
	if !ok {
		return
	}

	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	// Verify the task belongs to the caller's workspace.
	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" || wsID != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if task.ChatSessionID.Valid {
		if _, ok := h.gateChatSessionForUser(w, r, userID, wsID, uuidToString(task.ChatSessionID)); !ok {
			return
		}
	} else {
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          task.AgentID,
			WorkspaceID: parseUUID(wsID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		actorType, actorID := h.resolveActor(r, userID, wsID)
		if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, wsID) {
			writeError(w, http.StatusForbidden, "you do not have access to this agent")
			return
		}
	}

	var (
		messages []db.TaskMessage
		queryErr error
	)
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		sinceSeq, parseErr := strconv.Atoi(sinceStr)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter")
			return
		}
		messages, queryErr = h.Queries.ListTaskMessagesSince(r.Context(), db.ListTaskMessagesSinceParams{
			TaskID: taskUUID,
			Seq:    int32(sinceSeq),
		})
	} else {
		messages, queryErr = h.Queries.ListTaskMessages(r.Context(), taskUUID)
	}
	if queryErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to list task messages")
		return
	}

	issueID := uuidToString(task.IssueID)

	resp := make([]protocol.TaskMessagePayload, len(messages))
	for i, m := range messages {
		resp[i] = taskMessageToPayload(m, taskID, issueID)
	}

	writeJSON(w, http.StatusOK, resp)
}

// GetIssueUsage returns aggregated token usage for all tasks belonging to an issue.
func (h *Handler) GetIssueUsage(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	row, err := h.Queries.GetIssueUsageSummary(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get issue usage")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total_input_tokens":       row.TotalInputTokens,
		"total_output_tokens":      row.TotalOutputTokens,
		"total_cache_read_tokens":  row.TotalCacheReadTokens,
		"total_cache_write_tokens": row.TotalCacheWriteTokens,
		"task_count":               row.TaskCount,
	})
}

// GetIssueGCCheck returns minimal issue info needed by the daemon GC loop.
// Gated on workspace access so a daemon token scoped to workspace A cannot
// read issue metadata from workspace B via UUID enumeration.
func (h *Handler) GetIssueGCCheck(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueId")
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue_id")
	if !ok {
		return
	}
	issue, err := h.Queries.GetIssue(r.Context(), issueUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(issue.WorkspaceID)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     issue.Status,
		"updated_at": issue.UpdatedAt.Time,
	})
}

// GetChatSessionGCCheck returns the status and updated_at of a chat session
// for the daemon GC loop. A 404 here means the session was hard-deleted
// (DeleteChatSession in chat.go runs a real DELETE), which the daemon treats
// as an immediate-clean signal — the user's explicit delete is the strongest
// reclaim authorization we can get.
//
// Same anti-enumeration shape as GetIssueGCCheck: workspace mismatch returns
// the same 404 so a scoped daemon token can't probe other workspaces.
func (h *Handler) GetChatSessionGCCheck(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	sessionUUID, ok := parseUUIDOrBadRequest(w, sessionID, "session_id")
	if !ok {
		return
	}
	session, err := h.Queries.GetChatSession(r.Context(), sessionUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(session.WorkspaceID)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     session.Status,
		"updated_at": session.UpdatedAt.Time,
	})
}

// GetAutopilotRunGCCheck returns the status and completed_at of an autopilot
// run for the daemon GC loop. The daemon decides purely on terminal status:
// an autopilot run's workdir is never reused, so a terminal run is reclaimed on
// sight while non-terminal status is a skip signal — completed_at is returned
// for the API contract and diagnostics, not as a TTL anchor.
//
// Workspace ownership is resolved via the parent autopilot row.
func (h *Handler) GetAutopilotRunGCCheck(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	runUUID, ok := parseUUIDOrBadRequest(w, runID, "run_id")
	if !ok {
		return
	}
	run, err := h.Queries.GetAutopilotRun(r.Context(), runUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot run not found")
		return
	}
	autopilot, err := h.Queries.GetAutopilot(r.Context(), run.AutopilotID)
	if err != nil {
		// Parent autopilot is gone — treat as not found rather than 500
		// so the daemon can fall through to its orphan-by-mtime path.
		writeError(w, http.StatusNotFound, "autopilot run not found")
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(autopilot.WorkspaceID)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       run.Status,
		"completed_at": run.CompletedAt.Time,
	})
}

// GetTaskGCCheck returns the agent_task_queue status for quick-create cleanup.
// Quick-create tasks have no parent record (no issue_id at WriteGCMeta time,
// no chat session, no autopilot run) so the daemon keys GC directly on the
// task row itself.
func (h *Handler) GetTaskGCCheck(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       task.Status,
		"completed_at": task.CompletedAt.Time,
	})
}
