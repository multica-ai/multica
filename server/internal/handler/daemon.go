package handler

import (
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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/middleware"
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

	// PAT/JWT fallback: verify user is a member of the workspace.
	_, ok := h.requireWorkspaceMember(w, r, workspaceID, "not found")
	return ok
}

// requireDaemonRuntimeAccess looks up a runtime and verifies the caller owns its workspace.
func (h *Handler) requireDaemonRuntimeAccess(w http.ResponseWriter, r *http.Request, runtimeID string) (db.AgentRuntime, bool) {
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return db.AgentRuntime{}, false
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return db.AgentRuntime{}, false
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(rt.WorkspaceID)) {
		return db.AgentRuntime{}, false
	}
	return rt, true
}

// requireDaemonTaskAccess looks up a task and verifies the caller owns its workspace.
func (h *Handler) requireDaemonTaskAccess(w http.ResponseWriter, r *http.Request, taskID string) (db.AgentTaskQueue, bool) {
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
	if !ok {
		return db.AgentTaskQueue{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return db.AgentTaskQueue{}, false
	}

	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" {
		writeError(w, http.StatusNotFound, "task not found")
		return db.AgentTaskQueue{}, false
	}

	if !h.requireDaemonWorkspaceAccess(w, r, wsID) {
		return db.AgentTaskQueue{}, false
	}
	return task, true
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
	_, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	return err == nil
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
	} `json:"runtimes"`
}

type daemonWorkspaceReposResponse struct {
	WorkspaceID  string     `json:"workspace_id"`
	Repos        []RepoData `json:"repos"`
	ReposVersion string     `json:"repos_version"`
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
		normalized = append(normalized, RepoData{
			URL:         url,
			Description: strings.TrimSpace(repo.Description),
		})
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

func workspaceReposResponse(workspaceID string, raw []byte) daemonWorkspaceReposResponse {
	repos := parseWorkspaceRepos(raw)
	return daemonWorkspaceReposResponse{
		WorkspaceID:  workspaceID,
		Repos:        repos,
		ReposVersion: workspaceReposVersion(repos),
	}
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
		provider := strings.TrimSpace(runtime.Type)
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
			writeError(w, http.StatusInternalServerError, "failed to register runtime: "+err.Error())
			return
		}

		registered := db.AgentRuntime{
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
		}

		if row.Inserted {
			h.Analytics.Capture(analytics.RuntimeRegistered(
				uuidToString(ownerID),
				req.WorkspaceID,
				uuidToString(registered.ID),
				provider,
				runtime.Version,
				req.CLIVersion,
			))
		}

		// Seamless migration from the previous hostname-derived identity. The
		// daemon sends every legacy daemon_id it may have registered under
		// (e.g. "host.local", "host", "host-staging"); for each match we
		// reassign agents + tasks onto the new UUID-keyed row, then delete
		// the stale row so there's only ever one runtime per machine.
		h.mergeLegacyRuntimes(r, registered, provider, req.LegacyDaemonIDs)

		resp = append(resp, runtimeToResponse(registered))
	}

	slog.Info("daemon registered", "workspace_id", req.WorkspaceID, "daemon_id", req.DaemonID, "runtimes_count", len(resp))

	h.publish(protocol.EventDaemonRegister, req.WorkspaceID, "system", "", map[string]any{
		"runtimes": resp,
	})

	repoResp := workspaceReposResponse(req.WorkspaceID, ws.Repos)
	writeJSON(w, http.StatusOK, map[string]any{
		"runtimes":      resp,
		"repos":         repoResp.Repos,
		"repos_version": repoResp.ReposVersion,
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
// Scoping by (workspace_id, provider) is sufficient since provider is single-
// runtime-per-daemon; `unique (workspace_id, daemon_id, provider)` prevents
// any two *exact* matches but the `LOWER(...)` comparison crosses that bound
// precisely when case-duplicate rows exist — which is the bug we're fixing.
// We also dedupe across legacy ids so overlapping candidates (e.g. `foo` and
// `foo.local` both resolving to the same stored row) don't double-process.
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

	writeJSON(w, http.StatusOK, workspaceReposResponse(workspaceID, ws.Repos))
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

		if err := h.Queries.SetAgentRuntimeOffline(r.Context(), rt.ID); err != nil {
			slog.Warn("deregister: failed to set offline", "runtime_id", rid, "error", err)
			continue
		}

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
	RuntimeID string `json:"runtime_id"`
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

func (h *Handler) DaemonHeartbeat(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var (
		outcome                                                                  = "unauth"
		runtimeID                                                                string
		authMs, updateMs, probeSkillsMs, popSkillsMs, probeImportMs, popImportMs int64
		probeSkillsTimedOut, probeImportTimedOut                                 bool
	)
	defer func() {
		logHeartbeatEndpointSlow(runtimeID, outcome, start, authMs, updateMs, probeSkillsMs, popSkillsMs, probeImportMs, popImportMs, probeSkillsTimedOut, probeImportTimedOut)
	}()

	var req DaemonHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	// Verify the caller owns this runtime's workspace.
	rt, ok := h.requireDaemonRuntimeAccess(w, r, req.RuntimeID)
	if !ok {
		return
	}
	authMs = time.Since(start).Milliseconds()

	updateStart := time.Now()
	_, err := h.Queries.UpdateAgentRuntimeHeartbeat(r.Context(), rt.ID)
	updateMs = time.Since(updateStart).Milliseconds()
	if err != nil {
		outcome = "error_update"
		writeError(w, http.StatusInternalServerError, "heartbeat failed")
		return
	}

	slog.Debug("daemon heartbeat", "runtime_id", req.RuntimeID)

	resp := map[string]any{"status": "ok"}

	// Check for pending update requests for this runtime.
	if pending := h.UpdateStore.PopPending(req.RuntimeID); pending != nil {
		resp["pending_update"] = map[string]string{
			"id":             pending.ID,
			"target_version": pending.TargetVersion,
		}
	}

	// Check for pending model-list requests for this runtime.
	if pending := h.ModelListStore.PopPending(req.RuntimeID); pending != nil {
		resp["pending_model_list"] = map[string]string{"id": pending.ID}
	}

	// Probe then claim the local-skill list queue. The probe is bounded so a
	// slow shared store cannot stall the heartbeat on empty-queue ticks; the
	// claim runs unbounded (it inherits only r.Context()) because its Lua
	// side effects cannot be safely aborted mid-script.
	probeSkillsStart := time.Now()
	probeSkillsCtx, cancelProbeSkills := context.WithTimeout(r.Context(), heartbeatHasPendingTimeout)
	hasSkills, probeErr := h.LocalSkillListStore.HasPending(probeSkillsCtx, req.RuntimeID)
	cancelProbeSkills()
	probeSkillsMs = time.Since(probeSkillsStart).Milliseconds()
	switch {
	case probeErr == nil && hasSkills:
		popStart := time.Now()
		pendingSkills, popErr := h.LocalSkillListStore.PopPending(r.Context(), req.RuntimeID)
		popSkillsMs = time.Since(popStart).Milliseconds()
		if popErr != nil {
			slog.Warn("local skill list PopPending failed", "error", popErr, "runtime_id", req.RuntimeID)
		} else if pendingSkills != nil {
			resp["pending_local_skills"] = map[string]string{"id": pendingSkills.ID}
		}
	case probeErr != nil:
		if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(probeErr, context.Canceled) {
			probeSkillsTimedOut = true
			slog.Warn("local skill list HasPending timed out", "runtime_id", req.RuntimeID, "elapsed_ms", probeSkillsMs)
		} else {
			slog.Warn("local skill list HasPending failed", "error", probeErr, "runtime_id", req.RuntimeID)
		}
	}

	// Same probe-then-claim pattern for the import queue.
	probeImportStart := time.Now()
	probeImportCtx, cancelProbeImport := context.WithTimeout(r.Context(), heartbeatHasPendingTimeout)
	hasImport, probeErr := h.LocalSkillImportStore.HasPending(probeImportCtx, req.RuntimeID)
	cancelProbeImport()
	probeImportMs = time.Since(probeImportStart).Milliseconds()
	switch {
	case probeErr == nil && hasImport:
		popStart := time.Now()
		pendingImport, popErr := h.LocalSkillImportStore.PopPending(r.Context(), req.RuntimeID)
		popImportMs = time.Since(popStart).Milliseconds()
		if popErr != nil {
			slog.Warn("local skill import PopPending failed", "error", popErr, "runtime_id", req.RuntimeID)
		} else if pendingImport != nil {
			resp["pending_local_skill_import"] = map[string]string{
				"id":        pendingImport.ID,
				"skill_key": pendingImport.SkillKey,
			}
		}
	case probeErr != nil:
		if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(probeErr, context.Canceled) {
			probeImportTimedOut = true
			slog.Warn("local skill import HasPending timed out", "runtime_id", req.RuntimeID, "elapsed_ms", probeImportMs)
		} else {
			slog.Warn("local skill import HasPending failed", "error", probeErr, "runtime_id", req.RuntimeID)
		}
	}

	outcome = "ok"
	writeJSON(w, http.StatusOK, resp)
}

// logHeartbeatEndpointSlow emits one structured log when /api/daemon/heartbeat
// exceeds 500ms, splitting auth / update / probe / pop phases for both queues
// so the prod tail can be attributed without flooding logs at normal rates.
// Mirrors logClaimEndpointSlow for consistency.
func logHeartbeatEndpointSlow(runtimeID, outcome string, start time.Time, authMs, updateMs, probeSkillsMs, popSkillsMs, probeImportMs, popImportMs int64, probeSkillsTimedOut, probeImportTimedOut bool) {
	totalMs := time.Since(start).Milliseconds()
	if totalMs < 500 && !probeSkillsTimedOut && !probeImportTimedOut {
		return
	}
	slog.Info("heartbeat_endpoint slow",
		"runtime_id", runtimeID,
		"outcome", outcome,
		"total_ms", totalMs,
		"auth_ms", authMs,
		"update_ms", updateMs,
		"probe_skills_ms", probeSkillsMs,
		"pop_skills_ms", popSkillsMs,
		"probe_import_ms", probeImportMs,
		"pop_import_ms", popImportMs,
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
	resp := taskToResponse(*task)
	if agent, err := h.Queries.GetAgent(r.Context(), task.AgentID); err == nil {
		skills := h.TaskService.LoadAgentSkills(r.Context(), task.AgentID)
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
		resp.Agent = &TaskAgentData{
			ID:           uuidToString(agent.ID),
			Name:         agent.Name,
			Instructions: agent.Instructions,
			Skills:       skills,
			CustomEnv:    customEnv,
			CustomArgs:   customArgs,
			McpConfig:    mcpConfig,
			Model:        agent.Model.String,
		}
	}

	// Include workspace ID and repos so the daemon can set up worktrees.
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(r.Context(), task.IssueID); err == nil {
			resp.WorkspaceID = uuidToString(issue.WorkspaceID)
			if ws, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID); err == nil && ws.Repos != nil {
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
				resp.TriggerAuthorType = comment.AuthorType
				switch comment.AuthorType {
				case "agent":
					if comment.AuthorID.Valid {
						if a, err := h.Queries.GetAgent(r.Context(), comment.AuthorID); err == nil {
							resp.TriggerAuthorName = a.Name
						}
					}
				case "member":
					// For member-authored comments, AuthorID is a user UUID
					// (see handler.resolveActor) — look up the user's display name.
					if comment.AuthorID.Valid {
						if u, err := h.Queries.GetUser(r.Context(), comment.AuthorID); err == nil {
							resp.TriggerAuthorName = u.Name
						}
					}
				}
			}
		}

		// Look up the prior session for this (agent, issue) pair so the daemon
		// can resume the Claude Code conversation context.
		if prior, err := h.Queries.GetLastTaskSession(r.Context(), db.GetLastTaskSessionParams{
			AgentID: task.AgentID,
			IssueID: task.IssueID,
		}); err == nil && prior.SessionID.Valid {
			resp.PriorSessionID = prior.SessionID.String
			if prior.WorkDir.Valid {
				resp.PriorWorkDir = prior.WorkDir.String
			}
		}

		// Fetch label-linked instructions so the daemon can inject them
		// into the agent's system prompt. Labels with empty instructions
		// are skipped — they're organizational only.
		//
		// Skip the fetch when the issue's workspace couldn't be resolved
		// (GetIssue above failed transiently). The downstream isolation
		// check would already abort the claim, but querying with a zero
		// UUID is wasted I/O and obscures intent.
		if resp.WorkspaceID != "" {
			labels, err := h.Queries.ListLabelsByIssue(r.Context(), db.ListLabelsByIssueParams{
				IssueID:     task.IssueID,
				WorkspaceID: parseUUID(resp.WorkspaceID),
			})
			if err != nil {
				slog.Warn("ClaimTaskByRuntime: ListLabelsByIssue failed, proceeding without label instructions",
					"task_id", uuidToString(task.ID),
					"issue_id", uuidToString(task.IssueID),
					"workspace_id", resp.WorkspaceID,
					"error", err)
			} else {
				for _, l := range labels {
					if l.Instructions != "" {
						resp.LabelInstructions = append(resp.LabelInstructions, TaskLabelInstruction{
							Name:         l.Name,
							Instructions: l.Instructions,
						})
					}
				}
			}
		}
	}

	// Chat task: populate workspace/session info from the chat_session table.
	if task.ChatSessionID.Valid {
		if cs, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID); err == nil {
			resp.WorkspaceID = uuidToString(cs.WorkspaceID)
			resp.ChatSessionID = uuidToString(cs.ID)
			if ws, err := h.Queries.GetWorkspace(r.Context(), cs.WorkspaceID); err == nil && ws.Repos != nil {
				var repos []RepoData
				if json.Unmarshal(ws.Repos, &repos) == nil && len(repos) > 0 {
					resp.Repos = repos
				}
			}
			// Resume from the chat session's persistent session, falling back
			// to the most recent task that recorded a session_id when the
			// chat_session pointer is missing or stale (e.g. a previous task
			// failed before reporting completion). Without this fallback a
			// single failed turn would silently drop the entire conversation
			// memory on the next message.
			if cs.SessionID.Valid {
				resp.PriorSessionID = cs.SessionID.String
			}
			if cs.WorkDir.Valid {
				resp.PriorWorkDir = cs.WorkDir.String
			}
			if resp.PriorSessionID == "" {
				if prior, err := h.Queries.GetLastChatTaskSession(r.Context(), cs.ID); err == nil && prior.SessionID.Valid {
					resp.PriorSessionID = prior.SessionID.String
					if prior.WorkDir.Valid && resp.PriorWorkDir == "" {
						resp.PriorWorkDir = prior.WorkDir.String
					}
				}
			}
			// Load the latest user message for the chat prompt.
			if msgs, err := h.Queries.ListChatMessages(r.Context(), cs.ID); err == nil && len(msgs) > 0 {
				// Find the last user message.
				for i := len(msgs) - 1; i >= 0; i-- {
					if msgs[i].Role == "user" {
						resp.ChatMessage = msgs[i].Content
						break
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
		)
		if _, cerr := h.TaskService.CancelTask(r.Context(), task.ID); cerr != nil {
			slog.Error("task claim: cancel after workspace check failed",
				"task_id", uuidToString(task.ID), "error", cerr)
		}
		writeError(w, http.StatusInternalServerError, "task workspace isolation check failed")
		return
	}

	slog.Info("task claimed by runtime", "task_id", uuidToString(task.ID), "runtime_id", runtimeID, "agent_id", uuidToString(task.AgentID), "prior_session", resp.PriorSessionID)
	writeJSON(w, http.StatusOK, map[string]any{"task": resp})
}

// ListPendingTasksByRuntime returns queued/dispatched tasks for a runtime.
func (h *Handler) ListPendingTasksByRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	// Verify the caller owns this runtime's workspace.
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	tasks, err := h.Queries.ListPendingTasksByRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending tasks")
		return
	}

	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t)
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
	if _, ok := h.requireDaemonTaskAccess(w, r, taskID); !ok {
		return
	}

	task, err := h.TaskService.StartTask(r.Context(), parseUUID(taskID))
	if err != nil {
		slog.Warn("start task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("task started", "task_id", taskID, "agent_id", uuidToString(task.AgentID))
	writeJSON(w, http.StatusOK, taskToResponse(*task))
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
	if _, ok := h.requireDaemonTaskAccess(w, r, taskID); !ok {
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

	slog.Info("task completed", "task_id", taskID, "agent_id", uuidToString(task.AgentID))
	writeJSON(w, http.StatusOK, taskToResponse(*task))
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
	// distinct_id prefers the human creator so agent-driven events flow into
	// the issue-author's person profile (same place signup and
	// workspace_created land). Agent-created issues keep the agent id with a
	// prefix so PostHog doesn't merge them into a user by accident.
	distinct := uuidToString(marked.CreatorID)
	if marked.CreatorType == "agent" {
		distinct = "agent:" + distinct
	}
	h.Analytics.Capture(analytics.IssueExecuted(
		distinct,
		uuidToString(marked.WorkspaceID),
		uuidToString(marked.ID),
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
	if _, ok := h.requireDaemonTaskAccess(w, r, taskID); !ok {
		return
	}

	var req struct {
		Usage []TaskUsagePayload `json:"usage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, u := range req.Usage {
		if err := h.Queries.UpsertTaskUsage(r.Context(), db.UpsertTaskUsageParams{
			TaskID:           parseUUID(taskID),
			Provider:         u.Provider,
			Model:            u.Model,
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
		}); err != nil {
			slog.Warn("upsert task usage failed", "task_id", taskID, "model", u.Model, "error", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetTaskStatus returns the current status of a task.
// Used by the daemon to check whether a task was cancelled mid-execution.
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
	if _, ok := h.requireDaemonTaskAccess(w, r, taskID); !ok {
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

	slog.Info("task failed", "task_id", taskID, "agent_id", uuidToString(task.AgentID), "task_error", req.Error, "failure_reason", req.FailureReason)
	writeJSON(w, http.StatusOK, taskToResponse(*task))
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
		h.Queries.CreateTaskMessage(r.Context(), db.CreateTaskMessageParams{
			TaskID:  parseUUID(taskID),
			Seq:     int32(msg.Seq),
			Type:    msg.Type,
			Tool:    pgtype.Text{String: msg.Tool, Valid: msg.Tool != ""},
			Content: pgtype.Text{String: msg.Content, Valid: msg.Content != ""},
			Input:   inputJSON,
			Output:  pgtype.Text{String: msg.Output, Valid: msg.Output != ""},
		})

		if workspaceID != "" {
			h.publishTask(protocol.EventTaskMessage, workspaceID, "system", "", taskID, protocol.TaskMessagePayload{
				TaskID:  taskID,
				IssueID: uuidToString(task.IssueID),
				Seq:     msg.Seq,
				Type:    msg.Type,
				Tool:    msg.Tool,
				Content: msg.Content,
				Input:   msg.Input,
				Output:  msg.Output,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		var input map[string]any
		if m.Input != nil {
			json.Unmarshal(m.Input, &input)
		}
		resp[i] = protocol.TaskMessagePayload{
			TaskID:  taskID,
			IssueID: issueID,
			Seq:     int(m.Seq),
			Type:    m.Type,
			Tool:    m.Tool.String,
			Content: m.Content.String,
			Input:   input,
			Output:  m.Output.String,
		}
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

	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t)
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
	writeJSON(w, http.StatusOK, taskToResponse(*task))
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

	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t)
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListTaskMessagesByUser returns task messages for a task.
// Used by the frontend under regular user auth (not daemon auth).
// Verifies the task belongs to the caller's workspace.
func (h *Handler) ListTaskMessagesByUser(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	task, err := h.Queries.GetAgentTask(r.Context(), parseUUID(taskID))
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
			TaskID: parseUUID(taskID),
			Seq:    int32(sinceSeq),
		})
	} else {
		messages, queryErr = h.Queries.ListTaskMessages(r.Context(), parseUUID(taskID))
	}
	if queryErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to list task messages")
		return
	}

	issueID := uuidToString(task.IssueID)

	resp := make([]protocol.TaskMessagePayload, len(messages))
	for i, m := range messages {
		var input map[string]any
		if m.Input != nil {
			json.Unmarshal(m.Input, &input)
		}
		resp[i] = protocol.TaskMessagePayload{
			TaskID:  taskID,
			IssueID: issueID,
			Seq:     int(m.Seq),
			Type:    m.Type,
			Tool:    m.Tool.String,
			Content: m.Content.String,
			Input:   input,
			Output:  m.Output.String,
		}
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
