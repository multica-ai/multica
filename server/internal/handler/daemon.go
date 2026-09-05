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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// claimPollHintMinDelay bounds a future mismatch between the hint query and
// deferred-task promotion to at most one follow-up claim per second. Under the
// shared eligibility fences an overdue task should normally be promoted by the
// current request, so this is a defense-in-depth floor rather than the steady
// state poll interval.
const claimPollHintMinDelay = time.Second

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

// requireDaemonRuntimeAccess looks up a runtime and verifies the caller owns its workspace.
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
	rt, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceDaemonAPI, runtimeUUID)
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
	return rt, true
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
	FailedProfiles []struct {
		ProfileID   string `json:"profile_id"`
		CommandName string `json:"command_name"`
		Reason      string `json:"reason"`
	} `json:"failed_profiles"`
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

// inheritMachineCustomName gives a freshly-inserted runtime the machine's
// shared custom name (MUL-4217) when the machine is already named, so adding a
// provider — or recording a failed custom-runtime profile — on a named machine
// doesn't leave a custom_name = NULL row that makes the machine title revert to
// its hostname. Both the normal runtime path and the failed-profile path write
// daemon_id-scoped rows that show up in the machine grouping, so both call this.
//
// Only fresh inserts with no name of their own and a daemon_id participate;
// existing rows keep whatever they already have. A lookup/update error is
// non-fatal — registration must still succeed — so the input row is returned
// unchanged on any failure.
func (h *Handler) inheritMachineCustomName(ctx context.Context, rt db.AgentRuntime, inserted bool) db.AgentRuntime {
	if !inserted || rt.CustomName.Valid || !rt.DaemonID.Valid {
		return rt
	}
	names, err := h.Queries.ListDaemonCustomNames(ctx, db.ListDaemonCustomNamesParams{
		WorkspaceID: rt.WorkspaceID,
		DaemonID:    rt.DaemonID,
		ExcludeID:   rt.ID,
	})
	if err != nil {
		return rt
	}
	shared, ok := sharedDaemonCustomName(names)
	if !ok {
		return rt
	}
	updated, err := h.Queries.UpdateAgentRuntimeCustomName(ctx, db.UpdateAgentRuntimeCustomNameParams{
		CustomName: pgtype.Text{String: shared, Valid: true},
		ID:         rt.ID,
	})
	if err != nil {
		return rt
	}
	return updated
}

var errRuntimeProfileDisabled = errors.New("runtime profile is disabled")

// upsertRuntimeWithProfile serializes custom-runtime registration with profile
// deletion. The profile row remains KEY SHARE locked until the runtime upsert
// commits; DeleteRuntimeProfile takes a conflicting UPDATE lock before it
// enumerates runtime rows. This closes the stale-read window where deletion
// could miss an instance inserted by a concurrently registering daemon.
func (h *Handler) upsertRuntimeWithProfile(
	ctx context.Context,
	workspaceID, profileID pgtype.UUID,
	build func(db.RuntimeProfile) db.UpsertAgentRuntimeWithProfileParams,
) (db.UpsertAgentRuntimeWithProfileRow, db.RuntimeProfile, error) {
	var row db.UpsertAgentRuntimeWithProfileRow
	var profile db.RuntimeProfile

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return row, profile, fmt.Errorf("begin profile runtime registration: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	profile, err = qtx.LockRuntimeProfileForRegistration(ctx, db.LockRuntimeProfileForRegistrationParams{
		ID:          profileID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return row, profile, fmt.Errorf("lock runtime profile: %w", err)
	}
	if !profile.Enabled {
		return row, profile, errRuntimeProfileDisabled
	}

	row, err = qtx.UpsertAgentRuntimeWithProfile(ctx, build(profile))
	if err != nil {
		return row, profile, fmt.Errorf("upsert profile runtime: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return row, profile, fmt.Errorf("commit profile runtime registration: %w", err)
	}
	return row, profile, nil
}

// sharedDaemonCustomName returns the machine-level name shared by all of a
// daemon's runtimes — the same rule the frontend's sharedCustomName applies:
// every runtime must carry the identical non-empty custom_name. Returns
// ("", false) when the set is empty, any runtime is unnamed, or the names
// disagree (i.e. there is no single machine name to inherit).
func sharedDaemonCustomName(names []pgtype.Text) (string, bool) {
	if len(names) == 0 {
		return "", false
	}
	var first string
	for i, n := range names {
		if !n.Valid {
			return "", false
		}
		v := strings.TrimSpace(n.String)
		if v == "" {
			return "", false
		}
		if i == 0 {
			first = v
		} else if v != first {
			return "", false
		}
	}
	return first, true
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
	if len(req.Runtimes) == 0 && len(req.FailedProfiles) == 0 {
		writeError(w, http.StatusBadRequest, "at least one runtime or failed profile is required")
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
		// Persist what this daemon advertised, so surfaces that cannot see the
		// live request — the resource-save gate and the UI — decide from the
		// same signal the claim path uses, instead of re-deriving it from a
		// version string (MUL-5707).
		metadata, _ := json.Marshal(map[string]any{
			"version":      runtime.Version,
			"cli_version":  req.CLIVersion,
			"launched_by":  req.LaunchedBy,
			"capabilities": requestClientCapabilities(r),
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
			prow, profile, err := h.upsertRuntimeWithProfile(
				r.Context(),
				wsUUID,
				profileUUID,
				func(profile db.RuntimeProfile) db.UpsertAgentRuntimeWithProfileParams {
					return db.UpsertAgentRuntimeWithProfileParams{
						WorkspaceID: wsUUID,
						DaemonID:    strToText(req.DaemonID),
						Name:        name,
						RuntimeMode: "local",
						Provider:    profile.ProtocolFamily,
						Status:      status,
						DeviceInfo:  deviceInfo,
						Metadata:    metadata,
						OwnerID:     ownerID,
						ProfileID:   profileUUID,
					}
				},
			)
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, "unknown runtime profile: "+runtime.ProfileID)
				return
			}
			if errors.Is(err, errRuntimeProfileDisabled) {
				writeError(w, http.StatusConflict, "runtime profile is disabled: "+runtime.ProfileID)
				return
			}
			if err != nil {
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
			provider = profile.ProtocolFamily
			inserted = prow.Inserted
			registered = db.AgentRuntime{
				ID:             prow.ID,
				WorkspaceID:    prow.WorkspaceID,
				DaemonID:       prow.DaemonID,
				Name:           prow.Name,
				CustomName:     prow.CustomName,
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
				CustomName:     row.CustomName,
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

		// A brand-new runtime on an already-named machine inherits the machine's
		// shared custom name so the machine title stays stable as providers come
		// and go (MUL-4217). Shared with the failed-profile path below.
		registered = h.inheritMachineCustomName(r.Context(), registered, inserted)

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
	for _, failed := range req.FailedProfiles {
		profileID := strings.TrimSpace(failed.ProfileID)
		if profileID == "" {
			continue
		}
		profileUUID, pok := parseUUIDOrBadRequest(w, profileID, "profile_id")
		if !pok {
			return
		}
		reason := strings.TrimSpace(failed.Reason)
		if reason == "" {
			reason = "custom runtime command could not be resolved"
		}
		commandName := strings.TrimSpace(failed.CommandName)
		prow, _, err := h.upsertRuntimeWithProfile(
			r.Context(),
			wsUUID,
			profileUUID,
			func(profile db.RuntimeProfile) db.UpsertAgentRuntimeWithProfileParams {
				name := profile.DisplayName
				if req.DeviceName != "" {
					name = fmt.Sprintf("%s (%s)", name, req.DeviceName)
				}
				resolvedCommandName := commandName
				if resolvedCommandName == "" {
					resolvedCommandName = profile.CommandName
				}
				// Capabilities belong on this row too. It is written AFTER the
				// successful runtimes in the same request, so its last_seen_at is
				// the newest for this daemon until the first heartbeat — and the
				// worktree gates read the newest row. Omitting them made a failed
				// profile look, for that window, like a daemon that advertises
				// nothing (and now like a capability-blind server).
				metadata, _ := json.Marshal(map[string]any{
					"version":                            "",
					"cli_version":                        req.CLIVersion,
					"launched_by":                        req.LaunchedBy,
					"capabilities":                       requestClientCapabilities(r),
					"runtime_profile_registration_error": true,
					"runtime_profile_failure_reason":     reason,
					"command_name":                       resolvedCommandName,
				})
				return db.UpsertAgentRuntimeWithProfileParams{
					WorkspaceID: wsUUID,
					DaemonID:    strToText(req.DaemonID),
					Name:        name,
					RuntimeMode: "local",
					Provider:    profile.ProtocolFamily,
					Status:      "offline",
					DeviceInfo:  strings.TrimSpace(req.DeviceName),
					Metadata:    metadata,
					OwnerID:     ownerID,
					ProfileID:   profileUUID,
				}
			},
		)
		if err != nil {
			slog.Warn("failed to record runtime profile registration failure",
				"workspace_id", req.WorkspaceID, "daemon_id", req.DaemonID,
				"profile_id", profileID, "error", err)
			continue
		}
		// Keep the failed-profile row consistent with the machine's name so it
		// doesn't drag the machine title back to the hostname (MUL-4217).
		h.inheritMachineCustomName(r.Context(), db.AgentRuntime{
			ID:          prow.ID,
			WorkspaceID: prow.WorkspaceID,
			DaemonID:    prow.DaemonID,
			CustomName:  prow.CustomName,
		}, prow.Inserted)
	}

	slog.Info("daemon registered", "workspace_id", req.WorkspaceID, "daemon_id", req.DaemonID, "runtimes_count", len(resp))

	h.publish(protocol.EventDaemonRegister, req.WorkspaceID, "system", "", map[string]any{
		"runtimes": resp,
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

			if err := h.mergeLegacyRuntime(r.Context(), registered.ID, old.ID, legacyID, provider); err != nil {
				slog.Warn("legacy runtime merge failed",
					"legacy_daemon_id", legacyID, "old_runtime_id", oldID, "new_runtime_id", newID, "error", err)
			}
		}
	}
}

// errRuntimeMergeFenced reports that the task-write fence refused the
// reassignment because the target runtime's workspace is being deleted or is
// already gone. The merge is abandoned, not retried in place: the next daemon
// registration re-runs it, and by then either the workspace is gone entirely or
// the teardown rolled back.
var errRuntimeMergeFenced = errors.New("runtime merge refused by the task-write fence")

// mergeLegacyRuntime folds one legacy runtime row into the freshly registered one,
// as a single transaction.
//
// Order is load-bearing. The fence comes first — workspace row, then both runtime
// rows FOR UPDATE — and tasks move next, because that statement carries the same
// fence predicate every task write uses and is the only step that can tell us the
// target workspace is being torn down. Its verdict is separate from its row
// count for exactly this reason: "0 tasks reassigned" is also what a legacy runtime
// with no history looks like, and treating a fenced refusal as success would take
// us straight to DeleteAgentRuntime below — where agent_task_queue's
// ON DELETE CASCADE would delete the history this merge is supposed to carry
// forward (MUL-5999 review).
//
// One transaction, because the fence's workspace lock lives only as long as the
// statement that took it. Run as four autocommit statements, the merge had two
// ways to leave the tenant inconsistent and the registration reporting success:
//
//   - a transient failure on the agent reassignment left tasks pointing at the new
//     runtime while their agents stayed on the old one, self-healing only on some
//     later registration;
//   - worse, in the gap after the task commit a workspace teardown could lock and
//     delete the target runtime, whose UnbindTasksFromRuntime step clears
//     runtime_id on the tasks that had just moved. The next merge looks for tasks
//     by the OLD runtime id, so that history was permanently detached.
//
// Holding the fence lock from the first statement to COMMIT closes both: the
// teardown cannot start deleting the target until this merge finishes, and any
// failure rolls the whole merge back to its starting state.
func (h *Handler) mergeLegacyRuntime(ctx context.Context, newRuntimeID, oldRuntimeID pgtype.UUID, legacyID, provider string) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin merge tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	// Fence, in the same order every task write uses: workspace row first, then the
	// owner rows. Without the FOR UPDATE on the runtimes, a concurrent enqueue
	// against the OLD runtime slipped through after the task scan — writers hold
	// only FOR KEY SHARE on the workspace, and so did this merge, and two KEY SHARE
	// locks do not conflict — and DeleteAgentRuntime below then removed that
	// brand-new task through ON DELETE CASCADE.
	runtimeIDs := []pgtype.UUID{oldRuntimeID, newRuntimeID}
	if err := qtx.LockWorkspaceForRuntimeMerge(ctx, runtimeIDs); err != nil {
		return fmt.Errorf("lock workspace: %w", err)
	}
	locked, err := qtx.LockRuntimesForMerge(ctx, runtimeIDs)
	if err != nil {
		return fmt.Errorf("lock runtimes: %w", err)
	}
	if len(locked) != len(runtimeIDs) {
		// One of them went away while we were queueing for the lock; the fence
		// inside the reassignment would refuse anyway.
		return errRuntimeMergeFenced
	}

	reassignment, err := qtx.ReassignTasksToRuntime(ctx, db.ReassignTasksToRuntimeParams{
		NewRuntimeID: newRuntimeID,
		OldRuntimeID: oldRuntimeID,
	})
	if err != nil {
		return fmt.Errorf("reassign tasks: %w", err)
	}
	if !reassignment.FenceOk {
		return errRuntimeMergeFenced
	}

	agents, err := qtx.ReassignAgentsToRuntime(ctx, db.ReassignAgentsToRuntimeParams{
		NewRuntimeID: newRuntimeID,
		OldRuntimeID: oldRuntimeID,
	})
	if err != nil {
		return fmt.Errorf("reassign agents: %w", err)
	}

	// Inside the transaction this can no longer be best-effort: a failed statement
	// poisons the transaction, so it either lands with the rest of the merge or the
	// merge is rolled back.
	if err := qtx.RecordRuntimeLegacyDaemonID(ctx, db.RecordRuntimeLegacyDaemonIDParams{
		ID:             newRuntimeID,
		LegacyDaemonID: strToText(legacyID),
	}); err != nil {
		return fmt.Errorf("record legacy daemon_id: %w", err)
	}
	if err := qtx.DeleteAgentRuntime(ctx, oldRuntimeID); err != nil {
		return fmt.Errorf("delete old runtime: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit merge: %w", err)
	}
	h.NotifyRuntimeGone(uuidToString(oldRuntimeID))

	slog.Info("legacy runtime merged",
		"legacy_daemon_id", legacyID,
		"old_runtime_id", uuidToString(oldRuntimeID),
		"new_runtime_id", uuidToString(newRuntimeID),
		"provider", provider,
		"agents_reassigned", agents,
		"tasks_reassigned", reassignment.ReassignedTasks,
	)
	return nil
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

// setRuntimeOffline flips a runtime offline, recording the daemon's reason when
// it sent one. An absent or unusable reason falls back to the plain offline
// write: a malformed payload must never cost the caller the state change it
// actually asked for.
func (h *Handler) setRuntimeOffline(ctx context.Context, runtimeID pgtype.UUID, reason json.RawMessage) error {
	if len(reason) > 0 && json.Valid(reason) {
		return h.Queries.SetAgentRuntimeOfflineWithReason(ctx, db.SetAgentRuntimeOfflineWithReasonParams{
			ID:            runtimeID,
			OfflineReason: reason,
		})
	}
	return h.Queries.SetAgentRuntimeOffline(ctx, runtimeID)
}

// DaemonDeregister marks runtimes as offline when the daemon shuts down.
func (h *Handler) DaemonDeregister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RuntimeIDs []string `json:"runtime_ids"`
		// OfflineReasons is optional and keyed by runtime id. Present only for
		// causes the user must repair — a daemon shutting down sends none, so an
		// older daemon simply keeps today's behaviour (MUL-6164).
		OfflineReasons map[string]json.RawMessage `json:"offline_reasons"`
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
		rt, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceDaemonAPI, runtimeUUIDs[i])
		if err != nil {
			slog.Warn("deregister: runtime not found", "runtime_id", rid, "error", err)
			continue
		}

		wsID := uuidToString(rt.WorkspaceID)
		if !h.verifyDaemonWorkspaceAccess(r, wsID) {
			slog.Warn("deregister: workspace mismatch", "runtime_id", rid)
			continue
		}

		if err := h.setRuntimeOffline(r.Context(), rt.ID, req.OfflineReasons[rid]); err != nil {
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
	rt, lookupErr := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceHeartbeatHTTP, runtimeUUID)
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
	authMs = time.Since(start).Milliseconds()

	updateStart := time.Now()
	if err := h.recordHeartbeat(r.Context(), rt); err != nil {
		updateMs = time.Since(updateStart).Milliseconds()
		outcome = "error_update"
		writeError(w, http.StatusInternalServerError, "heartbeat failed")
		return
	}
	updateMs = time.Since(updateStart).Milliseconds()

	ack, m, err := h.processHeartbeat(r.Context(), runtimeID, req.SupportsBatchImport)
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

// HandleDaemonWSHeartbeat is the daemonws.HeartbeatHandler entry point. The
// WebSocket upgrade already batch-authenticated the fixed runtime set and
// captured each runtime's liveness state in a connection lease, so the hot
// path never reads agent_runtime. HTTP heartbeat remains the stateless lookup
// fallback for a daemon that stops receiving WebSocket acknowledgements.
func (h *Handler) HandleDaemonWSHeartbeat(ctx context.Context, identity daemonws.ClientIdentity, runtimeID string, supportsBatchImport bool) (*protocol.DaemonHeartbeatAckPayload, error) {
	lease := identity.RuntimeLeases[runtimeID]
	if lease == nil {
		return nil, fmt.Errorf("runtime not in connection lease")
	}
	// Defensive consistency assertion only: the workspace scope and lease came
	// from the same connection-time query. This does not re-authorize against DB.
	if !identity.AllowsWorkspace(lease.Snapshot().WorkspaceID) {
		return nil, fmt.Errorf("runtime not in connection workspace")
	}
	if err := h.recordHeartbeatLease(ctx, runtimeID, lease); err != nil {
		if isNotFound(err) {
			if h.DaemonRuntimeGone != nil {
				h.NotifyRuntimeGone(runtimeID)
				return nil, nil
			}
			return runtimeGoneHeartbeatAck(runtimeID), nil
		}
		return nil, err
	}
	ack, _, err := h.processHeartbeat(ctx, runtimeID, supportsBatchImport)
	return ack, err
}

func runtimeGoneHeartbeatAck(runtimeID string) *protocol.DaemonHeartbeatAckPayload {
	return &protocol.DaemonHeartbeatAckPayload{
		RuntimeID:   runtimeID,
		Status:      protocol.HeartbeatStatusRuntimeGone,
		RuntimeGone: true,
	}
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
	return h.recordHeartbeatState(ctx, rt.ID, uuidToString(rt.ID), heartbeatLivenessState{
		Status:          rt.Status,
		LastSeenAt:      rt.LastSeenAt.Time,
		LastSeenAtValid: rt.LastSeenAt.Valid,
		WorkspaceID:     rt.WorkspaceID,
	}, nil)
}

func (h *Handler) recordHeartbeatLease(ctx context.Context, runtimeID string, lease *daemonws.RuntimeLease) error {
	runtimeUUID, err := util.ParseUUID(runtimeID)
	if err != nil {
		return fmt.Errorf("invalid runtime_id: %w", err)
	}
	state := lease.Snapshot()
	// Lenient parse: the workspace ID only feeds the recovery refresh payload.
	// An invalid value suppresses the event instead of failing the heartbeat.
	wsUUID, _ := util.ParseUUID(state.WorkspaceID)
	return h.recordHeartbeatState(ctx, runtimeUUID, runtimeID, heartbeatLivenessState{
		Status:          state.Status,
		LastSeenAt:      state.LastSeenAt,
		LastSeenAtValid: state.LastSeenAtValid,
		WorkspaceID:     wsUUID,
	}, lease.MarkDBWriteScheduled)
}

type heartbeatLivenessState struct {
	Status          string
	LastSeenAt      time.Time
	LastSeenAtValid bool
	// WorkspaceID feeds the recovery lifecycle refresh; it is already known
	// from the lease snapshot or the HTTP row and never re-read from the DB.
	WorkspaceID pgtype.UUID
}

func (h *Handler) recordHeartbeatState(
	ctx context.Context,
	runtimeUUID pgtype.UUID,
	runtimeID string,
	state heartbeatLivenessState,
	markDBWriteScheduled func(time.Time),
) error {
	now := time.Now()

	// Decide whether the DB row needs a write *before* touching Redis, so a
	// Touch failure can simply force needDBWrite=true without re-evaluating
	// the structural reasons.
	needDBWrite := !h.LivenessStore.Available() ||
		state.Status != "online" ||
		!state.LastSeenAtValid ||
		now.Sub(state.LastSeenAt) >= runtimeHeartbeatDBFlushInterval

	if h.LivenessStore.Available() {
		if err := h.LivenessStore.Touch(ctx, runtimeID, runtimeLivenessTTL); err != nil {
			// Redis hiccup: degrade transparently to the DB-only path for
			// this beat. The sweeper falls back to its DB threshold the
			// same way when IsAliveBatch fails, so end-to-end correctness
			// is preserved.
			slog.Warn("liveness touch failed; falling back to DB heartbeat",
				"runtime_id", runtimeID, "error", err)
			needDBWrite = true
		}
	}

	if !needDBWrite {
		return nil
	}

	// Status transitions remain synchronous so the acknowledgement cannot race
	// dependent work that expects an online row. The steady-state online bump
	// is ID-only and may be coalesced by the production scheduler.
	if state.Status != "online" || !state.LastSeenAtValid {
		// The conditional update reports whether this beat performed the
		// offline → online flip. Only an actual flip publishes the lifecycle
		// refresh; a beat that lost the race (or a never-seen row already
		// online) stays silent and keeps the unconditional update below so
		// last_seen_at is bumped and pgx.ErrNoRows is preserved for deletions.
		flipped, err := h.Queries.MarkAgentRuntimeOnlineIfOffline(ctx, runtimeUUID)
		if err != nil {
			return err
		}
		if flipped > 0 {
			// Reuse daemon:register so web and mobile invalidate both runtime and
			// agent projections. Ordinary online heartbeats stay silent.
			if state.WorkspaceID.Valid {
				h.PublishRuntimeRefresh(uuidToString(state.WorkspaceID), "system", "", "heartbeat_recovery")
			}
		} else if _, err := h.Queries.MarkAgentRuntimeOnline(ctx, runtimeUUID); err != nil {
			return err
		}
		if markDBWriteScheduled != nil {
			markDBWriteScheduled(now)
		}
		return nil
	}
	if err := h.HeartbeatScheduler.Schedule(ctx, runtimeUUID, state.WorkspaceID); err != nil {
		return err
	}
	if markDBWriteScheduled != nil {
		markDBWriteScheduled(now)
	}
	return nil
}

// heartbeatMetrics carries per-stage timings out of processHeartbeat so the
// HTTP slow-log can stay structured. The WS path discards them.
type heartbeatMetrics struct {
	ProbeModelMs, PopModelMs, ProbeSkillsMs, PopSkillsMs, ProbeImportMs, PopImportMs int64
	ProbeModelTimedOut, ProbeSkillsTimedOut, ProbeImportTimedOut                     bool
}

// processHeartbeat pulls pending actions for both HTTP and WebSocket
// heartbeats using only the runtime ID. Each transport records liveness first:
// HTTP uses its stateless runtime row, while WebSocket uses the connection
// lease. Auth and request decoding also remain transport-specific.
func (h *Handler) processHeartbeat(ctx context.Context, runtimeID string, supportsBatchImport bool) (*protocol.DaemonHeartbeatAckPayload, heartbeatMetrics, error) {
	var m heartbeatMetrics

	slog.Debug("daemon heartbeat", "runtime_id", runtimeID)

	ack := &protocol.DaemonHeartbeatAckPayload{
		RuntimeID:          runtimeID,
		Status:             "ok",
		ServerCapabilities: []string{protocol.DaemonCapabilityRPCV1},
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
func logClaimEndpointSlow(runtimeID, outcome string, start time.Time, authMs, claimMs, buildMs int64, payloadBytes, agentSkillCount, builtinSkillCount, skillPayloadBytes int) {
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
		"payload_bytes", payloadBytes,
		"agent_skill_count", agentSkillCount,
		"builtin_skill_count", builtinSkillCount,
		"skill_payload_bytes", skillPayloadBytes,
	)
}

// requestHasClientCapability reports whether the caller advertised a capability
// in X-Client-Capabilities. Daemons and app clients share the header.
// requestClientCapabilities returns the advertised capability list, trimmed and
// with empties dropped. Nil when the header is absent — which is exactly what an
// older daemon looks like.
func requestClientCapabilities(r *http.Request) []string {
	raw := strings.Split(r.Header.Get("X-Client-Capabilities"), ",")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// runtimeHasCapability reports whether a stored runtime row advertised the
// capability at registration. Absent metadata means an older daemon that never
// sent the header, so this fails closed.
func runtimeHasCapability(metadata []byte, capability string) bool {
	if len(metadata) == 0 {
		return false
	}
	var m struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(metadata, &m); err != nil {
		return false
	}
	for _, c := range m.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

func requestHasClientCapability(r *http.Request, capability string) bool {
	for _, part := range strings.Split(r.Header.Get("X-Client-Capabilities"), ",") {
		if strings.TrimSpace(part) == capability {
			return true
		}
	}
	return false
}

func parseRuntimeConnectedAppsForClaim(raw []byte, taskID pgtype.UUID) []runtimeapps.ConnectedApp {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var apps []runtimeapps.ConnectedApp
	if err := json.Unmarshal(raw, &apps); err != nil {
		slog.Warn("daemon claim: unmarshal runtime_connected_apps failed",
			"task_id", uuidToString(taskID),
			"error", err,
		)
		return nil
	}
	return apps
}

// repairStaleCommentPlanIfNeeded handles the edit/delete race where a claimed
// task's trigger_comment_id was cleared but coalesced_comment_ids survive: such
// a task must never be dispatched as a generic assignment — its user-scoped MCP
// overlay still belongs to the deleted author, and the prompt would read issue
// history exposing that stale user's capabilities. When it applies, the task is
// cancelled and its surviving comments are replayed through normal routing
// (which recomputes originator + connected-app context).
//
// Returns handled=true when the task must NOT be dispatched — either a clean
// repair (failure==nil) or a hard failure (failure!=nil, carrying the
// status/message/outcome the per-runtime endpoint renders). handled=false means
// proceed with a normal claim. Shared by the per-runtime and batch claim
// handlers so the batch path can't silently drop surviving comments (MUL-4257).
func (h *Handler) repairStaleCommentPlanIfNeeded(ctx context.Context, task *db.AgentTaskQueue, runtimeWorkspaceID string) (handled bool, failure *claimBuildFailure) {
	if task.TriggerCommentID.Valid || len(task.CoalescedCommentIds) == 0 {
		return false, nil
	}
	if !task.IssueID.Valid {
		return true, &claimBuildFailure{outcome: "error_stale_comment_plan", status: http.StatusInternalServerError, message: "comment task has no issue"}
	}
	issue, loadErr := h.Queries.GetIssue(ctx, task.IssueID)
	if loadErr != nil {
		return true, &claimBuildFailure{outcome: "error_stale_comment_plan", status: http.StatusInternalServerError, message: "failed to repair stale comment task"}
	}
	if uuidToString(issue.WorkspaceID) != runtimeWorkspaceID {
		if _, cancelErr := h.TaskService.CancelTask(ctx, task.ID); cancelErr != nil {
			slog.Error("task claim: cancel stale cross-workspace task failed",
				"task_id", uuidToString(task.ID), "error", cancelErr)
		}
		return true, &claimBuildFailure{outcome: "error_workspace", status: http.StatusInternalServerError, message: "task workspace isolation check failed"}
	}
	cancelled, cancelErr := h.TaskService.CancelTask(ctx, task.ID)
	if cancelErr != nil {
		return true, &claimBuildFailure{outcome: "error_stale_comment_plan", status: http.StatusInternalServerError, message: "failed to repair stale comment task"}
	}
	h.retriggerCancelledTaskSurvivors(ctx, issue, []db.AgentTaskQueue{*cancelled}, pgtype.UUID{})
	return true, nil
}

// claimBatchMaxTasksCap bounds how many tasks a single machine-level batch
// claim may return, so one request can neither build an unbounded payload nor
// hold the DB for an unbounded number of per-agent claim transactions. The
// daemon never asks for more than its free execution-slot count anyway.
const claimBatchMaxTasksCap = 32

// ClaimTasksByRuntime is the machine-level (MUL-4257) batch claim endpoint. A
// daemon posts every runtime_id it hosts plus its free execution-slot count and
// receives up to max_tasks already-claimed tasks in ONE round trip — each
// carrying its runtime_id so the daemon routes it to the matching runtime
// locally. This collapses the per-runtime idle-poll fan-out (one HTTP request
// plus one promote/reclaim/list cycle per runtime) into a single request backed
// by = ANY merged queries.
//
// Each returned task goes through the same FinalizeTaskClaim as the per-runtime
// endpoint, so the task-scoped token AND the comment-delivery receipt
// (delivered_comment_ids) are persisted atomically; a finalization failure
// requeues that exact claim and omits it from the batch. Unknown/unauthorized
// runtime_ids are skipped silently (a daemon may send a just-deleted runtime;
// it self-heals via the heartbeat path).
func (h *Handler) ClaimTasksByRuntime(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req struct {
		DaemonID   string   `json:"daemon_id"`
		RuntimeIDs []string `json:"runtime_ids"`
		MaxTasks   int      `json:"max_tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Machine-level ownership (MUL-4257 review): the batch claim is scoped to a
	// single daemon. daemon_id is required so the server can reject any
	// runtime_id that belongs to a different machine (guards against a stale /
	// crossed runtime set claiming another daemon's tasks — which would land
	// local_directory / machine-local work on the wrong host). For an mdt_
	// token the body daemon_id must equal the token's daemon_id, so a
	// workspace-scoped token can't spoof a peer.
	if req.DaemonID == "" {
		writeError(w, http.StatusBadRequest, "daemon_id is required")
		return
	}
	if ctxDaemonID := middleware.DaemonIDFromContext(r.Context()); ctxDaemonID != "" && ctxDaemonID != req.DaemonID {
		writeError(w, http.StatusForbidden, "daemon_id does not match token")
		return
	}

	// max_tasks semantics (MUL-4257 review): a negative count is malformed; zero
	// is a valid "no free slots" poll that must claim nothing — never coerce to
	// 1, which would dispatch a task the daemon cannot run and strand it until
	// stale reclaim. Positive counts are capped so one request can't build an
	// unbounded payload.
	if req.MaxTasks < 0 {
		writeError(w, http.StatusBadRequest, "max_tasks must not be negative")
		return
	}
	if req.MaxTasks == 0 {
		writeMeasuredJSON(w, http.StatusOK, map[string]any{"tasks": []AgentTaskResponse{}})
		return
	}
	maxTasks := req.MaxTasks
	if maxTasks > claimBatchMaxTasksCap {
		maxTasks = claimBatchMaxTasksCap
	}

	// Parse + de-dup requested ids with the NON-panicking parser (MUL-4257
	// review): the handler-local parseUUID panics on malformed input, which
	// would turn a single bad id into a 500. Invalid ids are skipped, matching
	// this endpoint's "unknown id skipped" semantics. Key by the canonical uuid
	// string so the post-claim lookup by task.RuntimeID always matches.
	idByKey := make(map[string]pgtype.UUID, len(req.RuntimeIDs))
	for _, rid := range req.RuntimeIDs {
		ruid, err := util.ParseUUID(rid)
		if err != nil {
			continue
		}
		idByKey[util.UUIDToString(ruid)] = ruid
	}
	if len(idByKey) == 0 {
		writeMeasuredJSON(w, http.StatusOK, map[string]any{"tasks": []AgentTaskResponse{}})
		return
	}
	ids := make([]pgtype.UUID, 0, len(idByKey))
	for _, id := range idByKey {
		ids = append(ids, id)
	}

	// Resolve all requested runtimes in one query (instead of a point lookup
	// per runtime), then authorize each; skip (don't fail) unknown/unauthorized
	// ids so a single stale runtime can't sink the whole batch.
	runtimes, err := h.Queries.GetAgentRuntimes(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load runtimes")
		return
	}
	runtimeByID := make(map[string]db.AgentRuntime, len(runtimes))
	authorized := make([]pgtype.UUID, 0, len(runtimes))
	for _, rt := range runtimes {
		if !h.verifyDaemonWorkspaceAccess(r, uuidToString(rt.WorkspaceID)) {
			continue
		}
		// Group-ownership check (mirrors the WS path, daemon_ws.go): a runtime
		// bound to a different daemon must not be claimed by this one. Runtimes
		// with a NULL daemon_id (e.g. cloud runtimes) are not machine-pinned, so
		// they stay claimable — same tolerance as the WS handler.
		if rt.DaemonID.Valid && rt.DaemonID.String != req.DaemonID {
			continue
		}
		runtimeByID[uuidToString(rt.ID)] = rt
		authorized = append(authorized, rt.ID)
	}
	if len(authorized) == 0 {
		writeMeasuredJSON(w, http.StatusOK, map[string]any{"tasks": []AgentTaskResponse{}})
		return
	}

	claimed, err := h.TaskService.ClaimTasksForRuntimes(r.Context(), authorized, maxTasks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim tasks: "+err.Error())
		return
	}

	out := make([]AgentTaskResponse, 0, len(claimed))
	for i := range claimed {
		task := claimed[i]
		rt, ok := runtimeByID[uuidToString(task.RuntimeID)]
		if !ok {
			// Service guards claims to the authorized set; a miss here would be
			// a stray cross-daemon claim. Leave it for the owning daemon's
			// reclaim path rather than shipping it to the wrong machine.
			continue
		}
		rtWorkspaceID := uuidToString(rt.WorkspaceID)
		// Stale comment-plan repair must run for the batch path too: otherwise a
		// task whose trigger was deleted (only coalesced survive) would be
		// finalized+dispatched with no comment input, silently dropping the
		// surviving user comment. On repair (or hard failure) the task is
		// cancelled / left for reclaim and omitted from the batch.
		if handled, _ := h.repairStaleCommentPlanIfNeeded(r.Context(), &task, rtWorkspaceID); handled {
			continue
		}
		resp, deliveredCommentIDs, _, _, failure := h.buildClaimedTaskResponse(r, &task, rt, uuidToString(task.RuntimeID), rtWorkspaceID)
		if failure != nil {
			// Builder rejected this task (workspace isolation / chat-input);
			// it has already settled the task where the failure requires it.
			// Skip it — non-settling failures leave the task dispatched for
			// the reclaim path.
			continue
		}
		if !rt.OwnerID.Valid {
			slog.Error("batch claim: runtime owner missing; cancelling task to avoid unscoped agent credentials",
				"task_id", uuidToString(task.ID), "runtime_id", uuidToString(task.RuntimeID))
			if _, cerr := h.TaskService.CancelTask(r.Context(), task.ID); cerr != nil {
				slog.Error("batch claim: cancel after missing runtime owner failed",
					"task_id", uuidToString(task.ID), "error", cerr)
			}
			continue
		}
		tokenStr, terr := auth.GenerateAgentTaskToken()
		if terr != nil {
			slog.Error("batch claim: generate task token failed; requeueing claim",
				"task_id", uuidToString(task.ID), "error", terr)
			if _, rerr := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), task); rerr != nil {
				slog.Error("batch claim: requeue after token-gen failure failed",
					"task_id", uuidToString(task.ID), "error", rerr)
			}
			continue
		}
		remoteMCPToken, daemonTokens, derr := remoteMCPDaemonTokenForClaim(resp, rt)
		if derr != nil {
			slog.Error("batch claim: generate Remote MCP daemon token failed; requeueing claim",
				"task_id", uuidToString(task.ID), "error", derr)
			if _, rerr := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), task); rerr != nil {
				slog.Error("batch claim: requeue after Remote MCP token failure failed",
					"task_id", uuidToString(task.ID), "error", rerr)
			}
			continue
		}
		// Route through the SAME finalization as the per-runtime endpoint so the
		// token and the comment-delivery receipt (delivered_comment_ids for
		// comment/coalesced-comment tasks) are persisted atomically; on failure
		// the exact claim is requeued and omitted from this batch.
		commentBackedTask := task.TriggerCommentID.Valid || len(task.CoalescedCommentIds) > 0
		receipt, ferr := h.TaskService.FinalizeTaskClaim(r.Context(), task, db.CreateTaskTokenParams{
			ID:          dbid.NewV7(),
			TokenHash:   auth.HashToken(tokenStr),
			TaskID:      task.ID,
			AgentID:     task.AgentID,
			WorkspaceID: parseUUID(resp.WorkspaceID),
			UserID:      rt.OwnerID,
			ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
		}, deliveredCommentIDs, commentBackedTask, daemonTokens...)
		if ferr != nil {
			slog.Error("batch claim: finalize task claim failed; requeueing claim",
				"task_id", uuidToString(task.ID), "error", ferr)
			if _, rerr := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), task); rerr != nil {
				slog.Error("batch claim: requeue after finalize failure failed",
					"task_id", uuidToString(task.ID), "error", rerr)
			}
			continue
		}
		resp.AuthToken = tokenStr
		resp.RemoteMCPDaemonToken = remoteMCPToken
		resp.DeliveredCommentIDs = uuidStringsOrEmpty(receipt)
		out = append(out, resp)
	}

	if len(out) > 0 {
		slog.Info("tasks claimed by runtime batch",
			"runtimes", len(authorized), "requested_max", maxTasks, "claimed", len(out),
			"total_ms", time.Since(start).Milliseconds())
	}
	response := map[string]any{"tasks": out}
	// Only opted-in daemons understand this additive response metadata. Query
	// after the claim so a future fire_at can shorten the long healthy-WS safety
	// poll; a task that crossed fire_at during this request yields a bounded
	// follow-up hint and is promoted on the next claim. If the lookup fails,
	// omit the support bit so the daemon conservatively retains its ordinary
	// PollInterval.
	if len(out) < maxTasks && requestHasClientCapability(r, protocol.DaemonCapabilityClaimPollHintsV1) {
		nextDeferred, nextErr := h.Queries.NextDeferredTaskFireAtForRuntimes(r.Context(), db.NextDeferredTaskFireAtForRuntimesParams{
			RuntimeIds:       authorized,
			RuntimeStaleSecs: service.RuntimeClaimFreshnessSeconds,
		})
		if nextErr != nil {
			slog.Warn("batch claim: next deferred task lookup failed; retaining short client poll",
				"error", nextErr)
		} else {
			response["claim_poll_hint_supported"] = true
			if nextDeferred.Valid {
				response["next_deferred_task_after_ms"] = claimPollHintDelay(time.Now(), nextDeferred.Time).Milliseconds()
			}
		}
	}
	writeMeasuredJSON(w, http.StatusOK, response)
}

func claimPollHintDelay(now, fireAt time.Time) time.Duration {
	delay := fireAt.Sub(now)
	if delay < claimPollHintMinDelay {
		return claimPollHintMinDelay
	}
	return delay
}

// claimBuildFailure captures a pre-response failure from
// buildClaimedTaskResponse (workspace isolation, chat-input load/empty, ...) so
// the per-runtime handler can render the exact status/message/outcome and the
// batch handler can skip the task. Any task cancellation is already performed
// inside the builder before it returns one.
type claimBuildFailure struct {
	outcome string
	status  int
	message string
}

// rejectClaimSourceLoad settles a claim whose SOURCE row — the issue, chat
// session, autopilot_run or autopilot the task hangs off — could not be read.
//
// Every one of these branches used to swallow the error and fall through on
// `err == nil`. That is a fail-open: taskToResponse has already seeded
// resp.WorkspaceID with the RUNTIME's workspace, so a swallowed read ran on to
// the builder's backstop isolation check, which compared that seeded value
// against itself and passed. The agent was then dispatched with no issue, no
// chat input, or no autopilot instructions at all — and, for chat, past the
// empty-input guard that lives inside the success branch.
//
// The two failures settle differently:
//
//   - The read FAILED (transient: DB blip, timeout, reset). Preserve the
//     just-dispatched task so the stale-dispatched reclaim redelivers it,
//     exactly as the chat-input load does (MUL-4351). This is the reachable
//     case and the one worth fixing.
//   - The row is GONE (ErrNoRows). Cancel: the task can never run, and
//     preserving it would spin in the reclaim loop forever. Legacy FKs
//     currently keep every one of these states from arising —
//     agent_task_queue.issue_id ON DELETE CASCADE,
//     agent_task_queue.chat_session_id ON DELETE SET NULL, and
//     autopilot_run.autopilot_id ON DELETE CASCADE feeding
//     agent_task_queue.autopilot_run_id ON DELETE SET NULL — so this branch is
//     defensive. It is what protects the claim path when those FKs are dropped
//     for the repo's no-FK rule.
func (h *Handler) rejectClaimSourceLoad(ctx context.Context, task *db.AgentTaskQueue, err error, source, sourceID string) *claimBuildFailure {
	if errors.Is(err, pgx.ErrNoRows) {
		slog.Error("task claim: source row is missing; cancelling task",
			"task_id", uuidToString(task.ID), "source", source, "source_id", sourceID)
		if _, cerr := h.TaskService.CancelTask(ctx, task.ID); cerr != nil {
			slog.Error("task claim: cancel after missing source row failed",
				"task_id", uuidToString(task.ID), "error", cerr)
		}
		return &claimBuildFailure{
			outcome: "error_source_missing",
			status:  http.StatusInternalServerError,
			message: fmt.Sprintf("task is missing its %s", source),
		}
	}
	slog.Error("task claim: source load failed; preserving task for redelivery",
		"task_id", uuidToString(task.ID), "source", source, "source_id", sourceID, "error", err)
	return &claimBuildFailure{
		outcome: "error_source_load",
		status:  http.StatusInternalServerError,
		message: fmt.Sprintf("failed to load task %s", source),
	}
}

// rejectClaimSkillLoad settles a claim whose agent skills could not be read.
// It mirrors rejectClaimSourceLoad's transient branch — preserve the
// just-dispatched task so the stale-dispatched reclaim redelivers it — because
// this is the same kind of failure: a transient read on the claim-build path.
//
// Dispatching with whatever loaded is not the safer half-measure it looks
// like. LoadAgentSkills reads the whole skill set in two queries, so a failure
// means every skill loses its supporting files or the agent loses every skill,
// and the ref hashes sent to the daemon are computed over that same truncated
// content — so the daemon validates the bundle, caches it, and the agent runs
// with rules silently missing. There is no ErrNoRows branch to mirror: an
// agent with no skills is a legitimate empty result, not a missing row.
func (h *Handler) rejectClaimSkillLoad(task *db.AgentTaskQueue, err error) *claimBuildFailure {
	slog.Error("task claim: agent skill load failed; preserving task for redelivery",
		"task_id", uuidToString(task.ID), "agent_id", uuidToString(task.AgentID), "error", err)
	return &claimBuildFailure{
		outcome: "error_skill_load",
		status:  http.StatusInternalServerError,
		message: "failed to load agent skills",
	}
}

// rejectClaimOnWorkspaceMismatch enforces the claim's tenant boundary against
// the workspace that OWNS the task's context (issue / chat session / autopilot
// / quick-create), which is the only authority for MULTICA_WORKSPACE_ID in the
// agent env. An empty value would make the CLI silently fall back to the
// user-global config and talk to whatever workspace the user happened to last
// configure; a value that doesn't match the runtime's workspace means upstream
// routed a foreign-workspace task here. Both cases hard-fail AND cancel the
// just-dispatched task so the queue / agent status don't sit stuck until the
// stale-task sweeper fires minutes later.
//
// Every branch calls this as soon as it knows its authoritative workspace and
// BEFORE it hydrates project context, so "prove ownership, then load" is a
// property of the structure rather than a check each branch remembers to
// repeat. Returns nil when the claim may proceed.
func (h *Handler) rejectClaimOnWorkspaceMismatch(ctx context.Context, task *db.AgentTaskQueue, resolvedWorkspaceID, runtimeID, runtimeWorkspaceID string, hasQuickCreate bool) *claimBuildFailure {
	if resolvedWorkspaceID != "" && resolvedWorkspaceID == runtimeWorkspaceID {
		return nil
	}
	slog.Error("task claim: workspace isolation check failed, cancelling task",
		"task_id", uuidToString(task.ID),
		"runtime_id", runtimeID,
		"runtime_workspace", runtimeWorkspaceID,
		"resolved_workspace", resolvedWorkspaceID,
		"has_issue", task.IssueID.Valid,
		"has_chat", task.ChatSessionID.Valid,
		"has_autopilot_run", task.AutopilotRunID.Valid,
		"has_quick_create", hasQuickCreate,
	)
	if _, cerr := h.TaskService.CancelTask(ctx, task.ID); cerr != nil {
		slog.Error("task claim: cancel after workspace check failed",
			"task_id", uuidToString(task.ID), "error", cerr)
	}
	return &claimBuildFailure{
		outcome: "error_workspace",
		status:  http.StatusInternalServerError,
		message: "task workspace isolation check failed",
	}
}

// remoteMCPDaemonTokenForClaim prepares the short-lived credential the daemon
// uses to resolve write-only Remote MCP secrets for this task. The raw token is
// returned only in the claim response; its hash is committed atomically with
// the task-scoped agent token by FinalizeTaskClaim.
func remoteMCPDaemonTokenForClaim(resp AgentTaskResponse, runtime db.AgentRuntime) (string, []db.CreateDaemonTokenParams, error) {
	if len(resp.RemoteMCPConnections) == 0 {
		return "", nil, nil
	}
	if !runtime.DaemonID.Valid || strings.TrimSpace(runtime.DaemonID.String) == "" {
		return "", nil, errors.New("runtime daemon_id is required for Remote MCP")
	}
	raw, err := auth.GenerateDaemonToken()
	if err != nil {
		return "", nil, fmt.Errorf("generate Remote MCP daemon token: %w", err)
	}
	return raw, []db.CreateDaemonTokenParams{{
		TokenHash:   auth.HashToken(raw),
		WorkspaceID: runtime.WorkspaceID,
		DaemonID:    runtime.DaemonID.String,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}}, nil
}

// failClaimedTaskBeforeLaunch settles a durable claim-time rejection before
// the daemon ever receives the task. Leaving these failures in dispatched
// makes the stale-claim reaper deliver the same impossible task forever and
// leaves chat showing "starting" with no actionable outcome. If settlement
// itself fails, release the exact claim so a later attempt can retry the gate.
func (h *Handler) failClaimedTaskBeforeLaunch(
	ctx context.Context,
	task *db.AgentTaskQueue,
	userMessage string,
	failureReason taskfailure.Reason,
	outcome string,
	status int,
	claimMessage string,
) *claimBuildFailure {
	if _, err := h.TaskService.FailTask(
		ctx,
		task.ID,
		userMessage,
		"",
		"",
		"",
		failureReason.String(),
		false,
		"",
		"",
	); err != nil {
		slog.Error("task claim: fail rejected task failed; requeueing claim",
			"task_id", uuidToString(task.ID),
			"outcome", outcome,
			"error", err,
		)
		if _, requeueErr := h.TaskService.RequeueTaskAfterClaimFailure(ctx, *task); requeueErr != nil {
			slog.Error("task claim: requeue after rejected-task settlement failure failed; stale reclaim will recover it",
				"task_id", uuidToString(task.ID),
				"outcome", outcome,
				"error", requeueErr,
			)
		}
		return &claimBuildFailure{
			outcome: outcome + "_settle",
			status:  http.StatusInternalServerError,
			message: "failed to settle a task rejected before launch",
		}
	}
	return &claimBuildFailure{outcome: outcome, status: status, message: claimMessage}
}

func chatSessionResumeFallbackNeeded(priorSessionID, priorWorkDir string) bool {
	return priorSessionID == "" || priorWorkDir == ""
}

func rerunSourceMatchesTaskScope(task, source db.AgentTaskQueue) bool {
	if task.AgentID != source.AgentID {
		return false
	}
	if task.IssueID.Valid {
		return source.IssueID.Valid && task.IssueID == source.IssueID
	}
	if task.ChatSessionID.Valid {
		return source.ChatSessionID.Valid && task.ChatSessionID == source.ChatSessionID
	}
	return false
}

func claimResponseAgentIdentityMatches(resp AgentTaskResponse) bool {
	return resp.AgentID != "" && resp.Agent != nil && resp.Agent.ID == resp.AgentID
}

// buildClaimedTaskResponse assembles the full daemon claim payload for a
// single already-claimed task and computes the exact comment ids embedded in
// it (deliveredCommentIDs). Shared by the per-runtime handler
// (ClaimTaskByRuntime) and the machine-level batch handler
// (ClaimTasksByRuntime, MUL-4257) so both build byte-identical payloads and
// feed the same delivery receipt into FinalizeTaskClaim. A non-nil failure
// means the task must not be dispatched; the builder has already cancelled it
// where the failure semantics require it.
func (h *Handler) buildClaimedTaskResponse(r *http.Request, task *db.AgentTaskQueue, runtime db.AgentRuntime, runtimeID, runtimeWorkspaceID string) (resp AgentTaskResponse, deliveredCommentIDs []pgtype.UUID, agentSkillCount, builtinSkillCount int, failure *claimBuildFailure) {
	// Build response with fresh agent data (name + skills + custom_env + custom_args).
	resp = taskToResponse(*task, runtimeWorkspaceID)
	var issueNumber int32
	// Claim-only capability: this server resolves the squad-leader role on the
	// wire (is_leader_task / squad_id), so the daemon must not re-derive it
	// from the briefing text. Set unconditionally — on every claim, leader or
	// not — because its absence is what tells an upgraded daemon it is talking
	// to a server too old to have answered the question (MUL-5811).
	resp.LeaderRoleResolved = true
	// Agent-trigger plugin hooks, as tools. A failure here degrades to no
	// tools rather than failing the claim: a plugin that cannot be listed must
	// not stop an agent from working on the issue.
	if h.PluginService != nil && h.pluginsV1Enabled(r.Context()) {
		if tools, toolErr := h.PluginService.AgentHookTools(r.Context(), parseUUID(runtimeWorkspaceID)); toolErr != nil {
			slog.Warn("plugins: could not list agent hook tools", "workspace_id", runtimeWorkspaceID, "error", toolErr)
		} else {
			resp.PluginHookTools = tools
		}
		// mcp-transport hooks ride the EXISTING broker: the connection shape is
		// what validatePinnedRemoteMCPTools already reads, so an approved tool
		// that went missing or whose schema drifted refuses at startup without
		// any new enforcement code.
		if connections, connErr := h.PluginService.AgentMCPConnections(r.Context(), parseUUID(runtimeWorkspaceID)); connErr != nil {
			slog.Warn("plugins: could not list agent MCP connections", "workspace_id", runtimeWorkspaceID, "error", connErr)
		} else if len(connections) > 0 {
			resp.RemoteMCPConnections = append(resp.RemoteMCPConnections, connections...)
		}
	}
	supportsCoalescedComments := requestHasClientCapability(r, protocol.DaemonCapabilityCoalescedCommentsV1)
	// Empty-but-non-nil so pgx persists '{}' rather than NULL for tasks without
	// comment input. Comment tasks replace this with the ids actually embedded
	// in the capability-aware response built below.
	deliveredCommentIDs = []pgtype.UUID{}
	composioMCPEnabled := h.composioMCPAppsEnabled(r.Context())
	if composioMCPEnabled {
		resp.ConnectedApps = parseRuntimeConnectedAppsForClaim(task.RuntimeConnectedApps, task.ID)
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The current schema cascades agent deletion to task rows, so a
			// durable orphan task cannot normally reach this branch. It remains a
			// fail-closed guard for a deletion racing response assembly or for
			// externally corrupted data; settlement may report that the task row
			// already disappeared, but no claim payload is dispatched either way.
			slog.Error("daemon claim: task agent no longer exists; refusing dispatch",
				"task_id", uuidToString(task.ID), "agent_id", uuidToString(task.AgentID))
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, h.failClaimedTaskBeforeLaunch(
				r.Context(), task,
				"Task identity is invalid: the assigned agent no longer exists.",
				taskfailure.ReasonInvalidTaskIdentity,
				"error_invalid_task_identity", http.StatusConflict, "task agent no longer exists",
			)
		}
		slog.Error("daemon claim: load task agent failed; requeueing claim",
			"task_id", uuidToString(task.ID), "agent_id", uuidToString(task.AgentID), "error", err)
		if _, requeueErr := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), *task); requeueErr != nil {
			slog.Error("daemon claim: requeue after agent load failure failed; stale reclaim will recover it",
				"task_id", uuidToString(task.ID), "error", requeueErr)
		}
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
			outcome: "error_load_task_agent",
			status:  http.StatusInternalServerError,
			message: "failed to load task agent",
		}
	}
	// The SQL claim narrows candidates and repeats the access predicate before
	// changing task state, but Agent mutations do not stay locked through HTTP
	// response assembly. Recheck the freshly loaded Agent here so a rebind or
	// owner change that committed after the claim cannot reach the daemon.
	if agent.RuntimeID != task.RuntimeID {
		slog.Warn("daemon claim: agent runtime changed before delivery; refusing dispatch",
			"task_id", uuidToString(task.ID),
			"agent_id", uuidToString(task.AgentID),
			"task_runtime_id", uuidToString(task.RuntimeID),
			"agent_runtime_id", uuidToString(agent.RuntimeID),
		)
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, h.failClaimedTaskBeforeLaunch(
			r.Context(), task,
			"The agent moved to another runtime before this task could start. Retry the task to run it on the agent's current runtime.",
			taskfailure.ReasonInvalidTaskIdentity,
			"error_agent_runtime_changed", http.StatusConflict, "agent runtime changed before task delivery",
		)
	}
	if runtime.Visibility == "private" && runtime.OwnerID.Valid &&
		(!agent.OwnerID.Valid || agent.OwnerID != runtime.OwnerID) {
		userMessage := "This private runtime cannot run the assigned agent because the agent and runtime have different owners."
		if !agent.OwnerID.Valid {
			userMessage = "This private runtime cannot run the assigned agent because the agent has no owner."
		}
		slog.Warn("daemon claim: private runtime no longer permits task agent; refusing dispatch",
			"task_id", uuidToString(task.ID),
			"agent_id", uuidToString(task.AgentID),
			"runtime_id", runtimeID,
			"agent_owner_valid", agent.OwnerID.Valid,
		)
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, h.failClaimedTaskBeforeLaunch(
			r.Context(), task,
			userMessage,
			taskfailure.ReasonInvalidTaskIdentity,
			"error_runtime_access_denied", http.StatusForbidden, "private runtime does not permit task agent",
		)
	}
	useSkillRefs := requestHasClientCapability(r, protocol.DaemonCapabilitySkillBundlesV1)
	// A daemon older than the multica-platform merge assembles a brief that
	// still names the built-ins this server stopped shipping. It cannot be
	// fixed from here — the brief lives in the daemon binary — so the missing
	// capability buys that daemon a redirect stub under the old name instead of
	// a dangling pointer. Capability, not version: the version string is only
	// ever shown to humans.
	legacySkillRedirects := !requestHasClientCapability(r, protocol.DaemonCapabilityPlatformSkillV1)
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
	// Fold in the workspace MCP servers this agent has been explicitly
	// given (GH #6062). Only bound AND enabled servers are read, so a
	// workspace library entry nobody added reaches nothing. Read on every
	// claim, exactly like the agent column, so an admin's edit or a toggle
	// lands on the agent's next task with nothing to restart. Errors —
	// including a failed read — leave the agent config untouched: a broken
	// shared entry must never take away servers the agent runs with today.
	if bound, err := h.Queries.ListEnabledAgentMcpServers(r.Context(), agent.ID); err != nil {
		slog.Warn("daemon claim: load agent mcp servers failed; using agent mcp_config",
			"task_id", uuidToString(task.ID), "agent_id", uuidToString(agent.ID), "error", err)
	} else if len(bound) > 0 {
		bindings := make([]WorkspaceMcpBinding, 0, len(bound))
		for _, server := range bound {
			bindings = append(bindings, WorkspaceMcpBinding{Name: server.Name, Config: json.RawMessage(server.Config)})
		}
		if resolved, err := ResolveAgentMcpConfig(bindings, mcpConfig); err != nil {
			slog.Warn("daemon claim: resolve agent mcp servers failed; falling back to agent mcp_config",
				"task_id", uuidToString(task.ID), "agent_id", uuidToString(agent.ID), "error", err)
		} else {
			mcpConfig = resolved
		}
	}
	// Layer the per-task overlay (set at enqueue from the initiator
	// user's active integrations — currently Composio) on top of the
	// agent's saved mcp_config. Overlay wins on server-name collisions
	// because it carries the live user-scoped session URL. Errors are
	// logged but never fail the claim: a broken overlay must not prevent
	// the agent from running with its base config.
	if composioMCPEnabled && len(task.RuntimeMcpOverlay) > 0 {
		if merged, err := mergeMCPOverlay(mcpConfig, json.RawMessage(task.RuntimeMcpOverlay)); err != nil {
			slog.Warn("daemon claim: merge runtime_mcp_overlay failed; falling back to agent mcp_config", "task_id", uuidToString(task.ID), "error", err)
		} else {
			mcpConfig = merged
		}
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
		ID:                    uuidToString(agent.ID),
		Name:                  agent.Name,
		Instructions:          agent.Instructions,
		CustomEnv:             customEnv,
		CustomArgs:            customArgs,
		McpConfig:             mcpConfig,
		Model:                 agent.Model.String,
		ThinkingLevel:         agent.ThinkingLevel.String,
		ServiceTier:           agent.ServiceTier.String,
		RuntimeConfig:         runtimeConfig,
		DisabledRuntimeSkills: disabledRuntimeSkillsFor(agent.DisabledRuntimeSkills, runtimeID, runtime.Provider),
	}
	// System agents carry a product-owned instruction layer that ships with
	// this binary instead of being copied into their row at creation. That
	// is what makes it hot-updatable: editing the embedded file and
	// deploying reaches every existing workspace on its next task, with no
	// migration and no client upgrade. agent.Instructions holds only the
	// workspace's own notes, so a release can never overwrite them.
	//
	// Composing here covers every task kind, because this is the single
	// place a claimed task's agent payload is assembled.
	if agent.SystemKey.String == service.MikaSystemKey {
		resp.Agent.Instructions = service.ComposeMikaInstructions(agent.Name, agent.Instructions)
	}
	if useSkillRefs {
		_, skillRefs, err := h.TaskService.LoadAgentSkillBundles(r.Context(), task.AgentID, agent.SystemKey.String, legacySkillRedirects)
		if err != nil {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, h.rejectClaimSkillLoad(task, err)
		}
		agentSkillCount = len(skillRefs)
		resp.Agent.SkillRefs = skillRefs
	} else {
		skills, err := h.TaskService.LoadAgentSkills(r.Context(), task.AgentID)
		if err != nil {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, h.rejectClaimSkillLoad(task, err)
		}
		agentSkillCount = len(skills)
		builtinSkills := h.TaskService.BuiltinSkills(agent.SystemKey.String, legacySkillRedirects)
		builtinSkillCount = len(builtinSkills)
		skills = append(skills, builtinSkills...)
		resp.Agent.Skills = skills
	}
	if !claimResponseAgentIdentityMatches(resp) {
		responseAgentID := ""
		if resp.Agent != nil {
			responseAgentID = resp.Agent.ID
		}
		slog.Error("daemon claim: response agent identity mismatch; refusing dispatch",
			"task_id", uuidToString(task.ID), "task_agent_id", resp.AgentID, "response_agent_id", responseAgentID)
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, h.failClaimedTaskBeforeLaunch(
			r.Context(), task,
			"Task identity is invalid: the task and response agent disagree.",
			taskfailure.ReasonInvalidTaskIdentity,
			"error_invalid_task_identity", http.StatusConflict, "task response agent identity mismatch",
		)
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
	// Project context, repo precedence and the tenant/failure rules around both
	// live in resolveClaimProjectContext, which every claim path shares.
	if task.IssueID.Valid {
		issue, issueErr := h.Queries.GetIssue(r.Context(), task.IssueID)
		if issueErr != nil {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount,
				h.rejectClaimSourceLoad(r.Context(), task, issueErr, "issue", uuidToString(task.IssueID))
		}
		resp.WorkspaceID = uuidToString(issue.WorkspaceID)
		if failure := h.rejectClaimOnWorkspaceMismatch(r.Context(), task, resp.WorkspaceID, runtimeID, runtimeWorkspaceID, false); failure != nil {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, failure
		}
		resp.ThreadName = issue.Title
		issueNumber = issue.Number

		// Squad-leader briefing injection: keyed off the task being a
		// leader-task (is_leader_task) carrying a squad_id — NOT off the
		// issue being assigned to a squad. The task flag is stamped at
		// enqueue time and is true for every ISSUE-BOUND path that routes
		// work to a squad leader: direct assign-to-squad, comment
		// @squad-mention (even when the issue itself is assigned to a
		// plain agent — the MUL-3724 case), sub-issue done callback,
		// autopilot squad-assignee, and retry-clone inheritance. The old
		// issue.AssigneeType=="squad" gate missed the comment-mention
		// path, so the leader booted with zero squad context and
		// degraded into doing the work itself instead of orchestrating.
		//
		// NOTE: quick-create tasks do NOT reach this block — they have a
		// NULL issue_id (so the enclosing `task.IssueID.Valid` is false)
		// and do NOT carry is_leader_task / squad_id columns. They route
		// their squad through the task CONTEXT JSON (QuickCreateContext.
		// SquadID) and get their briefing from the separate quick-create
		// branch further below (search `qc.SquadID`). Do not "unify" the
		// two by deleting that branch: it also sets resp.SquadID /
		// resp.SquadName so the new issue defaults to the squad assignee,
		// and there is no issue row to hang this column-based path on.
		//
		// We resolve the squad directly from task.SquadID rather than
		// reverse-looking-up "which squad is this agent the leader of",
		// which is ambiguous when one agent leads multiple squads. The
		// uuidToString(squad.LeaderID) == resp.Agent.ID re-check is kept
		// as a defensive gate: if the squad's leader was swapped after the
		// task was enqueued, we never feed a stale briefing to a
		// non-leader. It also doubles as the dangling-squad_id guard: a
		// squad hard-deleted after enqueue makes GetSquadInWorkspace
		// return no row (err != nil) — we skip injection silently, which
		// is exactly the same observable result as "condition not
		// matched". Claim still succeeds; no stale briefing is emitted.
		// (No FK on squad_id — see migration 127.) We append (not replace)
		// so per-agent instructions stay authoritative; the squad briefing
		// stacks on top as task-specific squad context.
		if task.IsLeaderTask {
			injected := false
			if resp.Agent != nil && task.SquadID.Valid {
				if squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
					ID:          task.SquadID,
					WorkspaceID: issue.WorkspaceID,
				}); err == nil && uuidToString(squad.LeaderID) == resp.Agent.ID {
					// Parent-status authority is deliberately NARROWER than
					// briefing injection. Injection is keyed off is_leader_task
					// (see above) and therefore also fires on the MUL-3724 path,
					// where the issue belongs to a plain agent and this squad was
					// only @mentioned for help. Granting status ownership there
					// would let a guest squad push someone else's in-flight issue
					// to in_review, so we gate it on the issue actually being
					// assigned to this squad.
					ownsIssueStatus := issue.AssigneeType.Valid &&
						issue.AssigneeType.String == "squad" &&
						uuidToString(issue.AssigneeID) == uuidToString(squad.ID)
					briefing := buildSquadLeaderBriefing(r.Context(), h.Queries, squad, ownsIssueStatus)
					if strings.TrimSpace(resp.Agent.Instructions) == "" {
						resp.Agent.Instructions = briefing
					} else {
						resp.Agent.Instructions = resp.Agent.Instructions + "\n\n" + briefing
					}
					injected = true
					slog.Debug("injected squad leader briefing",
						"squad_id", uuidToString(squad.ID),
						"squad_name", squad.Name,
						"leader_agent_id", resp.Agent.ID,
						"owns_issue_status", ownsIssueStatus,
					)
				}
			}
			// Every skip above (NULL squad_id, squad hard-deleted, leader
			// swapped after enqueue) leaves a task the daemon must NOT run
			// as a leader: it has no roster to delegate to and no protocol
			// to follow. The daemon derives its leader role from this flag
			// (MUL-5811), so clearing it here is what keeps
			// "is_leader_task on the wire ⇔ briefing injected" true, and the
			// run degrades to an ordinary agent turn exactly as it did when
			// the daemon inferred the role from the briefing text itself.
			if !injected {
				resp.IsLeaderTask = false
				slog.Warn("squad leader briefing not injected; claim delivered as a non-leader task",
					"task_id", uuidToString(task.ID),
					"squad_id", uuidToString(task.SquadID),
					"agent_id", uuidToString(task.AgentID),
				)
			}
		}

		projectCtx, projectErr := h.resolveClaimProjectContext(r.Context(), issue.ProjectID, issue.WorkspaceID)
		if projectErr != nil {
			slog.Error("issue claim: load project context failed; preserving task for redelivery",
				"task_id", uuidToString(task.ID),
				"issue_id", uuidToString(issue.ID),
				"error", projectErr)
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
				outcome: "error_project_context",
				status:  http.StatusInternalServerError,
				message: "failed to load project context",
			}
		}
		projectCtx.applyTo(&resp)

		// Load every planned input as one chronological, de-duplicated set.
		// The trigger is included here so the delivery receipt can only contain
		// comments whose body we successfully embedded. Missing/deleted rows are
		// intentionally absent and remain eligible for reconciliation. A stable
		// payload budget always keeps the primary trigger, then admits an oldest-
		// first prefix of additional comments; overflow is reconciled later.
		// Workspace-scoped load (MUL-4252) so a foreign comment UUID resolves to
		// "missing" instead of leaking another tenant's text into the prompt.
		plannedCommentIDs := append([]pgtype.UUID{}, task.CoalescedCommentIds...)
		if task.TriggerCommentID.Valid {
			plannedCommentIDs = append(plannedCommentIDs, task.TriggerCommentID)
		}
		loadedComments := h.buildCoalescedCommentData(r.Context(), runtime.WorkspaceID, plannedCommentIDs)
		triggerCommentID := uuidToString(task.TriggerCommentID)
		var deliveredComments []CoalescedCommentData
		triggerLoaded := false
		for _, comment := range loadedComments {
			if comment.ID == triggerCommentID {
				triggerLoaded = true
				break
			}
		}
		if task.TriggerCommentID.Valid && triggerLoaded {
			deliveredComments = selectCommentDelivery(
				loadedComments,
				triggerCommentID,
				!supportsCoalescedComments,
				maxClaimCommentPayloadBytes,
			)
		}
		// If the persisted trigger body cannot be loaded, fail closed on comment
		// coverage for this claim. The trigger snapshot CAS below also rejects a
		// concurrent edit/delete that changes the FK after this read.
		deliveredCommentIDs = commentDataIDs(deliveredComments)
		// taskToResponse exposes the enqueue plan to UI task-list callers. A
		// daemon claim must instead advertise only the structured ids actually
		// present in this payload, especially when the delivery budget truncates.
		resp.CoalescedCommentIDs = nil
		for _, comment := range deliveredComments {
			if comment.ID == triggerCommentID {
				// Populate the actual payload from the same successful read that
				// earned the receipt. The richer GetComment lookup below resolves
				// initiator ids and count hints, but a transient second-read failure
				// must never acknowledge a body that was not embedded.
				resp.TriggerCommentContent = comment.Content
				resp.TriggerThreadID = comment.ThreadID
				resp.TriggerAuthorType = comment.AuthorType
				resp.TriggerAuthorName = comment.AuthorName
				continue
			}
			resp.CoalescedCommentIDs = append(resp.CoalescedCommentIDs, comment.ID)
			resp.CoalescedComments = append(resp.CoalescedComments, comment)
		}

		// Fetch the triggering comment content so the daemon can embed it
		// directly in the agent prompt (prevents the agent from ignoring comments
		// when stale output files exist in a reused workdir). Also surface the
		// comment author's kind and display name so the agent knows whether it
		// was triggered by a human or by another agent — a signal used by the
		// harness instructions to avoid mention loops between agents.
		effectiveTriggerUUID := task.TriggerCommentID
		if effectiveTriggerUUID.Valid {
			// Scope by the runtime's workspace so a task row carrying a foreign
			// comment UUID can never pull another workspace's comment text into
			// this agent's prompt. The task's issue workspace is asserted equal
			// to runtime.WorkspaceID below, so this is the right tenant (MUL-4252).
			if comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
				ID:          effectiveTriggerUUID,
				WorkspaceID: runtime.WorkspaceID,
			}); err == nil {
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
				//
				// NewCommentsDeltaKnown is set on the success path REGARDLESS of
				// the count, and is the only thing that distinguishes "the server
				// looked and there is nothing" from "the server could not look".
				// The count fields stay suppressed at zero — the daemon has no
				// hint to render from a zero — but the daemon must still be able
				// to tell a computed zero from a failed read, because only the
				// computed one may waive the workflow's comment scan (MUL-6984).
				if startedAt, err := h.Queries.GetLastTaskStartedAtForIssueAndAgent(r.Context(), db.GetLastTaskStartedAtForIssueAndAgentParams{
					AgentID: task.AgentID,
					IssueID: comment.IssueID,
				}); err == nil && startedAt.Valid {
					if cnt, err := h.Queries.CountNewCommentsSince(r.Context(), db.CountNewCommentsSinceParams{
						AnchorID:    effectiveTriggerUUID,
						IssueID:     comment.IssueID,
						WorkspaceID: comment.WorkspaceID,
						Since:       startedAt,
						AuthorID:    task.AgentID,
					}); err == nil {
						resp.NewCommentsDeltaKnown = true
						if cnt > 0 {
							resp.NewCommentCount = int(cnt)
							resp.NewCommentsSince = startedAt.Time.UTC().Format(time.RFC3339)
						}
					}
				}
			}
		}

		if !supportsCoalescedComments {
			// Legacy daemons ignore the structured coalesced fields. Fold every
			// successfully loaded comment into the one trigger field they already
			// understand, then hide the structured fields to avoid duplicate prompt
			// sections in intermediate daemons that understand them but do not yet
			// advertise the capability.
			if len(resp.CoalescedComments) > 0 || (resp.TriggerCommentContent == "" && len(deliveredComments) > 0) {
				resp.TriggerCommentContent = formatLegacyCommentBundle(deliveredComments)
			}
			resp.CoalescedCommentIDs = nil
			resp.CoalescedComments = nil
		} else if resp.TriggerCommentContent == "" && len(deliveredComments) > 0 {
			// A deleted newest trigger must not suppress the structured earlier
			// comments: buildCommentPrompt renders them inside its trigger-content
			// branch. The missing id itself is not acknowledged in the receipt.
			resp.TriggerCommentContent = "The newest triggering comment is no longer available. Address every earlier comment included below."
		}

		// Resolve the prior agent session / workdir to resume.
		if task.RerunOfTaskID.Valid {
			// Manual retry: resume precisely from the source task the user
			// clicked, NOT the most-recent (agent, issue) row — a parallel task
			// on the same issue must never hijack the resume (MUL-4869). The
			// workdir is ALWAYS reused when it still exists; the session is
			// resumed only when the source failure did not poison the
			// conversation AND the source ran on this runtime.
			//
			// Resume-safety is computed HERE from the source task, not read off
			// task.ForceFreshSession: RerunIssue pins that flag to true so an OLD
			// claim handler mid rolling-deploy degrades to a clean start instead
			// of resuming a different execution via the (agent, issue) lookup.
			// service.ResumeUnsafeFailure mirrors GetLastTaskSession, including
			// its 400/invalid_request_error text defense for legacy /
			// mis-classified rows that the exact-source path would otherwise miss.
			//
			// When the source workdir is gone (GC'd), absent on this runtime, or
			// was never recorded (failed too early), execenv.Reuse falls back to a
			// fresh Prepare and gateResumeToReusedWorkdir drops the now-unusable
			// session — reuse is best-effort, never a silent swap onto a stale
			// directory. PriorWorkDir is offered regardless of runtime (a shared
			// mount may still resolve it); only the per-cwd session is
			// runtime-gated.
			if src, err := h.Queries.GetAgentTask(r.Context(), task.RerunOfTaskID); err == nil && rerunSourceMatchesTaskScope(*task, src) {
				if src.WorkDir.Valid {
					resp.PriorWorkDir = src.WorkDir.String
				}
				if !service.ResumeUnsafeFailure(src.FailureReason.String, src.Error.String) &&
					src.SessionID.Valid && src.RuntimeID == task.RuntimeID {
					resp.PriorSessionID = src.SessionID.String
				}
				// MUL-5305: if the source task withheld its Codex session because
				// the rollout was missing, this rerun has nothing resumable from it
				// — disclose the gap rather than silently starting fresh.
				if src.SessionRolloutMissing {
					resp.PriorSessionResumeUnavailable = true
				}
			} else if err == nil {
				slog.Warn("daemon claim: rerun source belongs to another agent or scope; starting fresh",
					"task_id", uuidToString(task.ID),
					"task_agent_id", uuidToString(task.AgentID),
					"task_issue_id", uuidToString(task.IssueID),
					"source_task_id", uuidToString(src.ID),
					"source_agent_id", uuidToString(src.AgentID),
					"source_issue_id", uuidToString(src.IssueID),
				)
				resp.PriorSessionResumeUnavailable = true
			}
		} else if !task.ForceFreshSession {
			// Non-rerun follow-up on the same issue: resume the most recent
			// (agent, issue) session so the agent keeps the issue's conversation
			// context across turns. The "Focus on THIS comment" guard in
			// prompt.go defends against inheriting the prior turn's "Done."
			// marker, and GetLastTaskSession already excludes poisoned sessions.
			if prior, err := h.Queries.GetLastTaskSession(r.Context(), db.GetLastTaskSessionParams{
				AgentID: task.AgentID,
				IssueID: task.IssueID,
			}); err == nil && prior.SessionID.Valid {
				if prior.RuntimeID == task.RuntimeID {
					resp.PriorSessionID = prior.SessionID.String
				}
				if prior.WorkDir.Valid {
					resp.PriorWorkDir = prior.WorkDir.String
				}
			}
			// MUL-5305: if the most recent terminal task withheld its Codex
			// session because the rollout was missing, GetLastTaskSession fell
			// back to an older session (or none). Disclose the continuity gap so
			// the next run tells the user the most recent turn's context could not
			// be carried over — even when that older session resumes cleanly,
			// which the resume-presence gate would otherwise pass silently.
			if missing, err := h.Queries.GetLatestTaskRolloutMissing(r.Context(), db.GetLatestTaskRolloutMissingParams{
				AgentID: task.AgentID,
				IssueID: task.IssueID,
			}); err == nil && missing {
				resp.PriorSessionResumeUnavailable = true
			}
		}
	}

	// Chat task: populate workspace/session info from the chat_session table.
	if task.ChatSessionID.Valid {
		cs, chatErr := h.Queries.GetChatSession(r.Context(), task.ChatSessionID)
		if chatErr != nil {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount,
				h.rejectClaimSourceLoad(r.Context(), task, chatErr, "chat session", uuidToString(task.ChatSessionID))
		}
		resp.WorkspaceID = uuidToString(cs.WorkspaceID)
		if failure := h.rejectClaimOnWorkspaceMismatch(r.Context(), task, resp.WorkspaceID, runtimeID, runtimeWorkspaceID, false); failure != nil {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, failure
		}
		resp.ChatSessionID = uuidToString(cs.ID)
		resp.ThreadName = cs.Title
		// Legacy compatibility: agent creation no longer creates intro chats,
		// but historical is_agent_intro sessions can still be resumed. Such a
		// session carries no user message on its opening turn, so flag it for
		// the historical self-introduction prompt (MUL-4230). The column stays true for the
		// session's whole life, so gate the intro prompt on the session still
		// having zero human messages — otherwise every follow-up turn after the
		// creator replies would re-run the "introduce yourself" prompt and the
		// agent keeps repeating the same introduction (MUL-4259).
		if cs.IsAgentIntro {
			if hasUser, herr := h.Queries.ChatSessionHasUserMessage(r.Context(), cs.ID); herr != nil {
				slog.Warn("chat intro gate: has-user-message check failed",
					"chat_session_id", uuidToString(cs.ID), "error", herr)
			} else {
				resp.ChatIntro = !hasUser
			}
		}
		// A task-level delivery snapshot, not the Chat's historical binding,
		// decides whether this run is operating for an external audience.
		// Web/Desktop/Mobile turns in an old channel-originated Chat have no
		// snapshot and remain private to Multica after /new rotates the route.
		delivery, deliveryErr := h.Queries.GetChannelTaskDelivery(r.Context(), task.ID)
		if deliveryErr == nil {
			resp.ChatChannelType = delivery.ChannelType
			resp.ChatType = delivery.ChatType
			resp.ChatChannelDeliversFiles = h.channelDeliversFiles(delivery.ChannelType)
			if delivery.ChannelType == string(slack.TypeSlack) {
				resp.ChatInThread = delivery.ChannelThreadID.Valid &&
					delivery.ChannelThreadID.String != "" &&
					delivery.ChannelThreadID.String != delivery.ChannelMessageID.String
			}
		} else if !errors.Is(deliveryErr, pgx.ErrNoRows) {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount,
				h.rejectClaimSourceLoad(r.Context(), task, deliveryErr, "channel task delivery", uuidToString(task.ID))
		}
		// A web chat can opt into the same durable project context as an
		// issue-bound task.
		projectCtx, projectErr := h.resolveClaimProjectContext(r.Context(), cs.ProjectID, cs.WorkspaceID)
		if projectErr != nil {
			slog.Error("chat claim: load project context failed; preserving task for redelivery",
				"task_id", uuidToString(task.ID),
				"chat_session_id", uuidToString(cs.ID),
				"error", projectErr)
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
				outcome: "error_project_context",
				status:  http.StatusInternalServerError,
				message: "failed to load project context",
			}
		}
		projectCtx.applyTo(&resp)
		if !task.ForceFreshSession && !task.ChannelContextRevision.Valid {
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
		}
		// Resolve the user-message input batch for this run. A task-owned
		// task (chat_input_task_id set) reads exactly the user messages
		// tagged with its own input owner, so a message that arrived after
		// this turn was sealed can never be absorbed here. Direct-chat
		// tasks have owned their single message since MUL-4351; channel
		// (Slack/Lark) tasks now seal their trailing batch at enqueue too.
		// Only legacy tasks created before that deploy carry a NULL owner
		// and keep the trailing-message selector — the run of user messages
		// after the last assistant row, which also covers a debounced burst
		// (MUL-2968: "看上海天气" then "还有青岛" must both be delivered) —
		// so a rolling deploy never replays their history. Attachments are
		// collected per included message so the agent can
		// `multica attachment download <id>` (the inline markdown URL is
		// signed + 30-min expiring on the CDN).
		var unanswered []db.ChatMessage
		var inputLoadErr error
		if task.ChatInputTaskID.Valid {
			unanswered, inputLoadErr = h.Queries.ListChatInputMessages(r.Context(), task.ChatInputTaskID)
		} else if msgs, err := h.Queries.ListChatMessagesForLegacyTask(r.Context(), cs.ID); err == nil {
			unanswered = trailingUserMessages(msgs)
		} else {
			inputLoadErr = err
		}
		// A read failure must NOT masquerade as "zero input". Preserve the
		// just-dispatched task (the stale-dispatched reclaim redelivers it)
		// and reject the claim with 5xx, rather than cancelling a valid direct
		// task on a transient DB error (MUL-4351 review).
		if inputLoadErr != nil {
			slog.Error("chat claim: load chat input messages failed; preserving task for redelivery",
				"task_id", uuidToString(task.ID),
				"chat_session_id", uuidToString(cs.ID),
				"error", inputLoadErr)
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
				outcome: "error_chat_input_load",
				status:  http.StatusInternalServerError,
				message: "failed to load chat input",
			}
		}

		// Input ownership is the claim's fail-closed boundary. Resume-history
		// reads belong after it: a task that cannot load its input is preserved
		// for redelivery and must not spend two more queries before returning.
		if !task.ForceFreshSession {
			contextRevision := pgtype.Int8{}
			if task.ChannelContextRevision.Valid {
				contextRevision = task.ChannelContextRevision
			}
			// GetLastChatTaskSession currently has exactly two consumers: the
			// prior session and prior workdir fields. Keep this guard coupled to
			// both so adding a third consumer cannot silently skip its fallback.
			// Context-scoped channel tasks always resolve both fields from task
			// rows in their own generation; the Chat-wide pointer may belong to a
			// different generation when independent debounce timers race.
			if task.ChannelContextRevision.Valid || chatSessionResumeFallbackNeeded(resp.PriorSessionID, resp.PriorWorkDir) {
				h.Metrics.RecordChatClaimSessionFallbackNeeded()
				started := time.Now()
				prior, err := h.Queries.GetLastChatTaskSession(r.Context(), db.GetLastChatTaskSessionParams{
					ChatSessionID:          cs.ID,
					ChannelContextRevision: contextRevision,
				})
				h.Metrics.ObserveChatClaimLastSessionQuery(time.Since(started).Seconds())
				switch {
				case err == nil && prior.SessionID.Valid:
					h.Metrics.RecordChatClaimSessionFallbackHit()
					if resp.PriorSessionID == "" && prior.RuntimeID == task.RuntimeID {
						resp.PriorSessionID = prior.SessionID.String
					}
					if prior.WorkDir.Valid && resp.PriorWorkDir == "" {
						resp.PriorWorkDir = prior.WorkDir.String
					}
				case errors.Is(err, pgx.ErrNoRows):
					h.Metrics.RecordChatClaimSessionFallbackMiss()
				case err == nil:
					// Defensive only: the SQL excludes NULL session ids, but
					// preserve miss semantics if that contract ever changes.
					h.Metrics.RecordChatClaimSessionFallbackMiss()
				default:
					h.Metrics.RecordChatClaimSessionFallbackError()
				}
			}

			// MUL-5305: continuity-gap disclosure is independent of whether
			// either pointer field needed fallback, so this query stays
			// unconditional for non-force-fresh chat claims.
			started := time.Now()
			missing, err := h.Queries.GetLatestChatTaskRolloutMissing(r.Context(), db.GetLatestChatTaskRolloutMissingParams{
				ChatSessionID:          cs.ID,
				ChannelContextRevision: contextRevision,
			})
			h.Metrics.ObserveChatClaimRolloutMissingQuery(time.Since(started).Seconds())
			if err == nil && missing {
				resp.PriorSessionResumeUnavailable = true
			}
		}

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

		// Fail closed: a task-owned direct task that resolves to no user text
		// (and is not the agent's proactive intro) must never dispatch an
		// empty prompt. The send path creates the owning user message in the
		// same transaction as the task, so this only fires on genuinely
		// corrupt state — cancel the just-dispatched task and reject the claim
		// rather than run the agent with nothing to answer (MUL-4351).
		if task.ChatInputTaskID.Valid && !resp.ChatIntro && strings.TrimSpace(resp.ChatMessage) == "" {
			slog.Error("chat claim: task-owned direct task has no user input; cancelling",
				"task_id", uuidToString(task.ID),
				"chat_session_id", uuidToString(cs.ID),
				"chat_input_task_id", uuidToString(task.ChatInputTaskID),
			)
			if _, cerr := h.TaskService.CancelTask(r.Context(), task.ID); cerr != nil {
				slog.Error("chat claim: cancel after empty input failed",
					"task_id", uuidToString(task.ID), "error", cerr)
			}
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
				outcome: "error_empty_chat_input",
				status:  http.StatusInternalServerError,
				message: "chat task has no user input",
			}
		}

		if strings.TrimSpace(resp.ThreadName) == "" && resp.ChatMessage != "" {
			resp.ThreadName = resp.ChatMessage
		}
	}

	// Autopilot run_only task: resolve workspace from autopilot_run →
	// autopilot, and include the autopilot instructions because there is no
	// issue for the agent to fetch.
	if task.AutopilotRunID.Valid {
		run, runErr := h.Queries.GetAutopilotRun(r.Context(), task.AutopilotRunID)
		if runErr != nil {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount,
				h.rejectClaimSourceLoad(r.Context(), task, runErr, "autopilot run", uuidToString(task.AutopilotRunID))
		}
		ap, apErr := h.Queries.GetAutopilot(r.Context(), run.AutopilotID)
		if apErr != nil {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount,
				h.rejectClaimSourceLoad(r.Context(), task, apErr, "autopilot", uuidToString(run.AutopilotID))
		}

		// The autopilot owns this task's workspace. taskToResponse seeded
		// resp.WorkspaceID with the RUNTIME's workspace, so the old
		// `if resp.WorkspaceID == ""` guard never fired and left the isolation
		// check comparing the runtime workspace against itself. Assign
		// authoritatively, then prove ownership before exposing any
		// autopilot-owned context.
		resp.WorkspaceID = uuidToString(ap.WorkspaceID)
		if failure := h.rejectClaimOnWorkspaceMismatch(r.Context(), task, resp.WorkspaceID, runtimeID, runtimeWorkspaceID, false); failure != nil {
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, failure
		}

		resp.AutopilotID = uuidToString(run.AutopilotID)
		resp.AutopilotSource = run.Source
		if run.TriggerPayload != nil {
			resp.AutopilotTriggerPayload = json.RawMessage(run.TriggerPayload)
		}
		resp.AutopilotTitle = ap.Title
		resp.ThreadName = ap.Title
		if ap.Description.Valid {
			resp.AutopilotDescription = ap.Description.String
		}

		// A run_only autopilot has no issue from which to inherit project
		// context, so its configured project is hydrated here. That gives the
		// daemon the same resource contract as issue-bound and quick-create
		// tasks: project repositories scope the checkout, while local_directory
		// lets the daemon select the bound path and write its managed manifest.
		projectCtx, projectErr := h.resolveClaimProjectContext(r.Context(), ap.ProjectID, ap.WorkspaceID)
		if projectErr != nil {
			slog.Error("autopilot claim: load project context failed; preserving task for redelivery",
				"task_id", uuidToString(task.ID),
				"autopilot_id", uuidToString(run.AutopilotID),
				"error", projectErr)
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
				outcome: "error_project_context",
				status:  http.StatusInternalServerError,
				message: "failed to load project context",
			}
		}
		projectCtx.applyTo(&resp)
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
			resp.QuickCreatePriority = qc.Priority
			resp.QuickCreateDueDate = qc.DueDate
			resp.QuickCreateAttachmentIDs = append([]string(nil), qc.AttachmentIDs...)
			resp.ThreadName = qc.Prompt
			resp.WorkspaceID = qc.WorkspaceID
			if failure := h.rejectClaimOnWorkspaceMismatch(r.Context(), task, resp.WorkspaceID, runtimeID, runtimeWorkspaceID, true); failure != nil {
				return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, failure
			}
			if qc.SourceContextID != "" {
				workspaceID, workspaceErr := util.ParseUUID(qc.WorkspaceID)
				contextID, contextIDErr := util.ParseUUID(qc.SourceContextID)
				if workspaceErr != nil || contextIDErr != nil {
					return resp, nil, 0, 0, h.failClaimedTaskBeforeLaunch(
						r.Context(), task,
						"Captured source context is invalid. Start again from the branch point.",
						taskfailure.ReasonAgentUnknown,
						"error_source_context_invalid", http.StatusConflict, "captured source context is invalid",
					)
				}
				captured, contextErr := h.Queries.GetIssueSourceContextByID(r.Context(), db.GetIssueSourceContextByIDParams{WorkspaceID: workspaceID, ID: contextID})
				if contextErr != nil && !errors.Is(contextErr, pgx.ErrNoRows) {
					if _, requeueErr := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), *task); requeueErr != nil {
						slog.Error("quick-create claim: requeue after source context lookup failed", "task_id", uuidToString(task.ID), "error", requeueErr)
					}
					return resp, nil, 0, 0, &claimBuildFailure{
						outcome: "error_source_context_load", status: http.StatusInternalServerError, message: "failed to load captured source context",
					}
				}
				if contextErr != nil || captured.State != "pending" || captured.OriginTaskID != task.ID {
					return resp, nil, 0, 0, h.failClaimedTaskBeforeLaunch(
						r.Context(), task,
						"Captured source context is no longer available. Start again from the branch point.",
						taskfailure.ReasonAgentUnknown,
						"error_source_context_unavailable", http.StatusConflict, "captured source context is unavailable",
					)
				}
				resp.QuickCreateSourceContext = append(json.RawMessage(nil), captured.Snapshot...)
			}
			// When the user picked a project in the modal, surface its title
			// and resources to the daemon so the agent has the same context
			// it would for an issue-bound task. A prompt-supplied project id
			// that does not parse is treated as no project at all.
			var quickCreateProjectID pgtype.UUID
			if qc.ProjectID != "" {
				if parsed, err := util.ParseUUID(qc.ProjectID); err == nil {
					quickCreateProjectID = parsed
				}
			}
			projectCtx, projectErr := h.resolveClaimProjectContext(r.Context(), quickCreateProjectID, parseUUID(qc.WorkspaceID))
			if projectErr != nil {
				slog.Error("quick-create claim: load project context failed; preserving task for redelivery",
					"task_id", uuidToString(task.ID),
					"project_id", qc.ProjectID,
					"error", projectErr)
				return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
					outcome: "error_project_context",
					status:  http.StatusInternalServerError,
					message: "failed to load project context",
				}
			}
			projectCtx.applyTo(&resp)

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
				parentExists := true
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
						} else if qc.SourceContextID != "" && errors.Is(perr, pgx.ErrNoRows) {
							// A contextual quick-create already owns an immutable
							// snapshot. If its source was deleted before this first
							// issue exists, create the eventual target as top-level —
							// the same state application-layer detach would have
							// produced had deletion happened just after creation.
							// Ordinary Add sub-issue keeps its historical fail-loud
							// behavior when its parent disappears.
							parentExists = false
						}
					}
				}
				if parentExists {
					resp.ParentIssueID = qc.ParentIssueID
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
						// Quick-create has no issue yet — there is no parent
						// status to own on this turn. Once the leader opens the
						// issue with the squad as assignee, the issue-bound
						// claim path above grants ownership.
						briefing := buildSquadLeaderBriefing(r.Context(), h.Queries, squad, false)
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

	// Catch-all workspace isolation check. Every context branch above already
	// ran this the moment it resolved its owning workspace, which is what makes
	// "prove ownership, then hydrate" structural rather than per-branch
	// discipline. This final call is the backstop for a task that reached here
	// without any branch claiming it.
	if failure := h.rejectClaimOnWorkspaceMismatch(r.Context(), task, resp.WorkspaceID, runtimeID, runtimeWorkspaceID, hasQuickCreate); failure != nil {
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, failure
	}

	// Workspace-level Context (workspace.context DB column) — the per-workspace
	// system prompt that workspace owners set in Settings → General. Inject it
	// into the brief regardless of task kind (issue / chat / autopilot /
	// quick-create) so every agent running in the workspace sees the same
	// shared context. Empty string when the owner hasn't set one; the daemon
	// skips rendering the heading in that case.
	if ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(resp.WorkspaceID)); err == nil {
		resp.WorkspaceSlug = ws.Slug
		if issueNumber > 0 {
			resp.IssueIdentifier = service.IssueIdentifier(ws.IssuePrefix, issueNumber)
		}
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

	// Workspace status catalog (MUL-6460): active CUSTOM statuses only, so the
	// daemon can render them into the brief's status-command line. Read on every
	// claim, like the agent row, so an admin's edit lands on the next task.
	// Failure degrades to the built-in-only brief rather than failing the claim.
	if entries, err := h.Queries.ListIssueStatusEntries(r.Context(), db.ListIssueStatusEntriesParams{
		WorkspaceID:     parseUUID(resp.WorkspaceID),
		IncludeArchived: false,
	}); err != nil {
		slog.Warn("task claim: failed to load issue status catalog for brief injection",
			"task_id", uuidToString(task.ID),
			"workspace_id", resp.WorkspaceID,
			"error", err,
		)
	} else {
		for _, entry := range entries {
			if entry.IsSystem {
				continue
			}
			if len(resp.IssueStatuses) == taskIssueStatusCap {
				resp.IssueStatusesOmitted++
				continue
			}
			resp.IssueStatuses = append(resp.IssueStatuses, TaskIssueStatusData{
				Key:         entry.Key,
				Name:        entry.Name,
				Category:    entry.Category,
				Description: entry.Description,
			})
		}
	}

	// Last gate before dispatch: refuse to hand a worktree-mode local_directory
	// task to a daemon that cannot implement the mode.
	//
	// The save-time gate in project_resource.go cannot cover this. It checks the
	// version at the moment the resource is written; a machine downgraded
	// afterwards still claims tasks, and an old daemon json-skips execution_mode
	// entirely — it would run the task IN PLACE, editing the working copy the
	// user asked to isolate. That is the exact outcome worktree mode exists to
	// prevent, so it fails closed here, against the version of the runtime that
	// is actually claiming.
	if reason := worktreeClaimBlockReason(
		resp.ProjectResources,
		runtime,
		requestHasClientCapability(r, protocol.DaemonCapabilityLocalWorktreeV1),
	); reason != "" {
		slog.Error("task claim: runtime too old for worktree mode; cancelling rather than running in place",
			"task_id", uuidToString(task.ID),
			"runtime_id", runtimeID,
			"daemon_id", runtime.DaemonID.String,
			"reason", reason,
		)
		// Cancel rather than leave it dispatched: the resource is pinned to this
		// daemon, so redelivery would hand it straight back to the same too-old
		// runtime forever.
		//
		// Cancel WITH the reason persisted on the row. The 4xx below only
		// reaches the daemon's log, and the batch-claim path drops the task from
		// its response entirely — so without this the user is left with a task
		// that says "cancelled" and nothing else, for a condition only they can
		// fix (update the app on that machine).
		//
		// Through the TaskService, not a raw query: cancellation has side
		// effects beyond the row — audit capture, chat settle, agent status
		// reconcile, the task:cancelled broadcast that clears live cards, and
		// NotifyTaskFinished waking capacity/serial waiters. A direct query
		// leaves all of those stale.
		if _, cerr := h.TaskService.CancelTaskWithReason(r.Context(), task.ID, reason, "local_directory_error"); cerr != nil {
			// The cancel did not commit, so the row is still claimed. The
			// daemon is about to be refused, so left alone the task would
			// strand in dispatched until stale reclaim, with no visible
			// reason. Requeue it instead — the next claim re-runs this gate
			// and retries the cancel — and report a 5xx so the daemon reads
			// this as a transient server problem, not a claim refusal.
			slog.Error("task claim: cancel after worktree version gate failed; requeueing so the gate can run again",
				"task_id", uuidToString(task.ID), "error", cerr)
			if _, rerr := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), *task); rerr != nil {
				slog.Error("task claim: requeue after worktree-gate cancel failure failed; stale reclaim will recover it",
					"task_id", uuidToString(task.ID), "error", rerr)
			}
			return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
				outcome: "error_worktree_gate_cancel",
				status:  http.StatusInternalServerError,
				message: "failed to cancel a worktree task blocked by daemon version; task requeued",
			}
		}
		return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, &claimBuildFailure{
			outcome: "error_worktree_daemon_version",
			status:  http.StatusUnprocessableEntity,
			message: reason,
		}
	}

	return resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, nil
}

// worktreeClaimBlockReason returns a user-facing reason when this runtime must
// not run the task, or "" when it may proceed.
//
// The decision is made on the CAPABILITY the daemon advertised on this very
// request, not on its version string. A daemon without the implementation does
// not merely lose concurrency — it json-skips execution_mode and runs the task
// in place, editing the working copy the user asked to isolate. Version strings
// could not answer that reliably: the git-describe dev-build exemption that
// keeps `make daemon` unblocked let a v0.4.23-era daemon straight through, and
// two tasks silently ran in the user's own directory (MUL-5707).
//
// Only resources bound to the claiming runtime's own daemon are considered: a
// project may carry one local_directory per machine, and another machine's
// worktree resource says nothing about this one's ability to run the task.
func worktreeClaimBlockReason(resources []ProjectResourceData, runtime db.AgentRuntime, hasWorktreeCapability bool) string {
	if !runtime.DaemonID.Valid || runtime.DaemonID.String == "" {
		return ""
	}
	if hasWorktreeCapability {
		return ""
	}
	for _, res := range resources {
		if res.ResourceType != "local_directory" {
			continue
		}
		var ref localDirectoryRef
		if err := json.Unmarshal(res.ResourceRef, &ref); err != nil {
			continue
		}
		if ref.ExecutionMode != localDirectoryModeWorktree || ref.DaemonID != runtime.DaemonID.String {
			continue
		}
		return fmt.Sprintf(
			"This machine's Multica runtime does not support parallel (worktree) mode, which %q is set to use. "+
				"Update the Multica app on that machine to the latest version, then re-run this task. "+
				"Refusing to run rather than falling back to editing the directory directly, which is what this mode exists to prevent.",
			ref.LocalPath)
	}
	return ""
}

// ClaimTaskByRuntime atomically claims the next queued task for a runtime.
// The response includes the agent's name and skills, fetched fresh from the DB.
func (h *Handler) ClaimTaskByRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	start := time.Now()

	var (
		outcome                  = "unauth"
		authMs, claimMs, buildMs int64
		payloadBytes             int
		agentSkillCount          int
		builtinSkillCount        int
		skillPayloadBytes        int
		buildStart               time.Time
	)
	defer func() {
		// Emit at function exit so error / unauth paths also carry timing.
		// build_ms is computed from buildStart only when we entered the
		// response-build phase (otherwise stays 0).
		if !buildStart.IsZero() {
			buildMs = time.Since(buildStart).Milliseconds()
		}
		logClaimEndpointSlow(runtimeID, outcome, start, authMs, claimMs, buildMs, payloadBytes, agentSkillCount, builtinSkillCount, skillPayloadBytes)
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
		payloadBytes, _ = writeMeasuredJSON(w, http.StatusOK, map[string]any{"task": nil})
		outcome = "no_task"
		return
	}
	if !task.TriggerCommentID.Valid && len(task.CoalescedCommentIds) > 0 {
		handled, failure := h.repairStaleCommentPlanIfNeeded(r.Context(), task, runtimeWorkspaceID)
		if handled {
			if failure != nil {
				outcome = failure.outcome
				writeError(w, failure.status, failure.message)
				return
			}
			outcome = "repaired_stale_comment_plan"
			payloadBytes, _ = writeMeasuredJSON(w, http.StatusOK, map[string]any{"task": nil})
			return
		}
	}

	outcome = "claimed"
	buildStart = time.Now()

	resp, deliveredCommentIDs, agentSkillCount, builtinSkillCount, failure := h.buildClaimedTaskResponse(r, task, runtime, runtimeID, runtimeWorkspaceID)
	if failure != nil {
		outcome = failure.outcome
		writeError(w, failure.status, failure.message)
		return
	}
	commentBackedTask := task.TriggerCommentID.Valid || len(task.CoalescedCommentIds) > 0
	requeueFailedClaim := func(reason string) {
		if _, err := h.TaskService.RequeueTaskAfterClaimFailure(r.Context(), *task); err != nil {
			slog.Error("task claim: failed to requeue after finalization error",
				"task_id", uuidToString(task.ID),
				"reason", reason,
				"error", err,
			)
		}
	}
	// Mint a task-scoped `mat_` token bound to (agent, task, workspace,
	// owner). The daemon will inject this as MULTICA_TOKEN into the agent
	// process instead of its own credential, so any API call the agent
	// makes — even one that strips X-Agent-ID / X-Task-ID headers — is
	// recognized server-side as actor=agent, closing the lateral-movement
	// path on human-only endpoints (e.g. `/api/agents/{id}/env`). Runtime
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
		requeueFailedClaim("token_generation")
		writeError(w, http.StatusInternalServerError, "failed to mint task token")
		return
	}
	remoteMCPToken, daemonTokens, derr := remoteMCPDaemonTokenForClaim(resp, runtime)
	if derr != nil {
		outcome = "error_remote_mcp_token"
		slog.Error("task claim: failed to generate Remote MCP daemon token",
			"task_id", uuidToString(task.ID), "error", derr)
		requeueFailedClaim("remote_mcp_token_generation")
		writeError(w, http.StatusInternalServerError, "failed to mint Remote MCP daemon token")
		return
	}
	receipt, ferr := h.TaskService.FinalizeTaskClaim(r.Context(), *task, db.CreateTaskTokenParams{
		ID:          dbid.NewV7(),
		TokenHash:   auth.HashToken(tokenStr),
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		WorkspaceID: parseUUID(resp.WorkspaceID),
		UserID:      runtime.OwnerID,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}, deliveredCommentIDs, commentBackedTask, daemonTokens...)
	if ferr != nil {
		outcome = "error_claim_finalize"
		slog.Error("task claim: failed to finalize token and comment delivery receipt",
			"task_id", uuidToString(task.ID), "error", ferr)
		// FinalizeTaskClaim is transactional, so its newly generated token is
		// rolled back with the receipt. Never delete by task here: a stale
		// reclaim can race the original daemon starting, and broad revocation
		// would invalidate that already-authorized execution.
		requeueFailedClaim("token_and_delivery_receipt")
		writeError(w, http.StatusInternalServerError, "failed to finalize task claim")
		return
	}
	resp.AuthToken = tokenStr
	resp.RemoteMCPDaemonToken = remoteMCPToken
	task.DeliveredCommentIds = receipt
	resp.DeliveredCommentIDs = uuidStringsOrEmpty(receipt)

	slog.Info("task claimed by runtime", "task_id", uuidToString(task.ID), "runtime_id", runtimeID, "agent_id", uuidToString(task.AgentID), "prior_session", resp.PriorSessionID)
	if resp.Agent != nil && len(resp.Agent.Skills) > 0 {
		if skillPayload, err := json.Marshal(resp.Agent.Skills); err == nil {
			skillPayloadBytes = len(skillPayload)
		}
	} else if resp.Agent != nil && len(resp.Agent.SkillRefs) > 0 {
		if skillPayload, err := json.Marshal(resp.Agent.SkillRefs); err == nil {
			skillPayloadBytes = len(skillPayload)
		}
	}
	payloadBytes, _ = writeMeasuredJSON(w, http.StatusOK, map[string]any{"task": resp})
}

type resolveSkillBundlesRequest struct {
	Skills []resolveSkillBundleRef `json:"skills"`
}

type resolveSkillBundleRef struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Hash   string `json:"hash"`
}

// ResolveTaskSkillBundles returns full skill content for refs from a slim
// claim. The daemon calls this after claim and before execenv.Prepare so
// runtimes still see complete local skill files at startup.
//
// If a requested hash no longer matches the agent's current skill bundle, the
// endpoint returns the current bundle and hash. Stage 1 does not snapshot skill
// content at claim time; the daemon validates the returned bundle before
// writing it to cache and materializing it.
func (h *Handler) ResolveTaskSkillBundles(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	taskID := chi.URLParam(r, "taskId")

	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	task, taskWorkspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}
	if taskWorkspaceID != uuidToString(runtime.WorkspaceID) || uuidToString(task.RuntimeID) != runtimeID {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != "dispatched" && task.Status != "waiting_local_directory" {
		writeError(w, http.StatusConflict, "task is not preparing")
		return
	}

	var req resolveSkillBundlesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Skills) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"bundles": []service.AgentSkillData{}})
		return
	}

	// Validate before reading: a malformed ref is rejected the same way it
	// always was, now without paying for a load first.
	wanted := make([]service.AgentSkillBundleRef, 0, len(req.Skills))
	for _, ref := range req.Skills {
		if ref.ID == "" || ref.Source == "" || ref.Hash == "" {
			writeError(w, http.StatusBadRequest, "invalid skill ref")
			return
		}
		wanted = append(wanted, service.AgentSkillBundleRef{ID: ref.ID, Source: ref.Source})
	}

	// Load ONLY what was asked for. The daemon resolves one skill per request,
	// so serving these out of the agent's full bundle set meant reading and
	// hashing every skill the agent has, once per request, to return one of
	// them — quadratic in skill count across a cold dispatch.
	allowed, err := h.TaskService.LoadRequestedAgentSkillBundles(r.Context(), task.AgentID, wanted)
	if err != nil {
		// 5xx, not a partial answer: the daemon's resolve retry can recover a
		// transient read, and a bundle assembled from a failed read would pass
		// its client-side validation and be cached as if it were complete.
		slog.Error("resolve skill bundles: load agent skills failed",
			"task_id", uuidToString(task.ID), "agent_id", uuidToString(task.AgentID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load skill bundles")
		return
	}

	resolved := make([]service.AgentSkillData, 0, len(req.Skills))
	for _, ref := range req.Skills {
		bundle, ok := allowed[service.AgentSkillBundleKey(ref.Source, ref.ID)]
		if !ok {
			writeError(w, http.StatusNotFound, "skill bundle not found")
			return
		}
		if ref.Source == skillbundle.SourcePlugin && bundle.Hash != ref.Hash {
			writeError(w, http.StatusConflict, "pinned plugin skill bundle hash mismatch")
			return
		}
		resolved = append(resolved, bundle)
	}

	writeJSON(w, http.StatusOK, map[string]any{"bundles": resolved})
}

// trailingUserMessages returns the run of user messages after the last
// assistant message in a chronologically-ordered chat history — the set the
// agent has NOT yet replied to. The agent resumes its prior session and only
// learns of new input through the claim response's chat_message, so a single
// run that covers a debounced burst (MUL-2968) must deliver every one of
// these, not just the latest. Every completed or failed run writes an
// assistant row, so the anchor advances one turn at a time; the result is the
// whole slice on the first turn and exactly the new message(s) thereafter.
// Legacy channel tasks also stop at the first unexpired media marker so an old
// run cannot consume a placeholder that has not been bound yet. New channel
// tasks use task-owned input batches and do not depend on this fallback.
func trailingUserMessages(msgs []db.ChatMessage) []db.ChatMessage {
	start := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			start = i + 1
			break
		}
	}
	msgs = msgs[start:]
	now := time.Now()
	for i := range msgs {
		pending := msgs[i].ChannelMediaPendingUntil
		if pending.Valid && pending.Time.After(now) {
			return msgs[:i]
		}
	}
	return msgs
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

// ExtendTaskPrepareLease keeps a dispatched task protected while the daemon is
// resolving startup inputs and preparing the execution environment.
func (h *Handler) ExtendTaskPrepareLease(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	taskID := chi.URLParam(r, "taskId")

	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	task, taskWorkspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}
	if taskWorkspaceID != uuidToString(runtime.WorkspaceID) || uuidToString(task.RuntimeID) != runtimeID {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	updated, err := h.TaskService.ExtendTaskPrepareLease(r.Context(), parseUUID(taskID), parseUUID(runtimeID))
	if err != nil {
		slog.Warn("extend task prepare lease failed", "task_id", taskID, "runtime_id", runtimeID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(*updated, taskWorkspaceID))
}

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
	// DurableWorkDir is the configured project directory that replaces a
	// disposable task worktree after the daemon confirms the worktree is gone.
	DurableWorkDir string `json:"durable_work_dir,omitempty"`
	// BranchName is the branch this run delivered its work on. Worktree-mode
	// local_directory tasks never touch the user's working copy, so this is the
	// only pointer to where the changes went. Empty for every other task kind.
	BranchName string `json:"branch_name,omitempty"`
	// SessionRolloutMissing: the daemon withheld this task's Codex session
	// because its rollout was missing (MUL-5305). Clear the resume pointer and
	// flag the continuity gap for the next claim.
	SessionRolloutMissing bool `json:"session_rollout_missing,omitempty"`
	// RetiredSessionID: a session this run proved unresumable and abandoned
	// (GH #6066). Distinct from an empty SessionID, which only means "nothing
	// to report" — this says "never hand this id to a later run". Older
	// daemons omit it, which is exactly the pre-fix behaviour.
	RetiredSessionID string `json:"retired_session_id,omitempty"`
}

// sanitizeTaskCompleteRequest / sanitizeTaskFailRequest scrub every
// caller-supplied string on a terminal task callback. Both request types are
// flat bags of strings, so this is exhaustive by construction — but that also
// means a NEW string field must be added here, or it reopens GH #7098 through a
// fresh door. The task-row columns these feed (error, work_dir,
// durable_work_dir, branch_name, session_id) are all TEXT, and result is
// JSONB; neither tolerates a NUL.
func sanitizeTaskCompleteRequest(req *TaskCompleteRequest) {
	req.PRURL = util.SanitizeTextForPostgres(req.PRURL)
	req.Output = util.SanitizeTextForPostgres(req.Output)
	req.SessionID = util.SanitizeTextForPostgres(req.SessionID)
	req.WorkDir = util.SanitizeTextForPostgres(req.WorkDir)
	req.DurableWorkDir = util.SanitizeTextForPostgres(req.DurableWorkDir)
	req.BranchName = util.SanitizeTextForPostgres(req.BranchName)
	req.RetiredSessionID = util.SanitizeTextForPostgres(req.RetiredSessionID)
}

func sanitizeTaskFailRequest(req *TaskFailRequest) {
	req.Error = util.SanitizeTextForPostgres(req.Error)
	req.SessionID = util.SanitizeTextForPostgres(req.SessionID)
	req.WorkDir = util.SanitizeTextForPostgres(req.WorkDir)
	req.DurableWorkDir = util.SanitizeTextForPostgres(req.DurableWorkDir)
	req.FailureReason = util.SanitizeTextForPostgres(req.FailureReason)
	req.BranchName = util.SanitizeTextForPostgres(req.BranchName)
	req.RetiredSessionID = util.SanitizeTextForPostgres(req.RetiredSessionID)
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
	// Strip bytes PostgreSQL cannot store BEFORE anything reads this payload
	// (GH #7098). The whole request is marshalled into agent_task_queue.result,
	// a JSONB column, and encoding/json renders an embedded NUL as \u0000 —
	// which JSONB rejects (SQLSTATE 22P05), rolling the completion transaction
	// back and leaving the task stuck in 'running' forever. Sanitizing here
	// rather than just before the Marshal is deliberate: the context-exhaustion
	// re-route below feeds req.Output into the failure classifier, and that
	// classifier must see exactly the text we are going to persist.
	sanitizeTaskCompleteRequest(&req)

	// GH #6402: a daemon whose backend does not (yet) read the provider's
	// structured terminal reason reports a context-exhausted run as a clean
	// success, with the CLI's "your context window is full" notice as the
	// answer. Re-route it to the failure path here so the fix does not have to
	// wait for every installed daemon to update: an un-upgraded host would
	// otherwise keep publishing that notice as the agent's reply AND keep the
	// dead session pinned as the resume pointer, which is a permanently stuck
	// (agent, issue) pair rather than a mislabelled row. Same argument, and the
	// same shared classifier, as taskfailure.NormalizeDaemonReason on the fail
	// boundary (MUL-5370). A current daemon classifies this before it ever calls
	// /complete, so this branch is dead weight for it — by design.
	if taskfailure.ContextExhaustedCompletion(req.Output) {
		slog.Warn("complete task: output is a provider context-exhaustion notice, recording as failed",
			"task_id", taskID,
			"failure_reason", taskfailure.ReasonAgentContextOverflow,
		)
		h.failTask(w, r, taskID, workspaceID, TaskFailRequest{
			Error:          req.Output,
			FailureReason:  string(taskfailure.ReasonAgentContextOverflow),
			SessionID:      req.SessionID,
			WorkDir:        req.WorkDir,
			DurableWorkDir: req.DurableWorkDir,
			// Carry the branch across the reroute. The run still delivered one:
			// it ran out of context, it did not fail to produce anything, and
			// dropping the name here would hide the work it did commit.
			BranchName:            req.BranchName,
			SessionRolloutMissing: req.SessionRolloutMissing,
			RetiredSessionID:      req.RetiredSessionID,
		})
		return
	}

	result, _ := json.Marshal(req)
	// MUL-5305: SessionRolloutMissing is applied inside CompleteTask's terminal
	// transaction (force session_id NULL + flag the row), so an auto-retry the
	// same commit creates and wakes can never observe the withheld pointer or a
	// missing continuity-gap flag.
	task, err := h.TaskService.CompleteTask(r.Context(), parseUUID(taskID), result, req.SessionID, req.WorkDir, req.BranchName, req.SessionRolloutMissing, req.RetiredSessionID, req.DurableWorkDir)
	if err != nil {
		// A CompleteTask error is an infrastructure failure (transaction /
		// assistant-outcome write), not a bad request: an already-finalized
		// callback is treated as idempotent success and returns no error. Return
		// 5xx so the daemon retries the terminal callback and the completion —
		// including the single chat outcome row — lands exactly once (MUL-4351).
		slog.Warn("complete task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.emitIssueExecutedOnFirstCompletion(r, task)

	// MUL-4195: guarantee at-least-once processing. If a member posted a
	// deliberate comment while this run was executing (or one was merged into
	// it after its context was built), schedule a single follow-up so the
	// input is never silently dropped. Loop-safe: member-authored only, capped
	// by the existing per-(issue, agent) dedup, and terminating because the
	// triggering comment always predates the follow-up run's started_at.
	h.reconcileCommentsOnCompletion(r.Context(), task)
	// The terminal transaction and completion reconciliation are committed.
	// Wake the owning runtime now so queued work that was blocked by this
	// task's agent capacity or serialization key is re-claimed immediately.
	h.TaskService.NotifyTaskFinished(*task)

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

// reconcileCommentsOnCompletion closes the at-least-once gap for member
// comments a completing run did NOT deliver (MUL-4195).
//
// The merge path (mergeCommentIntoPendingTask) folds a comment into a task only
// while it is still PRE-CLAIM (queued/deferred). Those planned ids are not
// proof of delivery: older daemons ignore structured coalesced fields, and a
// referenced comment can be deleted before claim. Claim therefore records the
// ids it actually embedded in delivered_comment_ids. Anything posted during
// this run's lifetime that is NOT in that receipt must earn a follow-up.
//
// Anchor = created_at + delivered-set exclusion, NOT a dispatch/start timestamp
// (MUL-4195 review round-3 must-fix). A timestamp anchor cannot tell a
// delivered comment from an undelivered one, and there is a race it structurally
// misses: a comment created while the task was still queued, but whose merge
// lost the race to the daemon claiming the task (queued→dispatched) — the merge
// then finds no pre-claim row (ErrNoRows), the enqueue path defers to reconcile,
// yet the comment's created_at is BEFORE dispatched_at, so a dispatched_at
// anchor would skip it and it would vanish. Anchoring on the task's own
// created_at reaches back over the whole run, and excluding the delivered set
// is what prevents re-firing comments the run actually received. Together
// they catch the pre-dispatch merge-race comment, the
// dispatch→start comment, and the during-run comment, while never double-firing
// a delivered one.
//
// Scope + loop safety:
//   - MEMBER comments qualify as before, with their full routing. AGENT comments
//     now also qualify, but ONLY through an explicit @agent/@squad mention
//     (keepExplicitMentionTriggers). Every non-mention agent route — the
//     assigned-squad-leader fallback, thread-parent / conversation continuation
//     — is intentionally excluded, so a plain agent reply / acknowledgement
//     earns no follow-up here regardless of issue assignment. That is the
//     anti-loop boundary the old member-only filter protected.
//     This closes MUL-4304: an explicit agent→agent @mention that landed while
//     the target already had a DISPATCHED task is dropped by the create-time
//     enqueue path — merge only folds a comment into a QUEUED task, so a
//     dispatched target hits the merge-miss + active-task `continue` and is
//     deferred here — and was then never replayed because agent comments were
//     excluded. (A target with only a RUNNING/queued task does not hit that
//     drop: queued merges in, running-only takes the normal fresh-enqueue path.)
//   - Only comments routing to THE AGENT THAT JUST RAN earn a follow-up here;
//     an `@other-agent` comment is left to that agent's own creation-time
//     trigger, so a completion never re-wakes an unrelated agent.
//   - Every undelivered qualifying comment is replayed through the normal
//     enqueue path in chronological order, so they coalesce into a SINGLE
//     follow-up task (the first enqueues it, the rest merge in). Bounded to one
//     run, and terminating: the follow-up's own created_at is later than all of
//     these comments and its delivered set will contain them, so its completion
//     finds nothing to re-schedule.
func (h *Handler) reconcileCommentsOnCompletion(ctx context.Context, task *db.AgentTaskQueue) {
	if task == nil || !task.IssueID.Valid || !task.AgentID.Valid || !task.CreatedAt.Valid {
		return
	}
	plannedCommentIDs := append([]pgtype.UUID{}, task.CoalescedCommentIds...)
	if task.TriggerCommentID.Valid {
		plannedCommentIDs = append(plannedCommentIDs, task.TriggerCommentID)
	}
	comments, err := h.Queries.ListReconcilableCommentsForIssueSince(ctx, db.ListReconcilableCommentsForIssueSinceParams{
		IssueID:           task.IssueID,
		Since:             task.CreatedAt,
		PlannedCommentIds: plannedCommentIDs,
	})
	if err != nil {
		slog.Warn("reconcile comments on completion: list comments failed",
			"issue_id", uuidToString(task.IssueID), "task_id", uuidToString(task.ID), "error", err)
		return
	}
	if len(comments) == 0 {
		return
	}
	// The delivered set is the claim-time receipt, not the enqueue-time plan.
	// Legacy tasks backfill only the primary trigger, deliberately replaying
	// coalesced inputs that their daemon may have ignored.
	delivered := make(map[string]struct{}, len(task.DeliveredCommentIds))
	for _, id := range task.DeliveredCommentIds {
		if id.Valid {
			delivered[uuidToString(id)] = struct{}{}
		}
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		slog.Warn("reconcile comments on completion: load issue failed",
			"issue_id", uuidToString(task.IssueID), "error", err)
		return
	}
	agentID := uuidToString(task.AgentID)
	scheduled := 0
	for i := range comments {
		c := comments[i]
		if _, ok := delivered[uuidToString(c.ID)]; ok {
			// Already delivered to this run (trigger or pre-claim coalesced).
			continue
		}
		if isNoteComment(c.Content) {
			continue
		}
		// A delegated failure recovery signal is platform-authored and targets
		// the exact source coordinator recorded by the failed task. It bypasses
		// generic comment routing (which deliberately treats system authors as
		// unattributed) and excludes this just-completed task from the coverage
		// check because the signal was planned after claim, not delivered to it.
		if service.IsDelegatedFailureRecoveryComment(c) {
			if err := h.TaskService.DispatchDelegatedFailureRecoveryComment(ctx, c, task.ID); err != nil {
				slog.Warn("reconcile comments on completion: delegated failure recovery replay failed",
					"issue_id", uuidToString(task.IssueID),
					"task_id", uuidToString(task.ID),
					"comment_id", uuidToString(c.ID),
					"error", err,
				)
			} else {
				scheduled++
			}
			continue
		}
		var parentComment *db.Comment
		if c.ParentID.Valid {
			// Scope to the issue's workspace; a comment's parent is always in the
			// same workspace, so this only fails closed against a stray foreign
			// UUID rather than changing behavior (MUL-4252).
			if parent, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
				ID:          c.ParentID,
				WorkspaceID: issue.WorkspaceID,
			}); err == nil {
				parentComment = &parent
			}
		}
		// Compute what this comment would trigger, then keep ONLY the agent
		// that just completed — never the full fan-out (that would re-wake
		// unrelated `@other-agent` targets).
		//
		// The comment is routed under its OWN author_type. A member is its own
		// originator. For an agent author, the originator is the human at the
		// top of that agent's trigger chain (resolved from the comment's source
		// task); canInvokeAgent judges an agent→agent (A2A) mention by that
		// originator, not the immediate agent principal (MUL-3963).
		actorType := c.AuthorType
		actorID := uuidToString(c.AuthorID)
		originatorUserID := actorID
		if actorType != "member" {
			originatorUserID = uuidToString(h.TaskService.ResolveOriginatorFromTriggerComment(ctx, issue.WorkspaceID, c.ID))
		}
		triggers, _ := h.computeCommentAgentTriggers(ctx, issue, c.Content, parentComment, actorType, actorID, commentTriggerComputeOptions{
			ExcludeTriggerCommentID: c.ID,
			OriginatorUserID:        originatorUserID,
		})
		// For an AGENT author, compensate ONLY explicit @agent/@squad mentions.
		// computeCommentAgentTriggers can also return the assigned-squad-leader
		// fallback (Source = issue-assignee) for a plain worker-agent reply on a
		// squad-assigned issue; that conversational routing is intentionally NOT
		// replayed here. Restricting to the explicit-mention sources keeps the
		// invariant unconditional — a plain agent reply / acknowledgement earns
		// no follow-up regardless of issue assignment — which is the anti-loop
		// boundary the old member-only filter protected (MUL-4304). Member
		// comments are unaffected: they keep their full routing.
		if actorType != "member" {
			triggers = keepExplicitMentionTriggers(triggers)
		}
		scoped := make([]commentAgentTrigger, 0, 1)
		for _, trigger := range triggers {
			if uuidToString(trigger.Agent.ID) == agentID {
				scoped = append(scoped, trigger)
			}
		}
		if len(scoped) == 0 {
			continue
		}
		// The first qualifying comment enqueues the follow-up task; later ones
		// find it AlreadyPending and merge in, so all undelivered comments end
		// up covered by a single bounded run.
		//
		// A BLOCKED replay must not be discarded (#5914, Elon round 8): the slot
		// can be held by a task that will never see this comment (it predates that
		// task and is not in its planned ids), so dropping the failure here is
		// exactly how a promised follow-up is lost. Hand the obligation to that
		// blocker instead, keeping it alive until some run provably covers it.
		if res := h.enqueueCommentAgentTriggers(ctx, issue, c.ID, scoped)[agentID]; res.status == DispatchBlocked {
			headSha := h.TaskService.ResolveIssueReviewSHAParam(ctx, task.IssueID)
			if h.propagateUncoveredCommentObligation(ctx, issue, scoped[0], c.ID, headSha) {
				slog.Info("reconcile comments on completion: replay blocked, obligation handed to the active task",
					"issue_id", uuidToString(task.IssueID), "agent_id", agentID, "comment_id", uuidToString(c.ID))
			} else {
				// The slot is held by a DIFFERENT-head queued task: it can neither
				// cover this comment nor accept it (merging would let an old-head
				// run consume a new-head request — TEN-356). Today's schema has no
				// safe place to park the obligation, so surface it loudly rather
				// than let it disappear quietly.
				slog.Error("reconcile comments on completion: replay blocked and obligation could not be handed off; comment needs a durable obligation record",
					"issue_id", uuidToString(task.IssueID), "agent_id", agentID, "comment_id", uuidToString(c.ID),
					"reason", res.reason)
			}
		}
		scheduled++
	}
	if scheduled > 0 {
		slog.Info("reconcile comments on completion: scheduled follow-up",
			"issue_id", uuidToString(task.IssueID),
			"completed_task_id", uuidToString(task.ID),
			"agent_id", agentID,
			"undelivered_comments", scheduled)
	}
}

// keepExplicitMentionTriggers filters a computed trigger set down to the ones
// produced by an EXPLICIT @agent / @squad mention (MUL-4304). It is applied to
// agent-authored comments during completion reconcile so that only a
// deliberately-targeted mention earns a replay — the assigned-squad-leader
// fallback, thread-parent / conversation continuation, and issue-assignee
// routing (all non-mention sources) are intentionally excluded, so a plain
// agent reply or acknowledgement never earns a follow-up here. Member comments
// are never passed through this filter; they keep their full routing.
func keepExplicitMentionTriggers(triggers []commentAgentTrigger) []commentAgentTrigger {
	if len(triggers) == 0 {
		return triggers
	}
	filtered := make([]commentAgentTrigger, 0, len(triggers))
	for _, trigger := range triggers {
		switch trigger.Source {
		case commentTriggerSourceMentionAgent, commentTriggerSourceMentionSquadLeader:
			filtered = append(filtered, trigger)
		}
	}
	return filtered
}

// buildCoalescedCommentData loads the full detail of each comment that was
// folded into a not-yet-started run (MUL-4195) so the claim response can embed
// them and the prompt can address each without assuming they share the
// triggering thread (review should-fix #3). Thread id follows the same rule as
// the triggering comment (parent id when the comment is a reply, else the
// comment's own id). Missing comments (deleted / wrong workspace) are skipped
// rather than failing the claim. The set is bounded by how many comments a user
// fires before a run starts, so the per-comment lookups stay cheap.
func (h *Handler) buildCoalescedCommentData(ctx context.Context, workspaceID pgtype.UUID, ids []pgtype.UUID) []CoalescedCommentData {
	if len(ids) == 0 {
		return nil
	}
	out := make([]CoalescedCommentData, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		idString := uuidToString(id)
		if _, ok := seen[idString]; ok {
			continue
		}
		seen[idString] = struct{}{}
		// Workspace-scoped so a foreign comment UUID resolves to "missing"
		// (skipped) instead of leaking another tenant's text into the prompt
		// (MUL-4252). Matches this function's documented skip-on-missing rule.
		comment, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
			ID:          id,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			continue
		}
		data := CoalescedCommentData{
			ID:         uuidToString(comment.ID),
			ThreadID:   uuidToString(comment.ID),
			AuthorType: comment.AuthorType,
			Content:    comment.Content,
			CreatedAt:  timestampToString(comment.CreatedAt),
		}
		if comment.ParentID.Valid {
			data.ThreadID = uuidToString(comment.ParentID)
		}
		if comment.AuthorID.Valid {
			switch comment.AuthorType {
			case "agent":
				if a, err := h.Queries.GetAgent(ctx, comment.AuthorID); err == nil {
					data.AuthorName = a.Name
				}
			case "member":
				if u, err := h.Queries.GetUser(ctx, comment.AuthorID); err == nil {
					data.AuthorName = u.Name
				}
			}
		}
		out = append(out, data)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, out[i].CreatedAt)
		right, rightErr := time.Parse(time.RFC3339Nano, out[j].CreatedAt)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.Before(right)
		}
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func commentDataIDs(comments []CoalescedCommentData) []pgtype.UUID {
	if len(comments) == 0 {
		return []pgtype.UUID{}
	}
	ids := make([]pgtype.UUID, 0, len(comments))
	for _, comment := range comments {
		ids = append(ids, parseUUID(comment.ID))
	}
	return ids
}

const maxClaimCommentPayloadBytes = 512 << 10 // 512 KiB of comment input per claim

// selectCommentDelivery applies a deterministic budget to comment input. The
// primary trigger is mandatory when it still exists, even when that single
// comment exceeds the budget; one comment must never become unclaimable. Extra
// comments are admitted as an oldest-first prefix, so overflow remains a stable
// suffix for completion reconciliation and cannot starve across retries.
func selectCommentDelivery(comments []CoalescedCommentData, triggerID string, legacy bool, limit int) []CoalescedCommentData {
	if len(comments) == 0 {
		return nil
	}
	mandatoryID := triggerID
	mandatoryFound := false
	for _, comment := range comments {
		if comment.ID == mandatoryID {
			mandatoryFound = true
			break
		}
	}
	if !mandatoryFound {
		// The planned trigger may have been deleted. Keep the newest available
		// comment so the claim still makes progress and reconcile can pick up the
		// remainder.
		mandatoryID = comments[len(comments)-1].ID
	}

	selected := map[string]struct{}{mandatoryID: {}}
	used := commentDeliveryBaseSize(legacy) + commentDeliveryEntrySize(commentByID(comments, mandatoryID), legacy)
	for _, comment := range comments {
		if comment.ID == mandatoryID {
			continue
		}
		cost := commentDeliveryEntrySize(comment, legacy)
		if limit > 0 && used+cost > limit {
			break
		}
		selected[comment.ID] = struct{}{}
		used += cost
	}

	out := make([]CoalescedCommentData, 0, len(selected))
	for _, comment := range comments {
		if _, ok := selected[comment.ID]; ok {
			out = append(out, comment)
		}
	}
	return out
}

func commentByID(comments []CoalescedCommentData, id string) CoalescedCommentData {
	for _, comment := range comments {
		if comment.ID == id {
			return comment
		}
	}
	return CoalescedCommentData{}
}

func commentDeliveryBaseSize(legacy bool) int {
	if legacy {
		return escapedJSONStringContentSize(legacyCommentBundleHeader)
	}
	return 2 // JSON array brackets
}

func commentDeliveryEntrySize(comment CoalescedCommentData, legacy bool) int {
	if legacy {
		return escapedJSONStringContentSize(formatLegacyCommentEntry(comment))
	}
	encoded, err := json.Marshal(comment)
	if err != nil {
		return maxClaimCommentPayloadBytes + 1
	}
	return len(encoded) + 1 // comma between JSON array entries
}

func escapedJSONStringContentSize(value string) int {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) < 2 {
		return maxClaimCommentPayloadBytes + 1
	}
	// json.Marshal adds the surrounding string quotes. The remaining bytes
	// match the escaping cost paid when the legacy bundle is nested in the
	// claim response.
	return len(encoded) - 2
}

const legacyCommentBundleHeader = "This run covers multiple distinct issue comments. Address every comment below in chronological order; do not treat this bundle as one rewritten comment.\n"

// formatLegacyCommentBundle carries every planned comment through the one
// field understood by daemons that predate coalesced-comments-v1. Delimiters,
// ids and thread ids keep distinct instructions attributable and fetchable.
func formatLegacyCommentBundle(comments []CoalescedCommentData) string {
	if len(comments) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(legacyCommentBundleHeader)
	for _, comment := range comments {
		b.WriteString(formatLegacyCommentEntry(comment))
	}
	return strings.TrimSpace(b.String())
}

func formatLegacyCommentEntry(comment CoalescedCommentData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n--- comment %s", comment.ID)
	if comment.ThreadID != "" {
		fmt.Fprintf(&b, " [thread %s]", comment.ThreadID)
	}
	if comment.AuthorType != "" || comment.AuthorName != "" {
		fmt.Fprintf(&b, " [author %s", comment.AuthorType)
		if comment.AuthorName != "" {
			fmt.Fprintf(&b, ": %s", comment.AuthorName)
		}
		b.WriteString("]")
	}
	if comment.CreatedAt != "" {
		fmt.Fprintf(&b, " [created %s]", comment.CreatedAt)
	}
	b.WriteString(" ---\n")
	b.WriteString(comment.Content)
	fmt.Fprintf(&b, "\n--- end comment %s ---\n", comment.ID)
	return b.String()
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
	// CostUSDTicks is the provider's own price for this usage in 1e-10 USD.
	// Absent or 0 from every daemon that doesn't have one (older builds, and
	// every provider except Grok today) — stored as NULL so the reader knows
	// to fall back to rate-table estimation rather than reading a real $0.
	CostUSDTicks int64 `json:"cost_usd_ticks"`
}

// authoritativeCostTicks converts a reported cost into the nullable column.
// Only a positive figure is authoritative: 0 is what a daemon that knows
// nothing about cost sends, and storing it would claim a genuine $0 spend and
// suppress the estimate. Negative is nonsense from a malformed report.
func authoritativeCostTicks(ticks int64) pgtype.Int8 {
	if ticks <= 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: ticks, Valid: true}
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
				if rt, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceTask, task.RuntimeID); err == nil {
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
			CostUsdTicks:     authoritativeCostTicks(u.CostUSDTicks),
		}); err != nil {
			slog.Warn("upsert task usage failed", "task_id", taskID, "model", u.Model, "error", err)
			continue
		}
		h.TaskService.CaptureTaskUsage(r.Context(), task, provider, u.Model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens, u.CostUSDTicks)

		// Surface prompt-cache effectiveness per run so cache hit rates are
		// observable in logs, not just queryable from runtime_usage. The ratio
		// is cached input over total input-side tokens; a persistently low
		// value flags a prompt prefix that is not being reused across runs
		// (e.g. volatile values poisoning the cacheable prefix). MUL-3887.
		if totalInput := u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens; totalInput > 0 {
			slog.Info("task prompt-cache usage",
				"task_id", taskID,
				"provider", provider,
				"model", u.Model,
				"input_tokens", u.InputTokens,
				"output_tokens", u.OutputTokens,
				"cache_read_tokens", u.CacheReadTokens,
				"cache_write_tokens", u.CacheWriteTokens,
				"cache_read_ratio", float64(u.CacheReadTokens)/float64(totalInput),
			)
		}
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
	Error          string `json:"error"`
	SessionID      string `json:"session_id,omitempty"`
	WorkDir        string `json:"work_dir,omitempty"`
	DurableWorkDir string `json:"durable_work_dir,omitempty"`
	FailureReason  string `json:"failure_reason,omitempty"`
	// BranchName: a failed run can still have produced a branch — worktree mode
	// commits whatever the agent left before tearing the worktree down. Report
	// it so a partially-successful run is still findable.
	BranchName string `json:"branch_name,omitempty"`
	// SessionRolloutMissing: the daemon withheld this task's Codex session
	// because its rollout was missing (MUL-5305). Clear the resume pointer and
	// flag the continuity gap for the next claim.
	SessionRolloutMissing bool `json:"session_rollout_missing,omitempty"`
	// RetiredSessionID: a session this run proved unresumable and abandoned
	// (GH #6066). Distinct from an empty SessionID, which only means "nothing
	// to report" — this says "never hand this id to a later run". Older
	// daemons omit it, which is exactly the pre-fix behaviour.
	RetiredSessionID string `json:"retired_session_id,omitempty"`
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
	// TaskService.FailTask normalizes req.Error itself, but every other field
	// here lands in a TEXT column too and a NUL in any one of them fails the
	// same transaction (GH #7098).
	sanitizeTaskFailRequest(&req)

	h.failTask(w, r, taskID, workspaceID, req)
}

// failTask records a terminal failure and writes the response. Shared by the
// /fail endpoint and by CompleteTask's context-exhaustion normalization so a
// run re-classified at the /complete boundary lands through exactly the same
// transaction, token revocation and runtime wake-up as one the daemon reported
// as failed itself.
func (h *Handler) failTask(w http.ResponseWriter, r *http.Request, taskID, workspaceID string, req TaskFailRequest) {
	// MUL-5305: SessionRolloutMissing is applied inside FailTask's terminal
	// transaction — forcing session_id NULL (overriding the COALESCE that would
	// keep a stale mid-flight pin) and flagging the row in the same commit that
	// creates and wakes the auto-retry, so the retry can never claim the withheld
	// pointer or miss the continuity gap.
	task, err := h.TaskService.FailTask(r.Context(), parseUUID(taskID), req.Error, req.SessionID, req.WorkDir, req.BranchName, req.FailureReason, req.SessionRolloutMissing, req.RetiredSessionID, req.DurableWorkDir)
	if err != nil {
		// A FailTask error is an infrastructure failure (the terminal
		// transaction that also clears the withheld session, writes the
		// continuity-gap flag, and creates the auto-retry rolled back), not a bad
		// request. Return 5xx so the daemon's terminal callback — which treats a
		// 400 as permanent and bails without retrying (postJSONWithRetry /
		// isTransientError) — retries and the fail, gap flag, and retry land
		// exactly once (MUL-5305). An invalid request body still returns 400 above.
		slog.Warn("fail task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.TaskService.NotifyTaskFinished(*task)

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

	// Verify the caller owns this task's workspace. The access check already
	// resolves the workspace id (it needs it to authorize the daemon), so take
	// it from there instead of re-running GetIssue / GetChatSession: this
	// endpoint fires every 500ms for every running task, and that second
	// lookup was a whole extra query per batch on the hottest write path in
	// the system (MUL-6523).
	task, wsID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	// Broadcast reach is deliberately unchanged: only issue- and chat-backed
	// tasks streamed live messages before, and widening that to autopilot /
	// quick-create tasks is a product decision, not part of this optimization.
	workspaceID := ""
	if task.IssueID.Valid || task.ChatSessionID.Valid {
		workspaceID = wsID
	}

	// Column-wise parameters for the single batch insert below. Building them
	// as parallel arrays rather than as one JSON document is deliberate: these
	// strings were just decoded out of the request, and re-encoding content /
	// output into JSON would make the server escape every byte a second time
	// and Postgres parse the envelope back out — measurably worse on exactly
	// the large messages that are already the most expensive (MUL-6523). Empty
	// string means SQL NULL, applied by the query's NULLIF.
	n := len(req.Messages)
	params := db.CreateTaskMessagesParams{
		TaskID:   parseUUID(taskID),
		Ids:      make([]pgtype.UUID, 0, n),
		Seqs:     make([]int32, 0, n),
		Types:    make([]string, 0, n),
		Tools:    make([]string, 0, n),
		Contents: make([]string, 0, n),
		Inputs:   make([]string, 0, n),
		Outputs:  make([]string, 0, n),
	}
	for _, msg := range req.Messages {
		id, err := uuid.NewV7()
		if err != nil {
			slog.Error("failed to generate task message id", "task_id", taskID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to persist task message")
			return
		}

		// Redact sensitive information before persisting or broadcasting.
		msg.Content = redact.Text(msg.Content)
		msg.Output = redact.Text(msg.Output)
		msg.Input = redact.InputMap(msg.Input)

		// redact.Text only masks secret-shaped substrings; it has no opinion on
		// bytes PostgreSQL refuses to store. Tool output is the likeliest
		// carrier of a stray NUL in the whole system — an agent that cats a
		// binary, or a Windows tool emitting UTF-16 — and this endpoint has no
		// retry on the daemon side, so an unsanitized batch is silently lost
		// (GH #7098). Input is a JSONB column, so it needs the deep walk: the
		// offending byte can sit at any depth of a tool's arguments. Batching
		// raises the stakes: one bad byte now fails the whole statement rather
		// than a single row, which is inherent to one-statement writes.
		msg.Type = util.SanitizeTextForPostgres(msg.Type)
		msg.Tool = util.SanitizeTextForPostgres(msg.Tool)
		msg.Content = util.SanitizeTextForPostgres(msg.Content)
		msg.Output = util.SanitizeTextForPostgres(msg.Output)
		if msg.Input != nil {
			if cleaned, ok := util.SanitizeJSONForPostgres(msg.Input).(map[string]any); ok {
				msg.Input = cleaned
			}
		}

		inputJSON := ""
		if msg.Input != nil {
			// Fail loud rather than dropping the field: a tool call whose
			// arguments silently vanish is worse than a 500 the daemon logs,
			// because the transcript then shows a tool_use with no input and
			// nothing anywhere records that it was lost.
			encoded, err := json.Marshal(msg.Input)
			if err != nil {
				slog.Error("failed to encode task message input", "task_id", taskID, "seq", msg.Seq, "error", err)
				writeError(w, http.StatusInternalServerError, "failed to persist task message")
				return
			}
			inputJSON = string(encoded)
		}

		params.Ids = append(params.Ids, pgtype.UUID{Bytes: [16]byte(id), Valid: true})
		params.Seqs = append(params.Seqs, int32(msg.Seq))
		params.Types = append(params.Types, msg.Type)
		params.Tools = append(params.Tools, msg.Tool)
		params.Contents = append(params.Contents, msg.Content)
		params.Inputs = append(params.Inputs, inputJSON)
		params.Outputs = append(params.Outputs, msg.Output)
	}

	created, err := h.Queries.CreateTaskMessages(r.Context(), params)
	if err != nil {
		slog.Error("failed to create task messages", "task_id", taskID, "count", n, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist task message")
		return
	}

	if workspaceID != "" {
		// CreateTaskMessages orders its result by seq, which is the daemon's
		// own within-batch ordering — so this publishes in the same order the
		// per-message loop did. A bare INSERT ... RETURNING has no row-order
		// guarantee, and subscribers render these events as they arrive, so the
		// ordering lives in the query rather than in the clients.
		for _, m := range created {
			// The ordered CTE makes sqlc name the row type after the query
			// rather than reusing the table model; the columns are the table's,
			// in order, so the conversion is checked by the compiler and breaks
			// loudly if the query ever stops returning the whole row.
			h.publishTask(protocol.EventTaskMessage, workspaceID, "system", "", taskID,
				taskMessageToPayload(db.TaskMessage(m), taskID, uuidToString(task.IssueID)))
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AckTaskCancelled receives the daemon's acknowledgement that it observed a
// task cancellation and finished flushing the transcript. Settles the chat
// finalization that CancelTaskWithResult deferred for a started-but-empty
// transcript (#5219); idempotent when nothing was deferred.
// TaskCancelAckRequest is the body of the daemon's cancel acknowledgement.
type TaskCancelAckRequest struct {
	// BranchName: a cancelled worktree task has already committed whatever the
	// agent produced — the worktree is finalized before the daemon learns of
	// the cancellation. The rest of the result is discarded on this path, so
	// this is the only channel that can report where the work went.
	BranchName string `json:"branch_name,omitempty"`
	// DurableWorkDir is present only when Finalize confirmed the disposable
	// worktree was removed and the configured project directory is authoritative.
	DurableWorkDir string `json:"durable_work_dir,omitempty"`
	// ErrorMessage / FailureReason: set when the cancelled run's worktree
	// Finalize ABORTED — there is no branch, and the error text naming the
	// preserved worktree is the only pointer left to the agent's work.
	ErrorMessage  string `json:"error_message,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

func (h *Handler) AckTaskCancelled(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}
	// Body is optional: older daemons send `{}`, and a decode failure must not
	// break the cancellation contract this endpoint exists for.
	var req TaskCancelAckRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	// Same persistence hazard as /fail and /complete: these strings go straight
	// into TEXT columns, and this endpoint fails LOUD on write errors, so an
	// unsanitized NUL here turns a cancelled task's only pointer to its work
	// into an endless 500 retry loop (GH #7098).
	req.ErrorMessage = util.SanitizeTextForPostgres(req.ErrorMessage)
	req.FailureReason = util.SanitizeTextForPostgres(req.FailureReason)
	req.BranchName = util.SanitizeTextForPostgres(req.BranchName)
	req.DurableWorkDir = util.SanitizeTextForPostgres(req.DurableWorkDir)

	// Terminal deliveries first, failing LOUD on persistence errors: these
	// fields are the only pointer to a cancelled task's work, and the daemon
	// retries this ack on transient failures — a warn-and-200 would turn one
	// DB blip into a permanently undiscoverable branch. Both writes never
	// overwrite already-recorded values, so replays are idempotent — and both
	// carry a status='cancelled' CAS, because the daemon acks on EVERY
	// terminal status it observes: a late ack from a stale run must not stamp
	// its branch or error onto a completed/failed row whose own callback is
	// the authoritative channel. A CAS-refused write is a deliberate no-op
	// (Exec reports no error), so the ack still returns 200 — there is
	// nothing for the daemon to retry — and the rebroadcast below is guarded
	// by the same status check inside RebroadcastCancelledTask.
	delivered := false
	if durableWorkDir := strings.TrimSpace(req.DurableWorkDir); durableWorkDir != "" {
		if err := h.Queries.SetAgentTaskDurableWorkDir(r.Context(), db.SetAgentTaskDurableWorkDirParams{
			ID:             task.ID,
			DurableWorkDir: pgtype.Text{String: durableWorkDir, Valid: true},
		}); err != nil {
			slog.Error("cancel ack: record durable work directory failed",
				"task_id", taskID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to record durable work directory")
			return
		}
		delivered = true
	}
	if branch := strings.TrimSpace(req.BranchName); branch != "" {
		if err := h.Queries.SetAgentTaskBranchName(r.Context(), db.SetAgentTaskBranchNameParams{
			ID:         task.ID,
			BranchName: pgtype.Text{String: branch, Valid: true},
		}); err != nil {
			slog.Error("cancel ack: record branch name failed",
				"task_id", taskID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to record branch name")
			return
		}
		delivered = true
	}
	if msg := strings.TrimSpace(req.ErrorMessage); msg != "" {
		reason := strings.TrimSpace(req.FailureReason)
		if err := h.Queries.SetAgentTaskErrorIfEmpty(r.Context(), db.SetAgentTaskErrorIfEmptyParams{
			ID:            task.ID,
			Error:         pgtype.Text{String: msg, Valid: true},
			FailureReason: pgtype.Text{String: reason, Valid: reason != ""},
		}); err != nil {
			slog.Error("cancel ack: record preserved-work error failed",
				"task_id", taskID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to record task error")
			return
		}
		delivered = true
	}
	if delivered {
		// The task:cancelled broadcast fired at cancel time, before this ack —
		// clients may already hold a refetched row without the branch/error
		// and will not refetch again on their own.
		h.TaskService.RebroadcastCancelledTask(r.Context(), task.ID)
	}
	h.TaskService.FinalizeDeferredCancelledChat(r.Context(), task.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func taskMessageToPayload(m db.TaskMessage, taskID, issueID string) protocol.TaskMessagePayload {
	var input map[string]any
	if m.Input != nil {
		json.Unmarshal(m.Input, &input)
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
		resp[i] = taskToResponse(t, workspaceID)
	}
	// Same issue-facing attribution surface as ListTasksByIssue — hydrate names.
	h.hydrateTaskAttributions(r.Context(), attributionsOf(resp))

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

	task, err := h.TaskService.CancelTaskByUser(r.Context(), existing.ID)
	if err != nil {
		slog.Warn("cancel task failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("task cancelled by user", "task_id", taskID, "issue_id", uuidToString(task.IssueID))
	resp := taskToResponse(*task, uuidToString(issue.WorkspaceID))
	// Keep this issue-scoped surface consistent with the list endpoints so a
	// cancelled row keeps its resolved "on behalf of" name in the UI.
	h.hydrateTaskAttributions(r.Context(), []*TaskAttribution{resp.Attribution})
	writeJSON(w, http.StatusOK, resp)
}

// familyActiveRunCap bounds a scope=family read.
//
// It is a response-size budget set to the shape of the normal case — a handful
// of runs in flight at once (maintainer call, MUL-6846) — NOT a bound derived
// from how much work can really be in flight. A family can legitimately exceed
// it by a lot: a single agent may be configured up to
// agentconfig.MaxMaxConcurrentTasks (50) on its own, this feature exists
// precisely for the case where SEVERAL agents work a family at once, and the
// returned set includes queued / dispatched / waiting_local_directory rows,
// which no execution-slot limit bounds at all — a parent that fans out 200
// children can have 200 of them enqueued.
//
// So truncation here is an ordinary outcome, not a pathological one, and that
// is exactly why it must be reported. A cap that truncated silently would be
// worse than no cap: "I saw no run on that sibling" and "the answer was cut
// off" would look identical, and an agent would read the second as the first.
// The handler asks for one row more than it will return and sets
// HeaderActiveRunsTruncated when that extra row comes back. The query orders
// running-first, so what the budget drops is the least decision-relevant.
const familyActiveRunCap = 20

// HeaderActiveRunsTruncated tells a caller its coordination read hit
// familyActiveRunCap and is therefore an incomplete picture of who is working
// in this family. Same shape and convention as HeaderTimelineTruncated: the
// signal rides a header because the response body is a bare array that existing
// callers parse positionally.
const HeaderActiveRunsTruncated = "X-Active-Runs-Truncated"

// ActiveRunSummary is one in-flight run as the coordination read reports it:
// which issue, which agent, what state, since when, and the task id to follow
// up with `multica issue run-messages`.
//
// Deliberately NOT AgentTaskResponse. That type is the execution log's row —
// result, work_dir, attribution, coalesced comment ids — and it costs roughly
// 5x the bytes of this one. A caller asking "is anyone working next to me?"
// reads none of those fields, and it is an agent spending its own context on
// the answer, so the payload is cut to what the question needs.
type ActiveRunSummary struct {
	TaskID          string  `json:"task_id"`
	IssueID         string  `json:"issue_id"`
	IssueIdentifier string  `json:"issue_identifier"`
	IssueTitle      string  `json:"issue_title"`
	AgentID         string  `json:"agent_id"`
	Status          string  `json:"status"`
	CreatedAt       string  `json:"created_at"`
	StartedAt       *string `json:"started_at,omitempty"`
}

// ListTasksByIssue returns tasks for an issue — the execution history behind
// the issue-detail sidebar, and the coordination reads behind
// `multica issue runs --active` / `--siblings`.
//
// Two optional query params narrow or widen it; with neither, the response is
// byte-identical to what it has always been (full history, newest first), which
// is what the UI and the CLI's short-task-ID resolver both depend on:
//
//   - active=true — restrict to in-flight statuses, the same set the
//     issue-detail "agent live" banner calls active.
//   - scope=family — widen from this issue to its sub-issue family: the
//     issue's parent (or the issue itself, when it has no parent) plus every
//     child of that parent. Implies active=true, because a full execution
//     history across a whole family is unbounded and answers no question anyone
//     asked.
func (h *Handler) ListTasksByIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	scope := r.URL.Query().Get("scope")
	if scope != "" && scope != "issue" && scope != "family" {
		writeError(w, http.StatusBadRequest, "scope must be 'issue' or 'family'")
		return
	}
	// Parse rather than compare. `active=tru` answering with the full history
	// under a 200 is the same silent-downgrade failure the unknown-scope check
	// above rejects: the caller asked who is here NOW and would be handed every
	// run that ever finished, which reads as "nobody is here" only after it has
	// paid for the whole log. Same shape as the comment-list boolean params.
	activeOnly := false
	if activeStr := r.URL.Query().Get("active"); activeStr != "" {
		switch activeStr {
		case "true":
			activeOnly = true
		case "false":
		default:
			writeError(w, http.StatusBadRequest, "invalid active parameter; expected boolean")
			return
		}
	}

	workspaceID := uuidToString(issue.WorkspaceID)

	if scope == "family" {
		// Root the family at the parent when there is one, so a child sees its
		// siblings; at the issue itself otherwise, so a parent sees its children
		// and a standalone issue degenerates to its own active runs. One rule,
		// both directions.
		root := issue.ID
		if issue.ParentIssueID.Valid {
			root = issue.ParentIssueID
		}
		// One row beyond the cap, so a full page can be told apart from a
		// truncated one without a second count query.
		rows, err := h.Queries.ListActiveTasksByIssueFamily(r.Context(), db.ListActiveTasksByIssueFamilyParams{
			WorkspaceID: issue.WorkspaceID,
			RootIssueID: root,
			RowLimit:    familyActiveRunCap + 1,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list tasks")
			return
		}
		if len(rows) > familyActiveRunCap {
			rows = rows[:familyActiveRunCap]
			w.Header().Set(HeaderActiveRunsTruncated, "true")
		}
		summaries := make([]ActiveRunSummary, len(rows))
		for i, row := range rows {
			summaries[i] = ActiveRunSummary{
				TaskID:  uuidToString(row.TaskID),
				IssueID: uuidToString(row.IssueID),
				// Rows span several issues here, so each one has to carry the
				// issue it belongs to — a caller cannot label it from the task.
				IssueIdentifier: service.IssueIdentifier(row.IssuePrefix, row.IssueNumber),
				IssueTitle:      row.IssueTitle,
				AgentID:         uuidToString(row.AgentID),
				Status:          row.Status,
				CreatedAt:       timestampToString(row.CreatedAt),
				StartedAt:       timestampToPtr(row.StartedAt),
			}
		}
		// No attribution hydration either: it was the single largest field on
		// the old payload and needed its own query, and "on behalf of whom" is
		// an execution-log question, not a coordination one.
		writeJSON(w, http.StatusOK, summaries)
		return
	}

	var tasks []db.AgentTaskQueue
	var err error
	if activeOnly {
		tasks, err = h.Queries.ListActiveTasksByIssue(r.Context(), issue.ID)
	} else {
		tasks, err = h.Queries.ListTasksByIssue(r.Context(), issue.ID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t, workspaceID)
	}
	// Execution-log rows render the "on behalf of <member>" badge, so this
	// issue-facing surface must resolve initiator/originator names (departed-safe,
	// one batch) — otherwise the badge falls back to "someone" on issue detail.
	h.hydrateTaskAttributions(r.Context(), attributionsOf(resp))
	// Usage belongs to the execution log, not to a coordination read.
	// ListIssueTaskUsage returns a row per (task, provider, model) for EVERY
	// task the issue ever ran, so hydrating it on the active path would keep
	// paying the full-history cost this filter exists to remove — and pay it
	// for a column that is near-empty on runs that have not finished.
	if !activeOnly {
		h.hydrateTaskUsage(r.Context(), issue.ID, resp)
	}

	writeJSON(w, http.StatusOK, resp)
}

// hydrateTaskUsage attaches each run's own token usage to the execution-log
// rows. One query for the whole issue, then a map join — not one query per
// task, which would be an N+1 over a list the UI always renders in full.
//
// Usage is display metadata: a failure here must not take the execution log
// down with it, so the error is swallowed and every row keeps its nil Usage,
// which the UI already renders as "no usage recorded".
func (h *Handler) hydrateTaskUsage(ctx context.Context, issueID pgtype.UUID, resp []AgentTaskResponse) {
	if len(resp) == 0 {
		return
	}

	rows, err := h.Queries.ListIssueTaskUsage(ctx, issueID)
	if err != nil || len(rows) == 0 {
		return
	}

	byTask := make(map[string][]TaskUsageData, len(resp))
	for _, row := range rows {
		var cost *int64
		if row.CostUsdTicks.Valid {
			v := row.CostUsdTicks.Int64
			cost = &v
		}
		taskID := uuidToString(row.TaskID)
		byTask[taskID] = append(byTask[taskID], TaskUsageData{
			Provider:         row.Provider,
			Model:            row.Model,
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			CacheWriteTokens: row.CacheWriteTokens,
			CostUsdTicks:     cost,
		})
	}

	for i := range resp {
		if usage, ok := byTask[resp[i].ID]; ok {
			resp[i].Usage = usage
		}
	}
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
		// Cost split — see the note on DashboardUsageDailyResponse.
		"cost_usd_ticks":              row.TotalCostUsdTicks,
		"uncosted_input_tokens":       row.UncostedInputTokens,
		"uncosted_output_tokens":      row.UncostedOutputTokens,
		"uncosted_cache_read_tokens":  row.UncostedCacheReadTokens,
		"uncosted_cache_write_tokens": row.UncostedCacheWriteTokens,
		"task_count":                  row.TaskCount,
	})
}

const (
	maxIssueGCBatchSize      = 500
	maxIssueGCBatchBodyBytes = 64 << 10
)

type batchIssueGCCheckRequest struct {
	IssueIDs []string `json:"issue_ids"`
	// TaskIDs is optional diagnostic input from `daemon disk-usage`. Older
	// daemons omit it, so the periodic GC path keeps its existing query cost.
	TaskIDs []string `json:"task_ids,omitempty"`
}

type batchIssueGCCheckItem struct {
	ID        string     `json:"id"`
	Found     bool       `json:"found"`
	Status    string     `json:"status,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type batchIssueTaskEnvironmentItem struct {
	TaskID          string `json:"task_id"`
	Found           bool   `json:"found"`
	ResumeCandidate bool   `json:"resume_candidate,omitempty"`
}

// BatchIssueGCCheck returns one explicit result for every requested issue ID.
// The query is workspace-scoped at the SQL layer; missing rows and IDs owned by
// another workspace both become found=false so the endpoint is not an
// enumeration oracle. Requests are capped because installed daemons run this
// endpoint periodically and must not be able to produce unbounded DB work.
func (h *Handler) BatchIssueGCCheck(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxIssueGCBatchBodyBytes)
	var req batchIssueGCCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IssueIDs) > maxIssueGCBatchSize {
		writeError(w, http.StatusBadRequest, "too many issue_ids")
		return
	}
	if len(req.TaskIDs) > maxIssueGCBatchSize {
		writeError(w, http.StatusBadRequest, "too many task_ids")
		return
	}

	parsedIDs := make([]pgtype.UUID, 0, len(req.IssueIDs))
	canonicalIDs := make([]string, 0, len(req.IssueIDs))
	for _, issueID := range req.IssueIDs {
		parsedID, err := util.ParseUUID(issueID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue_id")
			return
		}
		parsedIDs = append(parsedIDs, parsedID)
		canonicalIDs = append(canonicalIDs, uuidToString(parsedID))
	}

	rows := make(map[string]db.ListIssueGCStatusesRow, len(parsedIDs))
	if len(parsedIDs) > 0 {
		result, err := h.Queries.ListIssueGCStatuses(r.Context(), db.ListIssueGCStatusesParams{
			WorkspaceID: workspaceUUID,
			IssueIds:    parsedIDs,
		})
		if err != nil {
			slog.Warn("list issue GC statuses failed", "workspace_id", workspaceID, "count", len(parsedIDs), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to check issues")
			return
		}
		for _, row := range result {
			rows[uuidToString(row.ID)] = row
		}
	}

	// ONE resolver for the whole batch, not a point lookup per row. The
	// package-level Effective issues a GetIssueStatusEntryByKey for every custom
	// key it sees, so resolving inside this loop cost up to maxIssueGCBatchSize
	// catalog queries per request — on an endpoint whose entire purpose is to
	// replace per-issue requests, and which every installed daemon runs on a
	// timer. The resolver reads the catalog lazily and at most once, so an
	// all-built-in batch still costs zero. (MUL-6243)
	resolver := issuestatus.NewResolver(workspaceUUID)
	items := make([]batchIssueGCCheckItem, 0, len(req.IssueIDs))
	for i, issueID := range req.IssueIDs {
		row, found := rows[canonicalIDs[i]]
		item := batchIssueGCCheckItem{ID: issueID, Found: found}
		if found {
			// The daemon consumes this purely as a machine signal ("is this
			// issue terminal, so its workdir can be reclaimed?") and has no
			// database of its own, so the canonical status is resolved here.
			// Normalizing server-side also means daemons that predate custom
			// statuses keep making correct GC decisions. (MUL-6243)
			item.Status = resolver.Effective(r.Context(), h.issueStatusCatalog(), row.Status)
			updatedAt := row.UpdatedAt.Time
			item.UpdatedAt = &updatedAt
		}
		items = append(items, item)
	}

	// Resume-candidate inspection is intentionally opt-in. `disk-usage` sends
	// the task ids found in local GC metadata; the normal daemon GC request does
	// not, so this cannot add point queries to the periodic cleanup loop.
	// Resolve ownership and the canonical resumable workdir for every distinct
	// (agent, issue) scope in one set-based query. Keeping the database call
	// count constant matters because this endpoint accepts up to 500 task ids.
	environments := make([]batchIssueTaskEnvironmentItem, 0, len(req.TaskIDs))
	if len(req.TaskIDs) > 0 {
		parsedTaskIDs := make([]pgtype.UUID, 0, len(req.TaskIDs))
		canonicalTaskIDs := make([]string, 0, len(req.TaskIDs))
		for _, taskID := range req.TaskIDs {
			parsedID, err := util.ParseUUID(taskID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid task_id")
				return
			}
			parsedTaskIDs = append(parsedTaskIDs, parsedID)
			canonicalTaskIDs = append(canonicalTaskIDs, uuidToString(parsedID))
		}

		subjectRows, err := h.Queries.ListIssueTaskEnvironmentSubjects(r.Context(), db.ListIssueTaskEnvironmentSubjectsParams{
			WorkspaceID: workspaceUUID,
			TaskIds:     parsedTaskIDs,
		})
		if err != nil {
			slog.Warn("list issue task environment subjects failed", "workspace_id", workspaceID, "count", len(parsedTaskIDs), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to inspect task environments")
			return
		}

		subjects := make(map[string]db.ListIssueTaskEnvironmentSubjectsRow, len(subjectRows))
		for _, subject := range subjectRows {
			taskID := uuidToString(subject.ID)
			subjects[taskID] = subject
		}

		for _, taskID := range canonicalTaskIDs {
			subject, found := subjects[taskID]
			item := batchIssueTaskEnvironmentItem{TaskID: taskID, Found: found}
			if found {
				// A durable_work_dir means this task ran in a disposable
				// local_directory worktree that was finalized and removed. The
				// server may still select its session/work_dir row, but the daemon
				// deliberately starts the next local task with a fresh env root.
				item.ResumeCandidate = !subject.DurableWorkDir.Valid &&
					subject.WorkDir.Valid && subject.WorkDir.String != "" &&
					subject.CanonicalWorkDir.Valid &&
					subject.WorkDir.String == subject.CanonicalWorkDir.String
			}
			environments = append(environments, item)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"issues": items, "task_environments": environments})
}

// GetIssueGCCheck returns minimal issue info needed by older daemon GC loops.
// Gated on workspace access so a daemon token scoped to workspace A cannot
// read issue metadata from workspace B via UUID enumeration.
func (h *Handler) GetIssueGCCheck(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueId")
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue_id")
	if !ok {
		return
	}
	issue, err := h.Queries.GetIssueGCStatus(r.Context(), issueUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if !h.requireDaemonWorkspaceAccess(w, r, uuidToString(issue.WorkspaceID)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		// Same reasoning as BatchIssueGCCheck: normalize server-side so the
		// daemon's terminal-status test stays correct. (MUL-6243)
		"status":     issuestatus.Effective(r.Context(), h.issueStatusCatalog(), issue.WorkspaceID, issue.Status),
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
